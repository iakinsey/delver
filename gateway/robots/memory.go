package robots

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/temoto/robotstxt"
)

// TODO put these values into a config module
const defaultTimeout = 10 * time.Second
const defaultUserAgent = "delver"
const defaultExpiration = 1 * time.Hour
const defaultClearExpiredDelay = 1 * time.Hour

type memoryRobots struct {
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

func NewMemoryRobots() Robots {
	job := &memoryRobots{
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
	client := &http.Client{Timeout: s.timeout}
	req, err := http.NewRequest("GET", robotsUrl, nil)
	info := &robotsInfo{
		created: time.Now(),
	}

	if err != nil {
		log.Printf("failed to create http client: %s", err)
		return info
	}

	req.Header.Set("User-Agent", s.userAgent)

	res, err := client.Do(req)

	if err != nil {
		log.Printf("failed to perform http request: %s", err)
		return info
	}

	robots, err := robotstxt.FromResponse(res)

	if err != nil {
		log.Printf("failed to parse robots file: %s", err)

		return info
	}

	res.Body.Close()

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