// gateway-replay classifies stored results without issuing network requests.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/iakinsey/delver/frontier"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
)

func main() {
	results := flag.String("results", "", "Path to results.jsonl")
	target := flag.String("apply-frontier", "", "Existing frontier directory for a stopped crawl; omit for report-only replay")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := replay(ctx, *results, *target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func replay(ctx context.Context, results, target string) (err error) {
	file, err := os.Open(results)
	if err != nil {
		return err
	}
	defer file.Close()
	// Snapshot the byte limit so an active crawl cannot extend a report-only replay.
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if target == "" {
		target, err = os.MkdirTemp("", "delver-gateway-replay-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(target)
	} else if _, err := os.Stat(target + "/MANIFEST"); err != nil {
		return fmt.Errorf("expected an existing crawl frontier: %w", err)
	}
	settings := trap.Default()
	store, err := frontier.OpenStore(frontier.StoreParams{Path: target, Traps: &settings})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	reader := bufio.NewReaderSize(file, 64*1024)
	for remaining := info.Size(); remaining > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if int64(len(line)) > remaining || err != nil && len(line) > 0 {
			// The snapshot can end inside the final result of an active crawl.
			break
		}
		if err != nil {
			return err
		}
		remaining -= int64(len(line))
		var envelope types.Message
		if json.Unmarshal(line, &envelope) != nil || envelope.MessageType != types.CompositeAnalysisType {
			continue
		}
		var analysis message.CompositeAnalysis
		if json.Unmarshal(envelope.Message, &analysis) != nil || !analysis.Success {
			continue
		}
		var links features.URIs
		if !analysis.LoadPermissive(features.UrlField, &links) || len(links) == 0 {
			continue
		}
		if _, err := store.ObserveGateway(ctx, analysis, links); err != nil {
			return err
		}
	}
	decisions, err := store.TrapDecisions(ctx)
	if err != nil {
		return err
	}
	clusters := map[string]*frontier.GatewayCluster{}
	for _, d := range decisions {
		if d.Gateway != nil {
			clusters[d.Family] = d.Gateway
		}
	}
	summary := make([]*frontier.GatewayCluster, 0, len(clusters))
	for _, c := range clusters {
		summary = append(summary, c)
	}
	sort.Slice(summary, func(i, j int) bool {
		if summary[i].Members != summary[j].Members {
			return summary[i].Members > summary[j].Members
		}
		return summary[i].Destination < summary[j].Destination
	})
	return json.NewEncoder(os.Stdout).Encode(summary)
}
