package queue

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/iakinsey/delver/types"
)

type timerQueue struct {
	startOnce  sync.Once
	stopOnce   sync.Once
	delay      time.Duration
	channel    chan types.Message
	terminate  chan bool
	terminated chan bool
}

type TimerQueueParams struct {
	Delay time.Duration `json:"delay"`
}

func NewTimerQueue(params TimerQueueParams) Queue {
	return &timerQueue{
		delay:      params.Delay,
		channel:    make(chan types.Message),
		terminate:  make(chan bool),
		terminated: make(chan bool),
	}
}

func (s *timerQueue) Start() error {
	s.startOnce.Do(func() { go s.perform() })

	return nil
}

func (s *timerQueue) perform() {
	defer close(s.terminated)
	s.notify()

	for {
		select {
		case <-time.After(s.delay):
			s.notify()
		case <-s.terminate:

			return
		}
	}
}

func (s *timerQueue) notify() {
	time, _ := json.Marshal(time.Now().Unix())

	select {
	case s.channel <- types.Message{
		ID:          string(types.NewV4()),
		MessageType: types.TimerType,
		Message:     json.RawMessage(time),
	}:
	case <-s.terminate:
	}

}

func (s *timerQueue) Stop() error {
	s.startOnce.Do(func() { close(s.terminated) })
	s.stopOnce.Do(func() { close(s.terminate) })
	<-s.terminated

	return nil
}

func (s *timerQueue) GetChannel() chan types.Message {
	return s.channel
}

func (s *timerQueue) Put(types.Message, int) error {
	return errors.New("timerQueue.Put not implemented")
}

func (s *timerQueue) Prepare() error {
	return nil
}

func (s *timerQueue) EndTransaction(types.Message, bool) error {
	return nil
}

func (s *timerQueue) Len() int64 {
	return -1
}
