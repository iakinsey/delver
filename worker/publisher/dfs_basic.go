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

	"github.com/iakinsey/delver/gateway/robots"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/maps"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type dfsBasicPublisher struct {
	outputQueue      queue.Queue
	urlStorePath     string
	visitedHostsPath string
	visitedHosts     maps.Map
	rotateAfter      time.Duration
	timeSinceEmpty   *time.Time
	lock             sync.Mutex
	firstPass        bool
	robots           robots.Robots
}

func NewDfsBasicPublisher(outputQueue queue.Queue, urlStorePath string, visitedDomainsPath string, rotateAfter time.Duration, r robots.Robots) worker.Worker {
	return &dfsBasicPublisher{
		outputQueue:      outputQueue,
		urlStorePath:     urlStorePath,
		visitedHostsPath: visitedDomainsPath,
		visitedHosts:     maps.NewPersistentMap(visitedDomainsPath),
		rotateAfter:      rotateAfter,
		lock:             sync.Mutex{},
		firstPass:        true,
		robots:           r,
	}
}

func (s *dfsBasicPublisher) OnMessage(msg types.Message) (interface{}, error) {
	s.lock.Lock()

	now := time.Now()
	queueEmpty := s.outputQueue.Len() == 0
	noEmptyTime := s.timeSinceEmpty != nil
	atRotateTime := noEmptyTime && s.timeSinceEmpty.Add(s.rotateAfter).Before(now)
	shouldFill := (queueEmpty && atRotateTime) || s.firstPass
	s.firstPass = false

	if shouldFill {
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
		host, err := base64.URLEncoding.DecodeString(encodedHost)

		if err != nil {
			log.Printf("Unable to decode host: %s", encodedHost)
			// TODO maybe move this somewhere for inspection later?
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
			continue
		}

		if _, err := s.visitedHosts.Get(host); err == maps.ErrKeyNotFound {
			err := s.publishUrls(string(host), path.Join(s.urlStorePath, encodedHost))
			if err := os.RemoveAll(path.Join(s.urlStorePath, encodedHost)); err != nil {
				log.Printf("Failed to delete host folder: %s", string(host))
			}
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

		if s.robots != nil {
			if allowed, err := s.robots.IsAllowed(req.URI); err != nil {
				log.Printf("unable to request robots.txt status for url: %s", err)
			} else if !allowed {
				return nil
			}
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
	s.visitedHosts.Close()
}
