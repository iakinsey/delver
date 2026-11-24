// Package repetition describes related page content without changing site identity.
package repetition

import (
	"bytes"
	"container/heap"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/iakinsey/delver/site"
	"github.com/iakinsey/delver/trap"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
	"golang.org/x/text/unicode/norm"
)

type Settings struct {
	Mode                  string  `json:"mode"`
	MinimumSites          int     `json:"minimum_sites"`
	StructureSimilarity   float64 `json:"structure_similarity"`
	TextSimilarity        float64 `json:"text_similarity"`
	DestinationSimilarity float64 `json:"destination_similarity"`
	HostDominance         float64 `json:"host_dominance"`
	SiteWindow            int     `json:"site_window"`
	FamilyWindow          int     `json:"family_window"`
	MaxTextKiB            int     `json:"max_text_kib"`
	MaxDestinations       int     `json:"max_destinations"`
	AdmissionShare        int     `json:"admission_share_percent"`
}

func Default() Settings { return Settings{"deprioritize", 3, .95, .85, .8, .8, 20, 100, 256, 256, 10} }

// UnmarshalJSON applies defaults only when the feature is explicitly present.
func (s *Settings) UnmarshalJSON(raw []byte) error {
	type plain Settings
	p := plain(Default())
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return err
	}
	*s = Settings(p)
	return s.Validate()
}
func (s Settings) Validate() error {
	for _, v := range []float64{s.StructureSimilarity, s.TextSimilarity, s.DestinationSimilarity, s.HostDominance} {
		if math.IsNaN(v) {
			return fmt.Errorf("repeated-page thresholds must be finite numbers")
		}
	}
	if s.Mode != "off" && s.Mode != "observe" && s.Mode != "deprioritize" {
		return fmt.Errorf("repeated-page mode must be off, observe, or deprioritize")
	}
	if s.MinimumSites < 3 || s.MinimumSites > s.FamilyWindow || s.SiteWindow < 3 || s.SiteWindow > 200 || s.FamilyWindow < s.SiteWindow || s.FamilyWindow > 1000 {
		return fmt.Errorf("invalid repeated-page observation windows or minimum sites")
	}
	if s.StructureSimilarity < .5 || s.StructureSimilarity > 1 || s.TextSimilarity < .5 || s.TextSimilarity > 1 || s.DestinationSimilarity < .5 || s.DestinationSimilarity > 1 || s.HostDominance < .5 || s.HostDominance > 1 {
		return fmt.Errorf("repeated-page similarities and dominance must be between 0.5 and 1")
	}
	if s.MaxTextKiB < 4 || s.MaxTextKiB > 1024 || s.MaxDestinations < 16 || s.MaxDestinations > 4096 || s.AdmissionShare < 1 || s.AdmissionShare > 50 {
		return fmt.Errorf("repeated-page limits: text 4–1024 KiB, destinations 16–4096, admission share 1–50 percent")
	}
	return nil
}

type Profile struct {
	URI          string          `json:"uri"`
	Site         string          `json:"site"`
	Event        string          `json:"event"`
	Timestamp    int64           `json:"timestamp"`
	QueuedAt     int64           `json:"queued_at"`
	Structure    trap.HashVector `json:"structure"`
	Text         trap.HashVector `json:"text"`
	Destinations trap.HashVector `json:"destinations"`
}

var addresses = regexp.MustCompile(`(?i)https?://[^\s<>"']+|[\pL\pN][\pL\pN.\-]*\.[\pL][\pL\pN.\-]*`)

// Build never returns a truncated profile. Bounds failures are explicit and
// leave the page unclassified. Text has already passed normal protection.
func Build(c message.CompositeAnalysis, links []string, s Settings) (Profile, string) {
	p := Profile{URI: c.URI, Site: site.Key(c.URI), Event: string(c.RequestID), Timestamp: c.Timestamp, QueuedAt: c.QueuedAt}
	if p.Site == "" {
		return p, "invalid_uri"
	}
	if c.EffectiveURI != "" && site.Key(c.EffectiveURI) != p.Site {
		return p, "redirected"
	}
	if !c.Success || c.HTTPCode < 200 || c.HTTPCode >= 300 {
		return p, "unsuccessful"
	}
	var a features.Adversarial
	if !c.LoadPermissive(features.AdversarialField, &a) || a.Structure == nil || len(a.Structure.Signature) != 64 || a.Structure.Tokens < 5 {
		return p, "structure_unavailable"
	}
	var text string
	if !c.LoadPermissive(features.TextField, &text) {
		return p, "text_unavailable"
	}
	if len(text) > s.MaxTextKiB*1024 {
		return p, "text_limit"
	}
	hosts := map[string]bool{p.Site: true}
	dests := map[uint64]bool{}
	for _, raw := range links {
		u, e := url.Parse(raw)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			continue
		}
		key := site.Key(raw)
		if key == p.Site {
			continue
		}
		hosts[key] = true
		dests[hash(key)] = true
		if len(dests) > s.MaxDestinations {
			return p, "destination_limit"
		}
	}
	if len(dests) == 0 {
		return p, "destinations_unavailable"
	}
	text = strings.ToLower(norm.NFKC.String(text))
	text = addresses.ReplaceAllStringFunc(text, func(v string) string {
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") || hosts[site.Key(strings.TrimSuffix(v, "."))] {
			return " address "
		}
		return v
	})
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(words) < 5 {
		return p, "insufficient_text"
	}
	p.Structure = append(trap.HashVector{}, a.Structure.Signature...)
	p.Text = sketch(words)
	for h := range dests {
		p.Destinations = append(p.Destinations, h)
	}
	sort.Slice(p.Destinations, func(i, j int) bool { return p.Destinations[i] < p.Destinations[j] })
	return p, ""
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	v := h.Sum64()
	v ^= v >> 30
	v *= 0xbf58476d1ce4e5b9
	v ^= v >> 27
	v *= 0x94d049bb133111eb
	return v ^ (v >> 31)
}

