package trap

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCalibratedDefaultConfiguration(t *testing.T) {
	raw, e := os.ReadFile("testdata/calibrated-defaults.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected Settings
	if e = json.Unmarshal(raw, &expected); e != nil {
		t.Fatal(e)
	}
	if expected != Default() {
		t.Fatalf("calibrated defaults changed: %+v", Default())
	}
	if e = expected.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestTrapSettingsConstraints(t *testing.T) {
	for name, change := range map[string]func(*Settings){
		"short confirmation window": func(s *Settings) { s.SiteWindow = 8 },
		"small family window":       func(s *Settings) { s.FamilyWindow = 10 },
		"negative novelty":          func(s *Settings) { s.Novelty = -.1 },
		"cross site fraction":       func(s *Settings) { s.CrossSiteLinks = 1.1 },
		"structure":                 func(s *Settings) { s.Similarity = .4 },
		"no links":                  func(s *Settings) { s.MinimumLinks = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			s := Default()
			change(&s)
			if s.Validate() == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}
