package robots

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/util"
	"github.com/pkg/errors"
	"github.com/temoto/robotstxt"
)

// TODO put these values into a config module
const defaultTimeout = 10 * time.Second
const defaultUserAgent = "delver"
const defaultExpiration = 1 * time.Hour
const defaultClearExpiredDelay = 1 * time.Hour

type memoryRobots struct {
	client            *util.DelverHTTPClient
	timeout           time.Duration
	userAgent         string
	expiration        time.Duration
	clearExpiredDelay time.Duration
	robotsMap         map[string]robotsInfo
	mapMutex          sync.RWMutex
}

type robotsInfo struct {
	robots  *robotstxt.RobotsData
	created time.Time
}

func NewMemoryRobots(client *util.DelverHTTPClient) Robots {
	job := &memoryRobots{
		client:            client,
		timeout:           defaultTimeout,
		userAgent:         defaultUserAgent,
		expiration:        defaultExpiration,
		clearExpiredDelay: defaultClearExpiredDelay,
		robotsMap:         make(map[string]robotsInfo),
		mapMutex:          sync.RWMutex{},
	}

	go job.clearExpired()

	return job
}

func (s *memoryRobots) IsAllowed(u string) (bool, error) {
	meta, err := url.Parse(u)

	if err != nil {
		return false, errors.Wrap(err, "failed to parse URL")
	}

	info := s.getRobots(meta)

	if info == nil {
		info, err = s.setRobots(meta)

		if err != nil {
			return false, errors.Wrap(err, "unable to parse robots file")
		}
	}

	if info.robots == nil {
		return true, nil
	}

	return info.robots.TestAgent(meta.Path, s.userAgent), nil
}

func (s *memoryRobots) getRobots(meta *url.URL) *robotsInfo {
	s.mapMutex.Lock()

	info, ok := s.robotsMap[meta.Host]

	s.mapMutex.Unlock()

	if ok {
		return &info
	}

	return nil
}

func (s *memoryRobots) setRobots(meta *url.URL) (*robotsInfo, error) {
	info := s.getRobotsInfo(meta)
	s.mapMutex.Lock()
	s.robotsMap[meta.Host] = *info
	s.mapMutex.Unlock()

	return info, nil
}

func (s *memoryRobots) getRobotsInfo(meta *url.URL) *robotsInfo {
	robotsUrl := fmt.Sprintf("%s://%s/robots.txt", meta.Scheme, meta.Host)
	info := &robotsInfo{
		created: time.Now(),
	}

	res, err := s.client.Perform(robotsUrl)

	if err != nil {
		log.Errorf("failed to perform http request: %s", err)
		return info
	}

	if res != nil {
		defer res.Body.Close()
	}

	robots, err := robotstxt.FromResponse(res)

	if err != nil {
		log.Errorf("failed to parse robots file: %s", err)

		return info
	}

	info.robots = robots

	return info
}

func (s *memoryRobots) clearExpired() {
	for {
		time.Sleep(s.clearExpiredDelay)
		s.mapMutex.Lock()

		var keys []string
		now := time.Now()

		for key, robotsInfo := range s.robotsMap {
			if robotsInfo.created.Add(s.expiration).After(now) {
				keys = append(keys, key)
			}
		}

		for _, key := range keys {
			delete(s.robotsMap, key)
		}

		s.mapMutex.Unlock()
	}
}
