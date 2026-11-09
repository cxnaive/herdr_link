package gateway

import (
	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
	"github.com/coder/websocket"
)

func (s *Server) limits() gatewayaccess.Limits {
	fallback := gatewayaccess.Limits{MaxRelays: s.opts.MaxRelays, MaxClients: s.opts.MaxClients, MaxClientsPerRelay: s.opts.MaxClientsPerRelay}
	if s.opts.Access != nil {
		return s.opts.Access.Limits(fallback)
	}
	return fallback
}

// State changes commit to disk before we cut the transport. New admissions are
// fenced by credential epoch; reconnects cannot race a disable/revoke operation.
func (s *Server) disconnectStale(id string) {
	s.mu.Lock()
	link := s.relays[id]
	if link != nil && !s.opts.Access.Admitted(id, link.authEpoch) {
		delete(s.relays, id)
	} else {
		link = nil
	}
	s.mu.Unlock()
	if link != nil {
		epoch := s.opts.Access.Snapshot().Computers[id].Epoch
		_ = s.opts.Access.Seen(id, epoch, s.now())
		link.detachClients("computer_access_revoked")
		link.close(websocket.StatusPolicyViolation, "computer_access_revoked")
	}
}

type managedComputer struct {
	gatewayaccess.Computer
	Online      bool   `json:"online"`
	Clients     int    `json:"clients"`
	ConnectedAt int64  `json:"connected_at,omitempty"`
	Bytes       uint64 `json:"relayed_bytes"`
}

func (s *Server) managementSnapshot() any {
	state := s.opts.Access.Snapshot()
	s.mu.Lock()
	links := make(map[string]*relayLink, len(s.relays))
	for id, link := range s.relays {
		links[id] = link
	}
	s.mu.Unlock()
	computers := make([]managedComputer, 0, len(state.Computers))
	for id, c := range state.Computers {
		row := managedComputer{Computer: c}
		if link := links[id]; link != nil {
			link.mu.Lock()
			row.Clients = len(link.conns)
			link.mu.Unlock()
			row.Online = s.opts.Access.Admitted(id, link.authEpoch)
			row.ConnectedAt = link.connectedAt.UnixMilli()
		}
		s.quota.mu.Lock()
		if entry := s.quota.relays[id]; entry != nil && entry.Month == monthKey(s.now()) {
			row.Bytes = entry.RelayedBytes
		}
		s.quota.mu.Unlock()
		computers = append(computers, row)
	}
	return map[string]any{"computers": computers, "invites": state.Invites, "audit": state.Audit, "limits": s.limits(), "version": s.opts.Version, "revision": s.opts.Revision, "month": monthKey(s.now()), "monthly_bytes": s.opts.MonthlyBytes}
}
