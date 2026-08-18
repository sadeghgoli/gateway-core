package registry

import (
	"strings"
	"sync"

	"sabzevar.ir/gateway-core/internal/models"
	"sabzevar.ir/gateway-core/internal/store"
)

type Registry struct {
	store  *store.Store
	mu     sync.RWMutex
	byHost map[string]*models.Gateway
	byID   map[string]*models.Gateway
}

func New(st *store.Store) *Registry {
	return &Registry{store: st}
}

func (r *Registry) Reload() error {
	list, err := r.store.ListGateways()
	if err != nil {
		return err
	}
	byHost := make(map[string]*models.Gateway, len(list))
	byID := make(map[string]*models.Gateway, len(list))
	for i := range list {
		g := list[i]
		cp := g
		byHost[strings.ToLower(g.Host)] = &cp
		byID[g.ID] = &cp
	}
	r.mu.Lock()
	r.byHost = byHost
	r.byID = byID
	r.mu.Unlock()
	return nil
}

func (r *Registry) ByHost(host string) *models.Gateway {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byHost[strings.ToLower(host)]
}

func (r *Registry) ByID(id string) *models.Gateway {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

func (r *Registry) All() []*models.Gateway {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*models.Gateway, 0, len(r.byID))
	for _, g := range r.byID {
		out = append(out, g)
	}
	return out
}
