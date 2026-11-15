package message

import "github.com/iakinsey/delver/types"

type FetcherRequest struct {
	ScopeRoot          string         `json:"scope_root,omitempty"`
	FrontierGeneration types.UUID     `json:"frontier_generation,omitempty"`
	RequestID          types.UUID     `json:"request_id,omitempty"`
	URI                string         `json:"uri,omitempty"`
	Host               string         `json:"host,omitempty"`
	Origin             string         `json:"origin,omitempty"`
	Protocol           types.Protocol `json:"protocol,omitempty"`
	Hops               int            `json:"hops,omitempty"`
	Depth              int            `json:"depth,omitempty"`
}

// BetterTraversalThan prefers fewer cross-site hops, then shallower site depth.
func (r FetcherRequest) BetterTraversalThan(other FetcherRequest) bool {
	return r.Hops < other.Hops || (r.Hops == other.Hops && r.Depth < other.Depth)
}
