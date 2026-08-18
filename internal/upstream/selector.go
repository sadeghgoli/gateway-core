package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sabzevar.ir/gateway-core/internal/models"
)

var ErrNoUpstream = errors.New("no healthy upstream")

type UpstreamSelector interface {
	Pick(ctx context.Context, gw models.Gateway) (models.Upstream, error)
	Report(id string, latency time.Duration, err error)
}

type stats struct {
	inflight  atomic.Int64
	fails     atomic.Int64
	ok        atomic.Int64
	unhealthy atomic.Bool
	mu        sync.Mutex
	lastErr   string
	lastCheck time.Time
	latency   time.Duration
	code      int
	status    string
}

type Selector struct {
	mu    sync.Mutex
	rr    map[string]*atomic.Uint64
	stats map[string]*stats
}

func NewSelector() *Selector {
	return &Selector{
		rr:    map[string]*atomic.Uint64{},
		stats: map[string]*stats{},
	}
}

func (s *Selector) Pick(_ context.Context, gw models.Gateway) (models.Upstream, error) {
	enabled := make([]models.Upstream, 0, len(gw.Upstreams))
	for _, u := range gw.Upstreams {
		if !u.Enabled {
			continue
		}
		st := s.get(u.ID)
		if st.unhealthy.Load() {
			continue
		}
		enabled = append(enabled, u)
	}
	if len(enabled) == 0 {
		for _, u := range gw.Upstreams {
			if u.Enabled {
				enabled = append(enabled, u)
			}
		}
	}
	if len(enabled) == 0 {
		return models.Upstream{}, ErrNoUpstream
	}
	if len(enabled) == 1 || gw.LBStrategy == "single" {
		s.get(enabled[0].ID).inflight.Add(1)
		return enabled[0], nil
	}

	s.mu.Lock()
	ctr, ok := s.rr[gw.ID]
	if !ok {
		ctr = &atomic.Uint64{}
		s.rr[gw.ID] = ctr
	}
	s.mu.Unlock()

	total := 0
	for _, u := range enabled {
		w := u.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	if total <= 0 {
		total = 1
	}
	n := int(ctr.Add(1)-1) % total
	acc := 0
	chosen := enabled[0]
	for _, u := range enabled {
		w := u.Weight
		if w <= 0 {
			w = 1
		}
		acc += w
		if n < acc {
			chosen = u
			break
		}
	}
	s.get(chosen.ID).inflight.Add(1)
	return chosen, nil
}

func (s *Selector) Report(id string, latency time.Duration, err error) {
	st := s.get(id)
	st.inflight.Add(-1)
	st.mu.Lock()
	st.latency = latency
	st.lastCheck = time.Now()
	st.mu.Unlock()
	if err != nil {
		st.fails.Add(1)
		st.mu.Lock()
		st.lastErr = err.Error()
		st.status = "down"
		st.mu.Unlock()
		if st.fails.Load() >= 5 {
			st.unhealthy.Store(true)
		}
		return
	}
	st.ok.Add(1)
	st.fails.Store(0)
	st.unhealthy.Store(false)
	st.mu.Lock()
	st.lastErr = ""
	st.status = "up"
	st.mu.Unlock()
}

func (s *Selector) Probe(u models.Upstream, healthPath string) {
	u.Normalize()
	st := s.get(u.ID)
	start := time.Now()
	host := u.TargetHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := u.TargetPort
	if port <= 0 {
		port = 80
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		s.mark(st, start, 0, "down", err)
		return
	}
	_ = conn.Close()

	path := u.HealthPath
	if path == "" {
		path = healthPath
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := strings.TrimRight(u.EffectiveURL(), "/") + path
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		s.mark(st, start, 0, "degraded", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		s.mark(st, start, 0, "degraded", fmt.Errorf("tcp ok, http: %w", err))
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		s.mark(st, start, resp.StatusCode, "down", fmt.Errorf("http %d", resp.StatusCode))
		return
	}
	s.mark(st, start, resp.StatusCode, "up", nil)
}

func (s *Selector) Snapshot(u models.Upstream, gatewayID string) models.HealthSnapshot {
	st := s.get(u.ID)
	st.mu.Lock()
	defer st.mu.Unlock()
	status := st.status
	if status == "" {
		status = "unknown"
	}
	return models.HealthSnapshot{
		UpstreamID: u.ID,
		GatewayID:  gatewayID,
		Target:     u.EffectiveURL(),
		Status:     status,
		Healthy:    status == "up",
		LatencyMS:  st.latency.Milliseconds(),
		StatusCode: st.code,
		LastError:  st.lastErr,
		LastCheck:  st.lastCheck,
		Fails:      st.fails.Load(),
	}
}

func (s *Selector) mark(st *stats, start time.Time, code int, status string, err error) {
	st.mu.Lock()
	st.lastCheck = time.Now()
	st.latency = time.Since(start)
	st.code = code
	st.status = status
	if err != nil {
		st.lastErr = err.Error()
	} else {
		st.lastErr = ""
	}
	st.mu.Unlock()
	if status == "down" {
		st.fails.Add(1)
		if st.fails.Load() >= 2 {
			st.unhealthy.Store(true)
		}
		return
	}
	if status == "up" {
		st.fails.Store(0)
		st.unhealthy.Store(false)
		st.ok.Add(1)
	}
}

func (s *Selector) get(id string) *stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.stats[id]
	if !ok {
		st = &stats{status: "unknown"}
		s.stats[id] = st
	}
	return st
}
