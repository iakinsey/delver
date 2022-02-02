package publisher

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/maps"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type dfsBasicPublisher struct {
	inputQueue       queue.Queue
	outputQueue      queue.Queue
	urlStorePath     string
	visitedHostsPath string
	urlStore         maps.Map
	visitedHosts     maps.Map
	rotateAfter      time.Duration
	timeSinceEmpty   *time.Time
	lock             sync.Mutex
}

func NewDfsBasicPublisher(inputQueue queue.Queue, outputQueue queue.Queue, urlStorePath string, visitedDomainsPath string, rotateAfter time.Duration) worker.Worker {
	return &dfsBasicPublisher{
		inputQueue:       inputQueue,
		urlStorePath:     urlStorePath,
		visitedHostsPath: visitedDomainsPath,
		urlStore:         maps.NewMultiHostMap(urlStorePath),
		visitedHosts:     maps.NewPersistentMap(visitedDomainsPath),
		rotateAfter:      rotateAfter,
		lock:             sync.Mutex{},
	}
}

func (s *dfsBasicPublisher) OnMessage(msg types.Message) (interface{}, error) {
	s.lock.Lock()

	now := time.Now()
	queueEmpty := s.inputQueue.Len() == 0

	if queueEmpty && s.timeSinceEmpty != nil && s.timeSinceEmpty.Add(s.rotateAfter).Before(now) {
		if err := s.fillQueue(); err != nil {
			return nil, errors.Wrap(err, "failed to fill queue")
		} else {
			s.timeSinceEmpty = nil
		}
	} else if queueEmpty && s.timeSinceEmpty == nil {
		s.timeSinceEmpty = &now
	} else if !queueEmpty {
		s.timeSinceEmpty = nil
	}

	s.lock.Unlock()

	return nil, nil
}

func (s *dfsBasicPublisher) fillQueue() error {
	f, err := os.Open(s.urlStorePath)

	if err != nil {
		return errors.Wrap(err, "failed to stat domain directory")
	}

	encodedHosts, err := f.Readdirnames(-1)

	if err != nil {
		return errors.Wrap(err, "failed to list domain directory")
	}

	for _, encodedHost := range encodedHosts {
		host, err := base64.RawStdEncoding.DecodeString(encodedHost)

		if err != nil {
			log.Printf("Unable to decode host: %s", encodedHost)
			// TODO maybe move this somewhere for inspection later?
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
			continue
		}

		if _, err := s.visitedHosts.Get(host); err == maps.ErrKeyNotFound {
			err := s.publishUrls(string(host), encodedHost)
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
			return err
		} else if err != nil {
			log.Printf("Error reading key from visitedDomains: %s", string(host))
		} else {
			// TODO maybe dont delete already visited domains
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
		}
	}

	return nil
}

func (s *dfsBasicPublisher) publishUrls(host string, hostDbPath string) error {
	m := maps.NewPersistentMap(hostDbPath)
	err := m.Iter(func(k, v []byte) error {
		req := message.FetcherRequest{}

		// Populate URI + Origin
		if err := json.Unmarshal(v, &req); err != nil {
			log.Printf("unable to parse json for url: %s", string(k))
			return nil
		}

		meta, err := url.Parse(req.URI)

		if err != nil {
			log.Printf("unable to parse url: %s", req.URI)
			return nil
		}

		req.RequestID = types.NewV4()
		req.Host = meta.Host
		req.Protocol = types.ProtocolHTTP
		req.Depth = 0
		reqPayload, err := json.Marshal(req)

		if err != nil {
			log.Printf("unable to serialize request to JSON for url: %s", req.URI)
			return nil
		}

		msg := types.Message{
			ID:          string(req.RequestID),
			MessageType: types.FetcherRequestType,
			Message:     json.RawMessage(reqPayload),
		}

		if err = s.outputQueue.Put(msg, 0); err != nil {
			log.Printf("unable to queue url: %s", req.URI)
			return nil
		}

		return nil
	})

	return errors.Wrapf(err, "failed to enumerate host: %s", host)
}

func (s *dfsBasicPublisher) OnComplete() {
	s.urlStore.Close()
	s.visitedHosts.Close()
}
