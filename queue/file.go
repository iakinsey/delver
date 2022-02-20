package queue

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
)

const claimedSuffix = ".claimed"
const writingSuffix = ".writing"
const nameRegex = `[a-zA-Z0-9]+`
const identifierRegex = `^\d+-\d+-\d+-` + nameRegex + "$"

var errQueueEmpty = errors.New("queue is empty")

type fileQueue struct {
	name         string
	path         string
	dlqPath      string
	maxPollDelay time.Duration
	// TODO
	maxSize        int
	channel        chan types.Message
	terminate      chan bool
	terminated     chan bool
	resilient      bool
	messageCounter uint64
	entityRegex    *regexp.Regexp
	reset          bool
}

type FileQueueParams struct {
	Name           string `json:"name"`
	Path           string `json:"name"`
	DlqPath        string `json:"dlq_path"`
	MaxPollDelayMs int    `json:"max_poll_delay_ms"`
	MaxSize        int    `json:"max_size"`
	Reset          bool   `json:"reset"`
	Resilient      bool   `json"resilient"`
}

func NewFileQueue(params FileQueueParams) Queue {
	nameRegexp, err := regexp.Compile(nameRegex)

	if err != nil {
		log.Fatalf(err.Error())
	}

	if !nameRegexp.MatchString(params.Name) {
		log.Fatalf("Queue name %s does not conform to regex %s", params.Name, nameRegexp)
	}

	if err := util.GetOrCreateDir(params.Path); err != nil {
		log.Fatalf(err.Error())
	}

	if err := util.GetOrCreateDir(params.DlqPath); err != nil {
		log.Fatalf(err.Error())
	}

	entityRegex, err := regexp.Compile(identifierRegex)

	if err != nil {
		log.Fatalf(err.Error())
	}

	return &fileQueue{
		name:           params.Name,
		path:           params.Path,
		dlqPath:        params.DlqPath,
		maxPollDelay:   time.Duration(params.MaxPollDelayMs) * time.Millisecond,
		maxSize:        params.MaxSize,
		channel:        make(chan types.Message),
		terminate:      make(chan bool),
		terminated:     make(chan bool),
		resilient:      params.Resilient,
		messageCounter: 0,
		entityRegex:    entityRegex,
		reset:          params.Reset,
	}
}

func (s *fileQueue) Start() error {
	go s.perform()

	return nil
}

func (s *fileQueue) Stop() error {
	s.terminate <- true
	<-s.terminated

	return nil
}

func (s *fileQueue) GetChannel() chan types.Message {
	return s.channel
}

func (s *fileQueue) Put(message types.Message, priority int) error {
	atomic.AddUint64(&s.messageCounter, 1)

	timestamp := time.Now().Unix()
	fileName := fmt.Sprintf("%d-%d-%d-%s", priority, timestamp, s.messageCounter, s.name)
	finalPath := filepath.Join(s.path, fileName)
	writingPath := fmt.Sprintf("%s%s", finalPath, writingSuffix)
	message.ID = fileName
	payload, err := json.Marshal(message)

	if err != nil {
		return err
	}

	f, err := os.Create(writingPath)

	if err != nil {
		return errors.Wrap(err, "failed to create path for queue put")
	}

	if _, err = f.Write(payload); err != nil {
		return errors.Wrap(err, "failed to write payload for queue put")
	}

	if err = f.Close(); err != nil {
		return errors.Wrap(err, "failed to close file for queue put")
	}

	if os.Rename(writingPath, finalPath); err != nil {
		return errors.Wrap(err, "failed to rename file for queue put")
	}

	return nil
}

func (s *fileQueue) Prepare() error {
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
	messagePath := filepath.Join(s.path, message.ID) + claimedSuffix

	if success {
		return os.Remove(messagePath)
	}

	return os.Rename(messagePath, filepath.Join(s.path, message.ID))
}

func (s *fileQueue) Len() int64 {
	files, err := ioutil.ReadDir(s.path)

	if err != nil {
		log.Errorf("failed to read queue directory: %s", s.path)
		return -1
	}

	return int64(len(files))
}

func (s *fileQueue) perform() {
	for {
		sleepTime := time.Duration(rand.Intn(int(s.maxPollDelay)))

		select {
		case <-time.After(sleepTime):
			message, err := s.next()

			if err == errQueueEmpty {
				continue
			} else if err != nil && !s.resilient {
				log.Fatalf(err.Error())
			} else if err != nil {
				log.Error(err)
			} else if message == nil && !s.resilient {
				log.Errorf("Queue %s got nil message", s.name)
			} else if message == nil {
				log.Errorf("Queue %s got nil message", s.name)
			} else {
				s.channel <- *message
			}
		case <-s.terminate:
			s.terminated <- true
			return
		}
	}
}

func (s *fileQueue) next() (*types.Message, error) {
	// TODO regex filter results
	files, err := util.ReadDirAlphabetized(s.path)

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
			return s.getFileMessage(claimedPath)
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
