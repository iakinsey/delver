package publisher

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/maps"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
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

type DfsBasicPublisherParams struct {
	OutputQueue  queue.Queue   `json:"-" resource:"output_queue"`
	UrlStorePath string        `json:"url_store_path"`
	VisitedHosts maps.Map      `json:"-" resource:"visited_hostmap"`
	RotateAfter  time.Duration `json:"rotate_after"`
}

func NewDfsBasicPublisher(params DfsBasicPublisherParams) worker.Worker {
	return &dfsBasicPublisher{
		outputQueue:  params.OutputQueue,
		urlStorePath: params.UrlStorePath,
		visitedHosts: params.VisitedHosts,
		rotateAfter:  params.RotateAfter,
		robots:       frontier.NewNullFilter(),
		lock:         sync.Mutex{},
		firstPass:    true,
	}
}

func (s *dfsBasicPublisher) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *dfsBasicPublisher) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	now := time.Now()
	queueEmpty := s.outputQueue.Len() == 0
	noEmptyTime := s.timeSinceEmpty != nil
	atRotateTime := noEmptyTime && s.timeSinceEmpty.Add(s.rotateAfter).Before(now)
	shouldFill := (queueEmpty && atRotateTime) || s.firstPass
	s.firstPass = false

	if shouldFill {
		if err := s.fillQueueContext(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to fill queue")
		} else {
			s.timeSinceEmpty = nil
		}
	} else if queueEmpty && s.timeSinceEmpty == nil {
		s.timeSinceEmpty = &now
	} else if !queueEmpty {
		s.timeSinceEmpty = nil
	}

	return nil, nil
}

func (s *dfsBasicPublisher) fillQueue() error { return s.fillQueueContext(context.Background()) }
func (s *dfsBasicPublisher) fillQueueContext(ctx context.Context) error {
	f, err := os.Open(s.urlStorePath)

	if err != nil {
		return errors.Wrap(err, "failed to stat domain directory")
	}

	defer f.Close()
	encodedHosts, err := f.Readdirnames(-1)

	if err != nil {
		return errors.Wrap(err, "failed to list domain directory")
	}

	for _, encodedHost := range encodedHosts {
		if err := ctx.Err(); err != nil {
			return err
		}
		host, err := base64.URLEncoding.DecodeString(encodedHost)

		if err != nil {
			log.Errorf("Unable to decode host: %s", encodedHost)
			// TODO maybe move this somewhere for inspection later?
			continue
		}

		n, err := s.publishUrlsContext(ctx, string(host), path.Join(s.urlStorePath, encodedHost))
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if err = s.visitedHosts.Set(host, []byte(time.Now().Format(time.RFC3339))); err != nil {
			return err
		}
		return nil
	}

	return nil
}

func (s *dfsBasicPublisher) publishUrls(host string, hostDbPath string) (int, error) {
	return s.publishUrlsContext(context.Background(), host, hostDbPath)
}
func (s *dfsBasicPublisher) publishUrlsContext(ctx context.Context, host, hostDbPath string) (int, error) {
	count := 0
	err := maps.ConsumeHost(hostDbPath, func(m maps.Map) error {
		return m.Iter(func(k, v []byte) error {
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

			req.RequestID = types.UUID(fmt.Sprintf("%x", sha256.Sum256([]byte("dfs:"+req.URI+":"+req.Origin))))
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

			if err = queue.PutContext(ctx, s.outputQueue, msg, 0); err != nil {
				return errors.Wrap(err, "unable to queue URL")
			}

			count += 1
			log.Printf("published %d requests for host %s", count, host)

			return nil
		})
	})

	return count, errors.Wrapf(err, "failed to enumerate host: %s", host)
}

func (s *dfsBasicPublisher) OnComplete() {
	if closer, ok := s.robots.(interface{ Close() error }); ok {
		closer.Close()
	}
}
