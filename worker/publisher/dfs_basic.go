package publisher

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util/maps"
	"github.com/iakinsey/delver/worker"
	"github.com/pkg/errors"
)

type dfsBasicPublisher struct {
	outputQueue    queue.Queue
	urlStorePath   string
	visitedHosts   maps.Map
	rotateAfter    time.Duration
	timeSinceEmpty *time.Time
	lock           sync.Mutex
	firstPass      bool
	robots         frontier.Filter
}

func NewDfsBasicPublisher(outputQueue queue.Queue, urlStorePath string, visitedHosts maps.Map, rotateAfter time.Duration, r frontier.Filter) worker.Worker {
	return &dfsBasicPublisher{
		outputQueue:  outputQueue,
		urlStorePath: urlStorePath,
		visitedHosts: visitedHosts,
		rotateAfter:  rotateAfter,
		lock:         sync.Mutex{},
		firstPass:    true,
		robots:       r,
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
			log.Errorf("Unable to decode host: %s", encodedHost)
			// TODO maybe move this somewhere for inspection later?
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
			continue
		}

		if _, err := s.visitedHosts.Get(host); err == maps.ErrKeyNotFound {
			n, err := s.publishUrls(string(host), path.Join(s.urlStorePath, encodedHost))

			if n == 0 {
				continue
			}

			if os.RemoveAll(path.Join(s.urlStorePath, encodedHost)) != nil {
				log.Errorf("Failed to delete host folder: %s", string(host))
			}

			return err
		} else if err != nil {
			log.Errorf("Error reading key from visitedDomains: %s", string(host))
		} else {
			// TODO maybe dont delete already visited domains
			os.RemoveAll(path.Join(s.urlStorePath, encodedHost))
		}
	}

	return nil
}

func (s *dfsBasicPublisher) publishUrls(host string, hostDbPath string) (int, error) {
	m := maps.NewPersistentMap(hostDbPath)
	count := 0

	err := m.Iter(func(k, v []byte) error {
		req := message.FetcherRequest{}

		// Populate URI + Origin
		if err := json.Unmarshal(v, &req); err != nil {
			log.Errorf("unable to parse json for url: %s", string(k))
			return nil
		}

		if s.robots != nil {
			if allowed, err := s.robots.IsAllowed(req.URI); err != nil {
				log.Errorf("unable to request robots.txt status for url: %s", err)
			} else if !allowed {
				return nil
			}
		}

		meta, err := url.Parse(req.URI)

		if err != nil {
			log.Errorf("unable to parse url: %s", req.URI)
			return nil
		}

		req.RequestID = types.NewV4()
		req.Host = meta.Host
		req.Protocol = types.ProtocolHTTP
		req.Depth = 0
		reqPayload, err := json.Marshal(req)

		if err != nil {
			log.Errorf("unable to serialize request to JSON for url: %s", req.URI)
			return nil
		}

		msg := types.Message{
			ID:          string(req.RequestID),
			MessageType: types.FetcherRequestType,
			Message:     json.RawMessage(reqPayload),
		}

		if err = s.outputQueue.Put(msg, 0); err != nil {
			log.Errorf("unable to queue url: %s", req.URI)
			return nil
		}

		count += 1
		log.Printf("published %d requests for host %s", count, host)

		return nil
	})

	return count, errors.Wrapf(err, "failed to enumerate host: %s", host)
}

func (s *dfsBasicPublisher) OnComplete() {
	s.visitedHosts.Close()
}
