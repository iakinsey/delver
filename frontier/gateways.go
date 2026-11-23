package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/iakinsey/delver/site"
	"github.com/iakinsey/delver/types/features"
	"github.com/iakinsey/delver/types/message"
)

const gatewayRepresentatives = 5
const gatewayReason = "duplicate_gateway_structure_and_destination"

type GatewayCluster struct {
	Destination     string   `json:"destination"`
	Representatives []string `json:"representatives"`
	Members         int      `json:"members"`
	Suppressed      int      `json:"suppressed_members"`
}

// ObserveGateway checks successful analysis from both seeds and discoveries.
// One distinct normalized destination must account for every usable link.
func (s *Store) ObserveGateway(ctx context.Context, c message.CompositeAnalysis, links features.URIs) (bool, error) {
	p := s.params.Traps
	if p == nil || !p.Enabled || !c.Success || c.HTTPCode < 200 || c.HTTPCode >= 300 {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	base := c.EffectiveURI
	if base == "" {
		base = c.URI
	}
	host := site.Key(base)
	// A redirect does not establish the source host's page structure.
	if host == "" || host != site.Key(c.URI) {
		return false, nil
	}
	destination := ""
	for _, link := range links {
		target, err := Normalize(link, base)
		if err != nil {
			continue
		}
		if site.Key(target) == host || (destination != "" && target != destination) {
			return false, nil
		}
		destination = target
	}
	if destination == "" {
		return false, nil
	}
	var a features.Adversarial
	if !c.LoadPermissive(features.AdversarialField, &a) || a.Structure == nil {
		return false, nil
	}
	o := a.Structure
	if o.Site != host || o.Tokens < 5 || len(o.Signature) != 64 {
		return false, nil
	}
	signature, err := json.Marshal(o.Signature)
	if err != nil {
		return false, err
	}
	id := fmt.Sprintf("%x", sha256.Sum256(append(append(signature, 0), destination...)))
	clusterKey := "trap/gateway/" + id
	memberKey := "trap/gateway-member/" + host + "/" + id
	exclusionKey := "trap/blocked/" + host
	blocked := false
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.update(func(tx *badger.Txn) error {
		var cluster GatewayCluster
		if err := read(tx, clusterKey, &cluster); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		var representative bool
		switch _, err := tx.Get([]byte(memberKey)); err {
		case nil:
			if err := read(tx, memberKey, &representative); err != nil {
				return err
			}
		case badger.ErrKeyNotFound:
			cluster.Destination = destination
			cluster.Members++
			representative = len(cluster.Representatives) < gatewayRepresentatives
			if representative {
				cluster.Representatives = append(cluster.Representatives, host)
			}
			if err := write(tx, memberKey, representative); err != nil {
				return err
			}
			if err := write(tx, clusterKey, cluster); err != nil {
				return err
			}
		default:
			return err
		}
		if representative || !p.Automatic {
			return nil
		}
		if _, err := tx.Get([]byte("trap/allow/" + host)); err != badger.ErrKeyNotFound {
			return err
		}
		var previous TrapDecision
		if err := read(tx, exclusionKey, &previous); err != badger.ErrKeyNotFound {
			blocked = err == nil && previous.Reason == gatewayReason && previous.Family == id
			return err
		}
		blocked = true
		cluster.Suppressed++
		if err := write(tx, clusterKey, cluster); err != nil {
			return err
		}
		return write(tx, exclusionKey, TrapDecision{Site: host, Family: id, At: time.Now().UTC(),
			Samples: 1, Similarity: 1, CrossSiteLinks: 1, Reason: gatewayReason, Settings: *p})
	})
	return blocked, err
}
