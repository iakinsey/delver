package queue

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iakinsey/delver/model"
	"github.com/iakinsey/delver/util"
)

const claimedSuffix = ".claimed"
const writingSuffix = ".writing"
const nameRegex = `[a-zA-Z0-9]+`
const identifierRegex = `\d+-\d+-\d+-` + nameRegex

type fileQueue struct {
	name           string
	path           string
	dlqPath        string
	maxPollDelayMs time.Duration
	maxSize        int
	channel        chan model.Message
	terminate      chan bool
	terminated     chan bool
	resilient      bool
	messageCounter uint64
	entityRegex    *regexp.Regexp
}

func NewFileQueue(name string, path string, dlqPath string, maxPollDelayMs int, maxSize int, resilient bool) (Queue, error) {
	nameRegexp, err := regexp.Compile(nameRegex)

	if err != nil {
		return nil, err
	}

	if !nameRegexp.MatchString(name) {
		return nil, fmt.Errorf("Queue name %s does not conform to regex %s", name, nameRegexp)
	}

	if err := util.GetOrCreateDir(path); err != nil {
		return nil, err
	}

	if err := util.GetOrCreateDir(dlqPath); err != nil {
		return nil, err
	}

	entityRegex, err := regexp.Compile(identifierRegex)

	if err != nil {
		return nil, err
	}

	return &fileQueue{
		name:           name,
		path:           path,
		dlqPath:        dlqPath,
		maxPollDelayMs: time.Duration(maxPollDelayMs) * time.Millisecond,
		maxSize:        maxSize,
		channel:        make(chan model.Message),
		terminate:      make(chan bool),
		resilient:      resilient,
		messageCounter: 0,
		entityRegex:    entityRegex,
	}, nil
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

func (s *fileQueue) GetChannel() chan model.Message {
	return s.channel
}

func (s *fileQueue) Put(message model.Message, priority int) error {
	atomic.AddUint64(&s.messageCounter, 1)

	timestamp := time.Now().Unix()
	fileName := fmt.Sprintf("%d-%d-%d-%s", priority, timestamp, s.messageCounter, s.name)
	finalPath := filepath.Join(s.path, fileName)
	writingPath := fmt.Sprintf("%s.%s", finalPath, writingSuffix)
	message.ID = fileName
	payload, err := json.Marshal(message)

	if err != nil {
		return err
	}

	f, err := os.Create(writingPath)

	if err != nil {
		return err
	}

	if _, err = f.Write(payload); err != nil {
		return err
	}

	if err = f.Close(); err != nil {
		return err
	}

	return os.Rename(writingPath, finalPath)
}

func (s *fileQueue) Prepare() error {
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
			return err
		}
	}

	return nil
}

func (s *fileQueue) EndTransaction(message model.Message, success bool) error {
	messagePath := filepath.Join(s.path, message.ID)

	if success {
		return os.Remove(messagePath)
	}

	fileName := strings.TrimSuffix(message.ID, claimedSuffix)
	dlqDest := filepath.Join(s.path, fileName)

	return os.Rename(messagePath, dlqDest)
}

func (s *fileQueue) perform() {
	for {
		sleepTime := time.Duration(rand.Intn(int(s.maxPollDelayMs)))

		select {
		case <-time.After(sleepTime):
			message, err := s.next()

			if err != nil && !s.resilient {
				log.Fatalf(err.Error())
			} else if err != nil {
				log.Println(err.Error())
			} else if message == nil && !s.resilient {
				log.Fatalf(fmt.Sprintf("Queue %s got nil message", s.name))
			} else if message == nil {
				log.Println(fmt.Errorf("Queue %s got nil message", s.name))
			} else {
				s.channel <- *message
			}
		case <-s.terminate:
			return
		}
	}
}

func (s *fileQueue) next() (*model.Message, error) {
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

	return nil, fmt.Errorf("Unable to claim files in queue %s", s.name)
}

func (s *fileQueue) claimFile(path string) (string, error) {
	newPath := fmt.Sprintf("%s.%s", path, claimedSuffix)

	return newPath, os.Rename(path, newPath)
}

func (s *fileQueue) getFileMessage(path string) (*model.Message, error) {
	contents, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	message := model.Message{}
	err = json.Unmarshal(contents, &message)

	return &message, err
}
