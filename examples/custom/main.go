package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/iakinsey/delver/application"
	"github.com/iakinsey/delver/extractors"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/message"
	"github.com/iakinsey/delver/worker"
)

type wordCount struct{}

func (wordCount) Name() string       { return "word_count" }
func (wordCount) Requires() []string { return []string{"text"} }
func (wordCount) Perform(_ *os.File, analysis message.CompositeAnalysis) (interface{}, error) {
	var text string
	if err := analysis.Load("text", &text); err != nil {
		return nil, err
	}
	return len(strings.Fields(text)), nil
}

type countedPassThrough struct{ counts *sync.Map }

func (s *countedPassThrough) OnMessage(msg types.Message) (interface{}, error) {
	s.counts.Store(msg.ID, true)
	return msg, nil
}
func (s *countedPassThrough) OnComplete() {}

func main() {
	extractors.Register("word_count", []string{"text"}, func() extractors.Extractor { return wordCount{} })
	application.RegisterResource("metadata_map", func(json.RawMessage) (interface{}, error) { return &sync.Map{}, nil })
	application.RegisterWorker("counted_passthrough", func(raw json.RawMessage, resources map[string]interface{}) (worker.Worker, error) {
		var params struct {
			Metadata string `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}
		counts, ok := resources[params.Metadata].(*sync.Map)
		if !ok {
			return nil, fmt.Errorf("metadata must reference metadata_map")
		}
		return &countedPassThrough{counts: counts}, nil
	})
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: custom CONFIG.json")
		os.Exit(2)
	}
	app, err := application.Load(os.Args[1])
	if err == nil {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		err = application.Run(ctx, app)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
