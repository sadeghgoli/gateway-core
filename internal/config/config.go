package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen           string
	DBPath           string
	AdminHost        string
	AdminUser        string
	AdminPassword    string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
	NginxConfPath    string
	NginxTestCmd     string
	NginxReloadCmd   string
	NginxGatewayAddr string
}

func Load() Config {
	return Config{
		Listen:           env("GATEWAY_LISTEN", ":8080"),
		DBPath:           env("GATEWAY_DB", "./data/gateway.db"),
		AdminHost:        strings.ToLower(env("GATEWAY_ADMIN_HOST", "gateway-admin.sabzevar.ir")),
		AdminUser:        env("GATEWAY_ADMIN_USER", "admin"),
		AdminPassword:    env("GATEWAY_ADMIN_PASSWORD", "changeme"),
		ReadTimeout:      duration("GATEWAY_READ_TIMEOUT", 60*time.Second),
		WriteTimeout:     duration("GATEWAY_WRITE_TIMEOUT", 120*time.Second),
		IdleTimeout:      duration("GATEWAY_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:  duration("GATEWAY_SHUTDOWN_TIMEOUT", 15*time.Second),
		NginxConfPath:    env("GATEWAY_NGINX_CONF", "/etc/nginx/conf.d/gateway-managed.conf"),
		NginxTestCmd:     env("GATEWAY_NGINX_TEST", "nginx -t"),
		NginxReloadCmd:   env("GATEWAY_NGINX_RELOAD", "nginx -s reload"),
		NginxGatewayAddr: env("GATEWAY_NGINX_UPSTREAM", "127.0.0.1:8080"),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return fallback
}
