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
	observer    func(types.Message, []types.Message, error)
	control     control
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
		s.control.mu.Lock()
		if s.control.activity.Paused {
			if s.control.changed == nil {
				s.control.changed = make(chan struct{})
			}
			changed := s.control.changed
			s.control.mu.Unlock()
			select {
			case <-changed:
				continue
			case <-s.terminate:
				return
			}
		}
		s.control.mu.Unlock()
		select {
		case message, ok := <-s.inbox.GetChannel():
			if !ok {
				return
			}
			for {
				s.control.mu.Lock()
				if !s.control.activity.Paused {
					s.control.activity.Active++
					s.control.mu.Unlock()
					break
				}
				if s.control.changed == nil {
					s.control.changed = make(chan struct{})
				}
				changed := s.control.changed
				s.control.mu.Unlock()
				select {
				case <-changed:
				case <-s.terminate:
					return
				}
			}
			if s.isTerminating() {
				s.control.mu.Lock()
				s.control.activity.Active--
				s.control.mu.Unlock()
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

			if err != nil {
				message.LastError = err.Error()
			} else {
				message.LastError = ""
			}
			processingSucceeded := err == nil
			transactionErr := s.inbox.EndTransaction(message, processingSucceeded)
			if transactionErr != nil {
				err = transactionErr
				sink.IncrCounter([]string{s.workerName, "inbox", "transaction", "error"}, 1)
				log.Error(err)
				s.control.mu.Lock()
				s.control.activity.LastError = err.Error()
				s.control.mu.Unlock()
			} else {
				if processingSucceeded {
					if committer, ok := s.worker.(MessageCommitter); ok {
						if cleanupErr := committer.OnCommit(message); cleanupErr != nil {
							err = cleanupErr
							log.Error(cleanupErr)
						}
					}
				}
				sink.IncrCounter([]string{s.workerName, "inbox", "transaction", "success"}, 1)
			}
			s.control.mu.Lock()
			s.control.activity.Active--
			s.control.activity.Processed++
			if err != nil {
				s.control.activity.Errors++
				s.control.activity.LastError = err.Error()
			} else {
				s.control.activity.LastSuccess = time.Now().Unix()
			}
			observer := s.observer
			s.control.mu.Unlock()
			if observer != nil {
				observer(message, outputs, err)
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

func (s *workerManager) process(input types.Message) (outputsResult []types.Message, processErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outputsResult = nil
			processErr = fmt.Errorf("worker %s panic: %v", s.workerName, recovered)
		}
	}()
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

func (s *workerManager) Pause(value bool) { s.control.pause(value) }
func (s *workerManager) Activity() Activity {
	result := s.control.snapshot()
	if health, ok := s.worker.(interface{ Health() string }); ok {
		result.LastError = health.Health()
	}
	return result
}

func (s *workerManager) SetObserver(observer func(types.Message, []types.Message, error)) {
	s.control.mu.Lock()
	defer s.control.mu.Unlock()
	s.observer = observer
}

func (s *workerManager) SetMetricSink(sink metrics.MetricSink) { s.metrics = sink }

func (s *workerManager) Health() string {
	if source, ok := s.worker.(interface{ Health() string }); ok {
		return source.Health()
	}
	return ""
}
