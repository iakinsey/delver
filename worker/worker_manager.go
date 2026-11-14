package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/armon/go-metrics"
	"github.com/iakinsey/delver/instrument"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
)

type WorkerManager interface {
	Start()
	Stop()
}
type workerManager struct {
	ctx         context.Context
	cancel      context.CancelFunc
	inbox       queue.Queue
	outbox      queue.Queue
	priority    int
	worker      Worker
	terminating bool
	terminate   chan bool
	terminated  chan bool
	metrics     metrics.MetricSink
	workerName  string
	termLock    sync.Mutex
	active      sync.WaitGroup
	stopOnce    sync.Once
}

func NewWorkerManager(worker Worker, inbox queue.Queue, outbox queue.Queue) WorkerManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &workerManager{
		ctx: ctx, cancel: cancel,
		inbox:       inbox,
		outbox:      outbox,
		priority:    0,
		worker:      worker,
		terminating: false,
		terminate:   make(chan bool),
		terminated:  make(chan bool),
		metrics:     instrument.GetMetrics(),
		workerName:  workerTypeName(worker),
	}
}

func (s *workerManager) Start() {
	s.termLock.Lock()
	if s.terminating {
		s.termLock.Unlock()
		return
	}
	s.active.Add(1)
	s.termLock.Unlock()
	defer s.active.Done()
	for {
		select {
		case message, ok := <-s.inbox.GetChannel():
			if !ok {
				return
			}
			if s.isTerminating() {
				s.metrics.IncrCounter([]string{s.workerName, "terminated"}, 1)

				return
			}

			sink := s.metrics
			if message.MessageType == types.MetricType {
				sink = &metrics.BlackholeSink{}
			}
			sink.IncrCounter([]string{s.workerName, "message", "in"}, 1)
			start := time.Now().UnixMilli()
			outputs, err := s.process(message)
			end := time.Now().UnixMilli()
			sink.AddSample([]string{s.workerName, "duration", "millisecond"}, float32(end-start))
			if err == nil {
				for _, output := range outputs {
					if err = s.put(output); err != nil {
						break
					}
				}
			}
			if err != nil {
				log.Error(err)
				sink.IncrCounter([]string{s.workerName, "error"}, 1)
			} else {
				sink.IncrCounter([]string{s.workerName, "success"}, 1)
				sink.IncrCounter([]string{s.workerName, "message", "out"}, float32(len(outputs)))
			}

			processingSucceeded := err == nil
			if err := s.inbox.EndTransaction(message, processingSucceeded); err != nil {
				sink.IncrCounter([]string{s.workerName, "inbox", "transaction", "error"}, 1)
				log.Error(err)
			} else {
				if processingSucceeded {
					if committer, ok := s.worker.(MessageCommitter); ok {
						if cleanupErr := committer.OnCommit(message); cleanupErr != nil {
							log.Error(cleanupErr)
						}
					}
				}
				sink.IncrCounter([]string{s.workerName, "inbox", "transaction", "success"}, 1)
			}
		case <-s.terminate:
			s.metrics.IncrCounter([]string{s.workerName, "terminated"}, 1)

			return
		}
	}
}

func (s *workerManager) Stop() {
	s.stopOnce.Do(func() { s.setTerminating(); close(s.terminate); s.cancel(); s.active.Wait(); s.worker.OnComplete() })
}

func (s *workerManager) setTerminating() {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	s.terminating = true
}

func (s *workerManager) isTerminating() bool {
	s.termLock.Lock()
	defer s.termLock.Unlock()

	return s.terminating
}

func (s *workerManager) publishResponse(result interface{}) error {
	if result == nil {
		return nil
	}
	values := []interface{}{result}
	if multi, ok := result.(types.MultiMessage); ok {
		values = multi.Values
	}
	for _, value := range values {
		if err := s.doPublish(value); err != nil {
			return err
		}
	}
	s.metrics.IncrCounter([]string{s.workerName, "message", "out"}, float32(len(values)))
	return nil
}
func (s *workerManager) doPublish(result interface{}) error {
	if s.outbox == nil {
		return fmt.Errorf("%s requires an outbox", s.workerName)
	}
	if envelope, ok := result.(types.Message); ok {
		return s.put(envelope)
	}
	kind, err := message.GetMessageTypeMapping(result)
	if err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.put(types.Message{ID: string(types.NewV4()), MessageType: kind, Message: data})
}

func (s *workerManager) put(msg types.Message) error {
	if q, ok := s.outbox.(interface {
		PutContext(context.Context, types.Message, int) error
	}); ok {
		return q.PutContext(s.ctx, msg, s.priority)
	}
	return s.outbox.Put(msg, s.priority)
}

func (s *workerManager) process(input types.Message) ([]types.Message, error) {
	store, staged := s.inbox.(queue.OutputStore)
	if staged {
		outputs, found, err := store.LoadOutputs(input)
		if err != nil || found {
			return outputs, err
		}
	}
	var result interface{}
	var err error
	if contextual, ok := s.worker.(ContextWorker); ok {
		result, err = contextual.OnMessageContext(s.ctx, input)
	} else {
		result, err = s.worker.OnMessage(input)
	}
	if err != nil {
		return nil, err
	}
	outputs := []types.Message{}
	if result != nil {
		values := []interface{}{result}
		if multi, ok := result.(types.MultiMessage); ok {
			values = multi.Values
		}
		for index, value := range values {
			output, ok := value.(types.Message)
			if !ok {
				kind, err := message.GetMessageTypeMapping(value)
				if err != nil {
					return nil, err
				}
				data, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				output = types.Message{MessageType: kind, Message: data}
			}
			if output.ID == "" {
				output.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", input.ID, index))))
			}
			outputs = append(outputs, output)
		}
	}
	if len(outputs) > 0 && s.outbox == nil {
		return nil, fmt.Errorf("%s requires outbox", s.workerName)
	}
	if staged {
		if err = store.SaveOutputs(input, outputs); err != nil {
			return nil, err
		}
	}
	return outputs, nil
}

func workerTypeName(w Worker) string {
	t := reflect.TypeOf(w)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
