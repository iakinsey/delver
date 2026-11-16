package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

const claimedSuffix = ".claimed"
const writingSuffix = ".writing"
const nameRegex = `[a-zA-Z0-9_-]+`
const identifierRegex = `^\d+-\d+-\d+-` + nameRegex + "$"

var errQueueEmpty = errors.New("queue is empty")

type fileQueue struct {
	admission
	failures       chan error
	healthMu       sync.Mutex
	lastError      string
	putLock        sync.Mutex
	lockFile       *os.File
	maxAttempts    int
	name           string
	path           string
	dlqPath        string
	maxPollDelay   time.Duration
	maxSize        int
	channel        chan types.Message
	terminate      chan bool
	startOnce      sync.Once
	stopOnce       sync.Once
	terminated     chan bool
	terminating    bool
	resilient      bool
	messageCounter uint64
	entityRegex    *regexp.Regexp
	reset          bool
	termLock       sync.Mutex
}

type FileQueueParams struct {
	MaxAttempts    int    `json:"max_attempts"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	DlqPath        string `json:"dlq_path"`
	MaxPollDelayMs int    `json:"max_poll_delay_ms"`
	MaxSize        int    `json:"max_size"`
	Reset          bool   `json:"reset"`
	Resilient      bool   `json:"resilient"`
}

func NewFileQueue(params FileQueueParams) Queue {
	nameRegexp, err := regexp.Compile("^" + nameRegex + "$")

	if err != nil {
		panic(err)
	}

	if !nameRegexp.MatchString(params.Name) {
		panic(fmt.Errorf("queue name %s does not conform to %s", params.Name, nameRegexp))
	}

	if err := util.GetOrCreateDir(params.Path); err != nil {
		panic(err)
	}

	if err := util.GetOrCreateDir(params.DlqPath); err != nil {
		panic(err)
	}

	entityRegex, err := regexp.Compile(identifierRegex)

	if err != nil {
		panic(err)
	}

	return &fileQueue{failures: make(chan error, 1),
		name:           params.Name,
		maxAttempts:    params.MaxAttempts,
		path:           params.Path,
		dlqPath:        params.DlqPath,
		maxPollDelay:   time.Duration(params.MaxPollDelayMs) * time.Millisecond,
		maxSize:        params.MaxSize,
		channel:        make(chan types.Message),
		terminating:    false,
		terminate:      make(chan bool),
		terminated:     make(chan bool),
		resilient:      params.Resilient,
		messageCounter: 0,
		entityRegex:    entityRegex,
		reset:          params.Reset,
	}
}

func (s *fileQueue) Start() error {
	var startErr error
	s.startOnce.Do(func() {
		f, err := os.OpenFile(filepath.Join(s.path, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			startErr = err
			close(s.terminated)
			return
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			startErr = errors.Wrap(err, "queue directory already in use")
			close(s.terminated)
			return
		}
		s.lockFile = f
		if err = s.Prepare(); err != nil {
			startErr = err
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			close(s.terminated)
			return
		}
		go s.perform()
	})
	return startErr
}

func (s *fileQueue) Stop() error {
	s.startOnce.Do(func() { close(s.terminated) })
	s.stopOnce.Do(func() { close(s.terminate) })
	<-s.terminated
	return nil
}

func (s *fileQueue) setTerminating() {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	s.terminating = true
}

func (s *fileQueue) isTerminating() bool {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	return s.terminating
}

func (s *fileQueue) GetChannel() chan types.Message {
	return s.channel
}

func (s *fileQueue) Put(message types.Message, priority int) error {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	select {
	case <-s.terminate:
		return errors.New("queue stopped")
	default:
	}
	if s.maxSize > 0 && s.Len() >= int64(s.maxSize) {
		return errors.New("queue capacity exceeded")
	}
	sequence := atomic.AddUint64(&s.messageCounter, 1)

	timestamp := time.Now().UnixNano()
	fileName := fmt.Sprintf("%d-%d-%d-%s", priority, timestamp, sequence, s.name)
	finalPath := filepath.Join(s.path, fileName)
	writingPath := fmt.Sprintf("%s%s", finalPath, writingSuffix)
	if message.ID == "" {
		message.ID = string(types.NewV4())
	}
	payload, err := json.Marshal(message)

	if err != nil {
		return err
	}

	f, err := os.OpenFile(writingPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)

	if err != nil {
		return errors.Wrap(err, "failed to create path for queue put")
	}

	defer os.Remove(writingPath)
	defer f.Close()
	if _, err = f.Write(payload); err != nil {
		return errors.Wrap(err, "failed to write payload for queue put")
	}

	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return errors.Wrap(err, "failed to close file for queue put")
	}

	if err := os.Rename(writingPath, finalPath); err != nil {
		return errors.Wrap(err, "failed to rename file for queue put")
	}

	return nil
}

func (s *fileQueue) Prepare() error {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	if err := s.restoreReplays(); err != nil {
		return err
	}
	if !s.reset {
		return nil
	}

	files, err := ioutil.ReadDir(s.path)

	if err != nil {
		return err
	}

	for _, file := range files {
		oldName := file.Name()

		if !strings.HasSuffix(oldName, claimedSuffix) {
			continue
		}

		oldPath := filepath.Join(s.path, oldName)
		newPath := filepath.Join(s.path, strings.TrimSuffix(oldName, claimedSuffix))

		if err := os.Rename(oldPath, newPath); err != nil {
			return errors.Wrap(err, "failed to rename file for queue prepare")
		}
	}

	return nil
}

func (s *fileQueue) EndTransaction(message types.Message, success bool) error {
	defer s.finish(message)
	delivery := message.DeliveryID
	if delivery == "" {
		delivery = message.ID
	}
	messagePath := filepath.Join(s.path, delivery) + claimedSuffix

	if success {
		if err := os.Remove(messagePath); err != nil {
			return err
		}
		os.Remove(messagePath + ".outputs")
		return nil
	}

	message.Attempts++
	if s.maxAttempts > 0 && message.Attempts >= s.maxAttempts {
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if err = os.WriteFile(messagePath, payload, 0600); err != nil {
			return err
		}
		if err := os.Rename(messagePath, filepath.Join(s.dlqPath, delivery)); err != nil {
			return err
		}
		if err := os.Rename(messagePath+".outputs", filepath.Join(s.dlqPath, delivery)+claimedSuffix+".outputs"); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err = os.WriteFile(messagePath, payload, 0600); err != nil {
		return err
	}
	return os.Rename(messagePath, filepath.Join(s.path, delivery))
}

func (s *fileQueue) Len() int64 {
	files, err := ioutil.ReadDir(s.path)

	if err != nil {
		log.Errorf("failed to read queue directory: %s", s.path)
		return -1
	}

	var count int64
	for _, file := range files {
		name := strings.TrimSuffix(file.Name(), claimedSuffix)
		if s.entityRegex.MatchString(name) {
			count++
		}
	}
	return count
}

func (s *fileQueue) perform() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.terminate:
			cancel()
		case <-ctx.Done():
		}
	}()
	defer s.releaseAll()
	defer close(s.failures)
	defer close(s.terminated)
	defer func() {
		if s.lockFile != nil {
			syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
			s.lockFile.Close()
		}
	}()
	for {
		sleepTime := time.Duration(rand.Intn(int(maxPollDelay(s.maxPollDelay))))

		select {
		case <-time.After(sleepTime):
			if s.isTerminating() {

				return
			}

			if s.admission.enabled() {
				counts, err := s.Counts()
				if err == nil && counts["pending"] == 0 {
					continue
				}
			}
			release, e := s.begin(ctx)
			if e != nil {
				return
			}
			message, err := s.next()
			if err != nil || message == nil {
				release()
			} else {
				s.hold(*message, release)
			}

			if err == errQueueEmpty {
				continue
			} else if err != nil {
				s.healthMu.Lock()
				s.lastError = err.Error()
				s.healthMu.Unlock()
				if !s.resilient {
					s.failures <- err
					return
				}
				log.Error(err)
			} else if message == nil && !s.resilient {
				log.Errorf("Queue %s got nil message", s.name)
			} else if message == nil {
				log.Errorf("Queue %s got nil message", s.name)
			} else {
				select {
				case s.channel <- *message:
				case <-s.terminate:
					return
				}
			}
		case <-s.terminate:

			return
		}
	}
}

func (s *fileQueue) next() (*types.Message, error) {
	s.putLock.Lock()
	defer s.putLock.Unlock()
	files, err := util.ReadDirAlphabetized(s.path)
	sortQueueFiles(files)

	if err != nil {
		return nil, err
	}

	for _, file := range files {
		name := file.Name()

		if !s.entityRegex.MatchString(name) {
			continue
		}

		workFile := filepath.Join(s.path, name)
		claimedPath, err := s.claimFile(workFile)

		if err == nil {
			msg, err := s.getFileMessage(claimedPath)
			if err != nil {
				if moveErr := os.Rename(claimedPath, filepath.Join(s.dlqPath, name)); moveErr != nil {
					return nil, moveErr
				}
				return nil, err
			}
			msg.DeliveryID = name
			return msg, nil
		}
	}

	return nil, errQueueEmpty
}

func (s *fileQueue) claimFile(path string) (string, error) {
	newPath := fmt.Sprintf("%s%s", path, claimedSuffix)

	return newPath, os.Rename(path, newPath)
}

func (s *fileQueue) getFileMessage(path string) (*types.Message, error) {
	contents, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	message := types.Message{}
	err = json.Unmarshal(contents, &message)

	return &message, errors.Wrap(err, "error while parsing message json")
}

func maxPollDelay(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Millisecond
	}
	return d
}
func sortQueueFiles(files []fs.FileInfo) {
	sort.SliceStable(files, func(i, j int) bool {
		a, b := strings.SplitN(files[i].Name(), "-", 4), strings.SplitN(files[j].Name(), "-", 4)
		if len(a) < 4 || len(b) < 4 {
			return files[i].Name() < files[j].Name()
		}
		for k := 0; k < 3; k++ {
			x, _ := strconv.ParseUint(a[k], 10, 64)
			y, _ := strconv.ParseUint(b[k], 10, 64)
			if x != y {
				return x < y
			}
		}
		return files[i].Name() < files[j].Name()
	})
}

func (s *fileQueue) PutContext(ctx context.Context, msg types.Message, priority int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Put(msg, priority)
}

func (s *fileQueue) outputPath(msg types.Message) string {
	delivery := msg.DeliveryID
	if delivery == "" {
		delivery = msg.ID
	}
	return filepath.Join(s.path, delivery) + claimedSuffix + ".outputs"
}
func (s *fileQueue) LoadOutputs(msg types.Message) ([]types.Message, bool, error) {
	data, err := os.ReadFile(s.outputPath(msg))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var outputs []types.Message
	err = json.Unmarshal(data, &outputs)
	return outputs, true, err
}
func (s *fileQueue) SaveOutputs(msg types.Message, outputs []types.Message) error {
	data, err := json.Marshal(outputs)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.path, ".outputs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.outputPath(msg))
}

func (s *fileQueue) ReferencedObjects() ([]types.UUID, error) {
	var result []types.UUID
	for _, directory := range []string{s.path, s.dlqPath} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == ".lock" || strings.HasSuffix(entry.Name(), writingSuffix) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			refs, err := objectReferences(data)
			if err != nil {
				return nil, err
			}
			result = append(result, refs...)
		}
	}
	return result, nil
}

func (s *fileQueue) Health() string         { s.healthMu.Lock(); defer s.healthMu.Unlock(); return s.lastError }
func (s *fileQueue) Failures() <-chan error { return s.failures }

func (s *fileQueue) PauseDispatch(paused bool) { s.admission.pauseDispatch(paused) }
func (s *fileQueue) DispatchActive() int       { return s.admission.dispatchActive() }
