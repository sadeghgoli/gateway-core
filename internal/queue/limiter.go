package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"sabzevar.ir/gateway-core/internal/models"
)

var (
	ErrQueueFull = errors.New("gateway queue full")
	ErrTimeout   = errors.New("gateway queue timeout")
)

type Limiter struct {
	maxConc   int
	queueSize int
	timeout   time.Duration
	rps       int
	sem       chan struct{}
	rate      *rate.Limiter
	inflight  atomic.Int64
	waiting   atomic.Int64
	rejected  atomic.Int64
}

func newLimiter(g models.Gateway) *Limiter {
	maxC := g.MaxConcurrency
	if maxC <= 0 {
		maxC = 100
	}
	l := &Limiter{
		maxConc:   maxC,
		queueSize: g.QueueSize,
		timeout:   time.Duration(g.QueueTimeoutMS) * time.Millisecond,
		rps:       g.RPS,
		sem:       make(chan struct{}, maxC),
	}
	if g.RPS > 0 {
		l.rate = rate.NewLimiter(rate.Limit(g.RPS), g.RPS)
	}
	if l.timeout <= 0 {
		l.timeout = 5 * time.Second
	}
	return l
}

func (l *Limiter) Acquire(ctx context.Context) (func(), error) {
	if l.rate != nil {
		wctx, cancel := context.WithTimeout(ctx, l.timeout)
		err := l.rate.Wait(wctx)
		cancel()
		if err != nil {
			l.rejected.Add(1)
			return nil, ErrTimeout
		}
	}
	release := func() {
		<-l.sem
		l.inflight.Add(-1)
	}
	select {
	case l.sem <- struct{}{}:
		l.inflight.Add(1)
		return release, nil
	default:
	}
	if l.queueSize <= 0 {
		l.rejected.Add(1)
		return nil, ErrQueueFull
	}
	waiting := l.waiting.Add(1)
	if int(waiting) > l.queueSize {
		l.waiting.Add(-1)
		l.rejected.Add(1)
		return nil, ErrQueueFull
	}
	timer := time.NewTimer(l.timeout)
	defer timer.Stop()
	select {
	case l.sem <- struct{}{}:
		l.waiting.Add(-1)
		l.inflight.Add(1)
		return release, nil
	case <-timer.C:
		l.waiting.Add(-1)
		l.rejected.Add(1)
		return nil, ErrTimeout
	case <-ctx.Done():
		l.waiting.Add(-1)
		l.rejected.Add(1)
		return nil, ctx.Err()
	}
}

func (l *Limiter) Snapshot() (inflight, waiting, rejected int64) {
	return l.inflight.Load(), l.waiting.Load(), l.rejected.Load()
}

type Manager struct {
	mu   sync.Mutex
	byID map[string]*Limiter
}

func NewManager() *Manager {
	return &Manager{byID: map[string]*Limiter{}}
}

func (m *Manager) For(g models.Gateway) *Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.byID[g.ID]; ok &&
		l.maxConc == g.MaxConcurrency &&
		l.queueSize == g.QueueSize &&
		l.rps == g.RPS &&
		l.timeout == time.Duration(g.QueueTimeoutMS)*time.Millisecond {
		return l
	}
	l := newLimiter(g)
	m.byID[g.ID] = l
	return l
}

func (m *Manager) Snapshot(g models.Gateway) (inflight, waiting, rejected int64) {
	m.mu.Lock()
	l := m.byID[g.ID]
	m.mu.Unlock()
	if l == nil {
		return 0, 0, 0
	}
	return l.Snapshot()
}
