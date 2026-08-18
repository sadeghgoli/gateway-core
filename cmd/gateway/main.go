package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"sabzevar.ir/gateway-core/internal/admin"
	"sabzevar.ir/gateway-core/internal/config"
	"sabzevar.ir/gateway-core/internal/nginxctl"
	"sabzevar.ir/gateway-core/internal/proxy"
	"sabzevar.ir/gateway-core/internal/queue"
	"sabzevar.ir/gateway-core/internal/registry"
	"sabzevar.ir/gateway-core/internal/store"
	"sabzevar.ir/gateway-core/internal/upstream"
	webui "sabzevar.ir/gateway-core/web"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer st.Close()
	if err := st.Seed(cfg); err != nil {
		log.Fatalf("seed: %v", err)
	}

	reg := registry.New(st)
	if err := reg.Reload(); err != nil {
		log.Fatalf("registry: %v", err)
	}

	limiters := queue.NewManager()
	sel := upstream.NewSelector()
	px := proxy.New(reg, limiters, sel, st)

	static, err := fs.Sub(webui.Admin, "admin")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	adm := admin.New(cfg, st, reg, limiters, sel, nginxctl.New(cfg), static)
	adminHost := adm.Handler("")
	adminPrefixed := adm.Handler("/_admin")

	go healthLoop(reg, sel)

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
		if host == cfg.AdminHost {
			adminHost.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/_admin" {
			http.Redirect(w, r, "/_admin/", http.StatusFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_admin/") {
			adminPrefixed.ServeHTTP(w, r)
			return
		}
		px.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      root,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("gateway-core listening on %s (admin host %s)", cfg.Listen, cfg.AdminHost)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func healthLoop(reg *registry.Registry, sel *upstream.Selector) {
	run := func() {
		for _, g := range reg.All() {
			for _, u := range g.Upstreams {
				if u.Enabled {
					sel.Probe(u, g.HealthPath)
				}
			}
		}
	}
	run()
	t := time.NewTicker(12 * time.Second)
	defer t.Stop()
	for range t.C {
		run()
	}
}
