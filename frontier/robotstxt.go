package frontier

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	"github.com/temoto/robotstxt"
)

type memoryRobots struct {
	done              chan struct{}
	closeOnce         sync.Once
	client            util.DelverHTTPClient
	userAgent         string
	expiration        time.Duration
	clearExpiredDelay time.Duration
	fetchGate         chan struct{}
	robotsMap         map[string]robotsInfo
	mapMutex          sync.RWMutex
}

type robotsInfo struct {
	robots  *robotstxt.RobotsData
	err     error
	created time.Time
}

type ClosableFilter interface {
	Filter
	IsAllowedContext(context.Context, string) (bool, error)
	Close() error
}

func NewMemoryRobots() ClosableFilter {
	return NewMemoryRobotsWithConfig(config.Get())
}
func NewMemoryRobotsWithConfig(c config.Config) ClosableFilter {
	conf := c.Robots
	clientConf := c.HTTPClient

	job := &memoryRobots{
		done:              make(chan struct{}),
		fetchGate:         make(chan struct{}, 1),
		client:            util.NewHTTPClientWithConfig(c.HTTPClient),
		userAgent:         clientConf.UserAgent,
		expiration:        conf.Expiration,
		clearExpiredDelay: conf.ClearExpiredDelay,
		robotsMap:         make(map[string]robotsInfo),
		mapMutex:          sync.RWMutex{},
	}

	go job.clearExpired()
	return job
}

func (s *memoryRobots) IsAllowed(u string) (bool, error) {
	return s.IsAllowedContext(context.Background(), u)
}

func (s *memoryRobots) IsAllowedContext(ctx context.Context, u string) (bool, error) {
	meta, err := url.Parse(u)

	if err != nil {
		return false, errors.Wrap(err, "failed to parse URL")
	}

	info := s.getRobots(meta)

	if info == nil {
		info, err = s.setRobots(ctx, meta)

		if err != nil {
			return false, errors.Wrap(err, "unable to parse robots file")
		}
	}

	if info.err != nil {
		return false, info.err
	}
	if info.robots == nil {
		return true, nil
	}

	return info.robots.TestAgent(meta.Path, s.userAgent), nil
}

func (s *memoryRobots) getRobots(meta *url.URL) *robotsInfo {
	s.mapMutex.Lock()

	info, ok := s.robotsMap[meta.Scheme+"://"+meta.Host]

	s.mapMutex.Unlock()

	if ok && time.Since(info.created) < s.expiration {
		return &info
	}

	return nil
}

func (s *memoryRobots) setRobots(ctx context.Context, meta *url.URL) (*robotsInfo, error) {
	select {
	case s.fetchGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, context.Canceled
	}
	defer func() { <-s.fetchGate }()
	if info := s.getRobots(meta); info != nil {
		return info, info.err
	}
	info := s.getRobotsInfo(ctx, meta)
	if ctx.Err() != nil {
		return info, ctx.Err()
	}
	s.mapMutex.Lock()
	s.robotsMap[meta.Scheme+"://"+meta.Host] = *info
	s.mapMutex.Unlock()

	return info, info.err
}

func (s *memoryRobots) getRobotsInfo(ctx context.Context, meta *url.URL) *robotsInfo {
	robotsUrl := fmt.Sprintf("%s://%s/robots.txt", meta.Scheme, meta.Host)
	info := &robotsInfo{
		created: time.Now(),
	}

	var res *http.Response
	var err error
	if client, ok := s.client.(interface {
		PerformContext(context.Context, string) (*http.Response, error)
	}); ok {
		res, err = client.PerformContext(util.WithoutHTTPPolicy(ctx), robotsUrl)
	} else {
		res, err = s.client.Perform(robotsUrl)
	}

	if err != nil {
		info.err = err
		return info
	}

	if res != nil {
		defer res.Body.Close()
	}

	robots, err := robotstxt.FromResponse(res)

	if err != nil {
		info.err = err

		return info
	}

	info.robots = robots

	return info
}

func (s *memoryRobots) clearExpired() {
	if s.clearExpiredDelay <= 0 {
		return
	}
	timer := time.NewTicker(s.clearExpiredDelay)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-timer.C:
			s.mapMutex.Lock()
			for key, info := range s.robotsMap {
				if !info.created.Add(s.expiration).After(now) {
					delete(s.robotsMap, key)
				}
			}
			s.mapMutex.Unlock()
		}
	}
}
func (s *memoryRobots) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if client, ok := s.client.(interface{ Close() error }); ok {
			client.Close()
		}
	})
	return nil
}
