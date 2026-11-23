package extractor

import (
	"context"
	"github.com/iakinsey/delver/config"
	"github.com/iakinsey/delver/types/message"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"strings"
	"testing"
)

func BenchmarkSharedPageAnalysis(b *testing.B) {
	old := logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(old)
	for _, input := range []struct{ name, fragment string }{{"html", `<article><h1>Fixture</h1><a href="/next">Next</a><p>Words &amp; text</p></article>`}, {"malformed", `<div><a href="/next">Next<p>Words &amp; text</div><p><b>open`}, {"unicode", `<section><a href="/next">界 É</a><p>Ｔｅｘｔ&nbsp;界</p></section>`}} {
		b.Run(input.name, func(b *testing.B) {
			path := b.TempDir() + "/body"
			body := `<html><head><title>Fixture</title></head><body>` + strings.Repeat(input.fragment, 2048) + `</body></html>`
			if e := os.WriteFile(path, []byte(body), 0600); e != nil {
				b.Fatal(e)
			}
			w := NewCompositeExtractorWorkerWithConfig(CompositeArgs{Enabled: []string{"url", "text", "title", "adversarial"}}, config.LoadConfig()).(*compositeExtractor)
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, e := w.executeExtractorsContext(context.Background(), path, message.FetcherResponse{FetcherRequest: message.FetcherRequest{URI: "http://fixture.invalid/page"}})
				if e != nil {
					b.Fatal(e)
				}
				if len(out.Features) != 4 {
					b.Fatal(out.Features)
				}
			}
		})
	}
}
