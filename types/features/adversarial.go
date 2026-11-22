package features

import "github.com/iakinsey/delver/trap"

type Adversarial struct {
	Structure          *trap.Observation `json:"structure,omitempty"`
	Enumeration        *bool             `json:"enumeration"`
	SubdomainExplosion *bool             `json:"subdomain_explosion"`
}
