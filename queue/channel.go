package queue

import (
	"context"
	"errors"
	"github.com/iakinsey/delver/types"
	"sync"
)

type channelQueue struct {
	admission
	done    chan struct{}
	once    sync.Once
	channel chan types.Message
}

func NewChannelQueue() Queue {
	return &channelQueue{
		channel: make(chan types.Message),
		done:    make(chan struct{}),
	}
}

func (s *channelQueue) Start() error {
	return nil
}

func (s *channelQueue) Stop() error {
	s.once.Do(func() { close(s.done); s.releaseAll() })
	return nil
}

func (s *channelQueue) GetChannel() chan types.Message {
	return s.channel
}

func (s *channelQueue) Put(msg types.Message, priority int) error {
	return s.PutContext(context.Background(), msg, priority)
}
func (s *channelQueue) PutContext(ctx context.Context, msg types.Message, priority int) error {
	claimCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-claimCtx.Done():
		}
	}()
	release, err := s.begin(claimCtx)
	if err != nil {
		return err
	}
	if msg.ID == "" {
		msg.ID = string(types.NewV4())
	}
	s.hold(msg, release)
	select {
	case s.channel <- msg:
		return nil
	case <-ctx.Done():
		s.finish(msg)
		return ctx.Err()
	case <-s.done:
		s.finish(msg)
		return errors.New("queue stopped")
	}
}

func (s *channelQueue) Prepare() error {
	return nil
}

func (s *channelQueue) EndTransaction(msg types.Message, success bool) error {
	s.finish(msg)
	return nil
}

func (s *channelQueue) Len() int64 {
	return int64(len(s.channel))
}
