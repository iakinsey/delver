package gateway

import (
	"fmt"
	"github.com/iakinsey/delver/types"
	"golang.org/x/net/websocket"
	"sync"
	"time"
)

type clientSender struct {
	finished chan struct{}
	conn     *websocket.Conn
	messages chan types.ClientStreamerMessage
	done     chan struct{}
	once     sync.Once
}

func newClientSender(conn *websocket.Conn) *clientSender {
	s := &clientSender{conn: conn, finished: make(chan struct{}), messages: make(chan types.ClientStreamerMessage, 64), done: make(chan struct{})}
	go s.run()
	return s
}
func (s *clientSender) enqueue(message types.ClientStreamerMessage) error {
	select {
	case <-s.done:
		return fmt.Errorf("subscription closed")
	default:
	}
	select {
	case s.messages <- message:
		return nil
	case <-s.done:
		return fmt.Errorf("subscription closed")
	default:
		s.stop()
		return fmt.Errorf("subscription buffer exhausted")
	}
}
func (s *clientSender) stop() { s.once.Do(func() { close(s.done) }) }
func (s *clientSender) write(message types.ClientStreamerMessage) error {
	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return websocket.JSON.Send(s.conn, message)
}
func (s *clientSender) run() {
	defer close(s.finished)
	defer s.conn.Close()
	for {
		select {
		case message := <-s.messages:
			if err := s.write(message); err != nil {
				s.stop()
				return
			}
		case <-s.done:
			for {
				select {
				case message := <-s.messages:
					if err := s.write(message); err != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}