type maxHeap []uint64

func (h maxHeap) Len() int            { return len(h) }
func (h maxHeap) Less(i, j int) bool  { return h[i] > h[j] }
func (h maxHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *maxHeap) Push(x interface{}) { *h = append(*h, x.(uint64)) }
func (h *maxHeap) Pop() interface{}   { a := *h; x := a[len(a)-1]; *h = a[:len(a)-1]; return x }
func sketch(words []string) trap.HashVector {
	h := &maxHeap{}
	kept := map[uint64]bool{}
	for i := 0; i+5 <= len(words); i++ {
		v := hash(strings.Join(words[i:i+5], "\x00"))
		if kept[v] || len(*h) == 128 && v >= (*h)[0] {
			continue
		}
		if len(*h) == 128 {
			delete(kept, heap.Pop(h).(uint64))
		}
		heap.Push(h, v)
		kept[v] = true
	}
	out := trap.HashVector(*h)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TextSimilarity is a bottom-k union estimator; short documents retain all
// shingles and consequently use exact Jaccard similarity.
func TextSimilarity(a, b []uint64) float64        { return similarity(a, b, 128) }
func DestinationSimilarity(a, b []uint64) float64 { return similarity(a, b, 0) }
func similarity(a, b []uint64, limit int) float64 {
	i, j, n, shared := 0, 0, 0, 0
	for (i < len(a) || j < len(b)) && (limit == 0 || n < limit) {
		n++
		if i < len(a) && j < len(b) && a[i] == b[j] {
			shared++
			i++
			j++
		} else if j >= len(b) || i < len(a) && a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(shared) / float64(n)
}

type Measurement struct {
	Structure    float64 `json:"structure_similarity"`
	Text         float64 `json:"text_similarity"`
	Destinations float64 `json:"destination_similarity"`
}

func Compare(a, b Profile) Measurement {
	return Measurement{trap.Similarity(a.Structure, b.Structure), TextSimilarity(a.Text, b.Text), DestinationSimilarity(a.Destinations, b.Destinations)}
}
func (m Measurement) Qualifies(s Settings) bool {
	return m.Structure >= s.StructureSimilarity && m.Text >= s.TextSimilarity && m.Destinations >= s.DestinationSimilarity
}

type Family struct {
	Representative string      `json:"representative"`
	ID             string      `json:"id"`
	Sites          int         `json:"sites"`
	SupportSites   int         `json:"support_sites"`
	Epoch          string      `json:"epoch"`
	Confirmed      bool        `json:"confirmed"`
	Exempt         bool        `json:"exempt"`
	Observations   int         `json:"observations"`
	Measurements   Measurement `json:"measurements"`
}
type Host struct {
	Host               string  `json:"host,omitempty"`
	FamilyObservations int     `json:"family_observations,omitempty"`
	Eligible           int     `json:"eligible"`
	Reason             string  `json:"reason,omitempty"`
	URI                string  `json:"uri"`
	Site               string  `json:"site"`
	Family             string  `json:"family,omitempty"`
	Samples            int     `json:"samples"`
	Dominance          float64 `json:"dominance"`
	Epoch              string  `json:"epoch"`
}

// Update is a compact, replayable projection into the existing report ledger.
type Update struct {
	Hosts    []Host   `json:"hosts,omitempty"`
	Revision int64    `json:"revision"`
	Epoch    string   `json:"epoch"`
	Host     *Host    `json:"host,omitempty"`
	Families []Family `json:"families,omitempty"`
}
type Writer interface{ RepeatedPages([]Update) error }

// Turn apportions successful fresh admissions without floating-point drift.
func Turn(step uint64, share int) bool {
	n := step % 100
	return (n+1)*uint64(share)/100 > n*uint64(share)/100
}
