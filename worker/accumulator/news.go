package accumulator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/queue"
	"github.com/iakinsey/delver/resource/bloom"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/util"
	"github.com/iakinsey/delver/worker"
)

const maxDepth = 1

var blacklistedExtensions = []string{
	".jpg",
	".jpeg",
	".gif",
	".raw",
	".tiff",
	".pdf",
	".rtf",
	".doc",
	".ppt",
	".svg",
	".bmp",
	".ico",
	".png",
	".webp",
	".js",
	".css",
	".zip",
	".scss",
	".json",
	".exe",
	".jss",
	".mp4",
	".mkv",
	".mov",
	".avi",
	".flv",
	".wmv",
	".aac",
	".ogg",
	".mp3",
	".alac",
	".m4a",
	".flac",
	".wav",
	".wma",
}

var blacklistedPaths = []string{
	"section",
	"tag",
	"tags",
	"hub",
	"opinion",
	"comment",
	"feed",
	"static",
	"_static",
	"css",
	"script",
	"js",
	"img",
	"wp-content",
	"assets",
}

type newsAccumulator struct {
	maxDepth  int
	robots    frontier.Filter
	newsQueue queue.Queue
	seenUrls  bloom.BloomFilter
}

type NewsAccumulatorParams struct {
	MaxDepth  int               `json:"max_depth"`
	NewsQueue queue.Queue       `json:"-" resource:"news_queue"`
	SeenUrls  bloom.BloomFilter `json:"-" resource:"seen_urls"`
}

func NewNewsAccumulator(params NewsAccumulatorParams) worker.Worker {
	depth := params.MaxDepth
	if depth == 0 {
		depth = maxDepth
	}
	return &newsAccumulator{
		maxDepth:  depth,
		robots:    frontier.NewNullFilter(),
		newsQueue: params.NewsQueue,
		seenUrls:  params.SeenUrls,
	}
}

func (s *newsAccumulator) OnMessage(msg types.Message) (interface{}, error) {
	return s.OnMessageContext(context.Background(), msg)
}
func (s *newsAccumulator) OnMessageContext(ctx context.Context, msg types.Message) (interface{}, error) {
	var URIs features.URIs
	composite := message.CompositeAnalysis{}

	if err := json.Unmarshal(msg.Message, &composite); err != nil {
		return nil, err
	}

	if !s.isValidFormat(composite) {
		return nil, nil
	}

	if err := composite.Load(features.UrlField, &URIs); err != nil {
		return nil, nil
	}

	urls := s.processUrls(composite, URIs)
	if err := s.processArticleContext(ctx, composite); err != nil {
		return nil, err
	}

	log.Printf("published %d requests for uri %s", len(urls), composite.URI)

	return types.MultiMessage{
		Values: urls,
	}, nil
}

func (s *newsAccumulator) processUrls(composite message.CompositeAnalysis, URIs features.URIs) []interface{} {
	if composite.Depth >= s.maxDepth {
		return nil
	}

	var results []interface{}
	originParsed, err := url.Parse(composite.URI)

	if err != nil {
		log.Errorf("Unable to parse URI %s", composite.URI)
		return nil
	}

	origin := originParsed.Host
	count := 0

	for _, u := range URIs {
		parsed, err := url.Parse(u)

		if err != nil {
			continue
		}

		if !s.urlAllowed(parsed, origin) {
			continue
		}

		count += 1
		results = append(results, message.FetcherRequest{
			RequestID: types.NewV4(),
			URI:       u,
			Host:      parsed.Host,
			Origin:    composite.URI,
			ScopeRoot: composite.ScopeRoot,
			Protocol:  types.ProtocolHTTP,
			Depth:     composite.Depth + 1,
		})
	}

	if count > 0 {
		log.Printf("Published %d urls", count)
	}

	return results
}

func (s *newsAccumulator) processArticle(composite message.CompositeAnalysis) error {
	return s.processArticleContext(context.Background(), composite)
}
func (s *newsAccumulator) processArticleContext(ctx context.Context, composite message.CompositeAnalysis) error {
	if composite.Depth == 0 {
		return nil
	}

	msg, err := json.Marshal(composite)

	if err != nil {
		return err
	}

	article := types.Message{
		ID:          fmt.Sprintf("%x", sha256.Sum256([]byte("news:"+string(composite.RequestID)+":"+composite.URI))),
		MessageType: types.CompositeAnalysisType,
		Message:     json.RawMessage(msg),
	}

	return queue.PutContext(ctx, s.newsQueue, article, 0)
}

func (s *newsAccumulator) isValidFormat(composite message.CompositeAnalysis) bool {
	keys := []string{"Content-Type", "content-type"}

	for _, key := range keys {
		if val, ok := composite.Header[key]; ok {
			if len(val) == 0 {
				continue
			}

			if !strings.Contains(val[0], "html") {
				return false
			}
		}
	}

	return true
}

func (s *newsAccumulator) urlAllowed(u *url.URL, origin string) bool {
	// External sites
	if u.Host != origin {
		return false
	}

	// Blacklisted media formats
	if util.HasSuffixes(u.Path, blacklistedExtensions) {
		return false
	}

	uri := u.String()

	// Robots.txt
	if allowed, err := s.robots.IsAllowed(uri); err != nil {
		log.Errorf("Failed to get robots info for URL %s: %s", u, err)
		return false
	} else if !allowed {
		return false
	}

	if !s.urlLooksLikeArticle(u) {
		return false
	}

	if strings.Contains(u.Path, ":") && strings.Contains(u.Path, "=") {
		return false
	}

	if s.seenUrls != nil && s.seenUrls.ContainsString(uri) {
		return false
	}

	return true
}

func (s *newsAccumulator) urlLooksLikeArticle(u *url.URL) bool {
	var tokens []string

	for _, t := range strings.Split(u.Path, "/") {
		if t == "" {
			continue
		}

		tokens = append(tokens, t)
	}

	// Empty path, should not even be in this branch
	if len(tokens) == 0 {
		return false
	}

	// If it starts with article then its probably an article
	if strings.Contains(tokens[0], "article") {
		return true
	}

	count := 0

	for _, token := range tokens {
		if len(token) <= 20 {
			count += 1
		}
	}

	if count == len(tokens) {
		return false
	}

	// Contains blacklisted path prefix
	if util.ContainsAny(tokens[0], blacklistedPaths) {
		return false
	}

	return true
}

func (s *newsAccumulator) OnComplete() {
	if closer, ok := s.robots.(interface{ Close() error }); ok {
		closer.Close()
	}
}

func (s *newsAccumulator) OnCommit(msg types.Message) error {
	var c message.CompositeAnalysis
	if err := json.Unmarshal(msg.Message, &c); err != nil {
		return err
	}
	if s.seenUrls == nil {
		return nil
	}
	return s.seenUrls.SetString(c.URI)
}
