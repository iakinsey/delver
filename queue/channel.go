package queue

import "github.com/iakinsey/delver/types"

type channelQueue struct {
	channel chan types.Message
}

func NewChannelQueue() Queue {
	return &channelQueue{
		channel: make(chan types.Message),
	}
}

func (s *channelQueue) Start() error {
	return nil
}

func (s *channelQueue) Stop() error {
	return nil
}

func (s *channelQueue) GetChannel() chan types.Message {
	return s.channel
}

func (s *channelQueue) Put(msg types.Message, priority int) error {
	s.channel <- msg

	return nil
}

func (s *channelQueue) Prepare() error {
	return nil
}

func (s *channelQueue) EndTransaction(types.Message, bool) error {
	return nil
}

func (s *channelQueue) Len() int64 {
	return int64(len(s.channel))
}
