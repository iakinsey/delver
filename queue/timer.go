package queue

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/iakinsey/delver/types"
)

type timerQueue struct {
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
	go s.perform()

	return nil
}

func (s *timerQueue) perform() {
	s.notify()

	for {
		select {
		case <-time.After(s.delay):
			s.notify()
		case <-s.terminate:
			s.terminated <- true
			return
		}
	}
}

func (s *timerQueue) notify() {
	time, _ := json.Marshal(time.Now().Unix())

	s.channel <- types.Message{
		ID:          string(types.NewV4()),
		MessageType: types.TimerType,
		Message:     json.RawMessage(time),
	}

}

func (s *timerQueue) Stop() error {
	s.terminate <- true
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
