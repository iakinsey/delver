package extractors

import (
	"fmt"
	"os"

	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"github.com/pkg/errors"
	"golang.org/x/net/html"
)

type titleExtractor struct{}

func NewTitleExtractor() Extractor {
	return &titleExtractor{}
}

func (s *titleExtractor) Perform(f *os.File, composite message.CompositeAnalysis) (interface{}, error) {
	document, err := html.Parse(f)

	if err != nil {
		return nil, errors.Wrap(err, "failed to parse html document for title extraction")
	}

	if title, ok := seekTitle(document); ok {
		return features.Title(title), nil
	}

	return nil, nil
}

func (s *titleExtractor) Name() string {
	return message.TitleExtractor
}

func (s *titleExtractor) Requires() []string {
	return nil
}

func (s *titleExtractor) SetResult(result interface{}, composite *message.CompositeAnalysis) error {
	if result == nil {
		return nil
	}

	switch d := result.(type) {
	case features.Title:
		composite.Title = d
		return nil
	default:
		return fmt.Errorf("TitleExtractor: attempt to cast unknown type")
	}
}

func seekTitle(node *html.Node) (string, bool) {
	if node.Type == html.ElementNode && node.Data == "title" {
		return node.FirstChild.Data, true
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if title, ok := seekTitle(child); ok {
			return title, ok
		}
	}

	return "", false
}
