package monitor

import (
	"sync"
	"time"
)

type PollStatus string

const (
	PollStarting PollStatus = "starting"
	PollHealthy  PollStatus = "ok"
	PollStale    PollStatus = "polling_stale"
)

// PollingHealth records confirmed successful getUpdates responses.
type PollingHealth struct {
	mu          sync.RWMutex
	lastSuccess time.Time
	staleAfter  time.Duration
}

func NewPollingHealth(staleAfter time.Duration) *PollingHealth {
	return &PollingHealth{staleAfter: staleAfter}
}

func (h *PollingHealth) RecordSuccess(at time.Time) {
	h.mu.Lock()
	h.lastSuccess = at
	h.mu.Unlock()
}

func (h *PollingHealth) Status(now time.Time) PollStatus {
	h.mu.RLock()
	lastSuccess, staleAfter := h.lastSuccess, h.staleAfter
	h.mu.RUnlock()
	if lastSuccess.IsZero() {
		return PollStarting
	}
	if now.Sub(lastSuccess) >= staleAfter {
		return PollStale
	}
	return PollHealthy
}
