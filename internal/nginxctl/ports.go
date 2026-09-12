package nginxctl

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"sabzevar.ir/gateway-core/internal/models"
)

const (
	DefaultDomainPortStart = 8000
	DefaultDomainPortMax   = 8999
)

func DomainPortRange(settings models.NginxSettings) (start, max int) {
	start = settings.DomainPortStart
	max = settings.DomainPortMax
	if start <= 0 {
		start = DefaultDomainPortStart
	}
	if max < start {
		max = start + 999
	}
	if max > 65535 {
		max = 65535
	}
	return start, max
}

func ReservedPorts(settings models.NginxSettings) map[int]struct{} {
	out := map[int]struct{}{
		80:  {},
		443: {},
	}
	if settings.ListenHTTP > 0 {
		out[settings.ListenHTTP] = struct{}{}
	}
	if settings.ListenHTTPS > 0 {
		out[settings.ListenHTTPS] = struct{}{}
	}
	if settings.ListenAdminHTTPS > 0 {
		out[settings.ListenAdminHTTPS] = struct{}{}
	}
	if _, p, ok := strings.Cut(settings.GatewayUpstream, ":"); ok {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			out[n] = struct{}{}
		}
	}
	return out
}

func TCPPortFree(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	time.Sleep(20 * time.Millisecond)
	return true
}

func PickPort(start, max, preferred int, used, reserved map[int]struct{}, keepIfBusy int, free func(int) bool) (int, error) {
	if free == nil {
		free = TCPPortFree
	}
	busy := func(p int) bool {
		if p < 1 || p > 65535 {
			return true
		}
		if p == keepIfBusy {
			return false
		}
		if reserved != nil {
			if _, ok := reserved[p]; ok {
				return true
			}
		}
		if used != nil {
			if _, ok := used[p]; ok {
				return true
			}
		}
		return !free(p)
	}
	if preferred > 0 && !busy(preferred) {
		return preferred, nil
	}
	if start <= 0 {
		start = DefaultDomainPortStart
	}
	if max < start {
		max = start + 999
	}
	for p := start; p <= max; p++ {
		if !busy(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("پورت خالی از %d تا %d پیدا نشد", start, max)
}

func TryOpenHostPort(port int) {
	if port < 1 || port > 65535 {
		return
	}
	p := strconv.Itoa(port)
	_ = exec.Command("firewall-cmd", "--permanent", "--add-port="+p+"/tcp").Run()
	_ = exec.Command("firewall-cmd", "--reload").Run()
	_ = exec.Command("semanage", "port", "-a", "-t", "http_port_t", "-p", "tcp", p).Run()
	_ = exec.Command("semanage", "port", "-m", "-t", "http_port_t", "-p", "tcp", p).Run()
}

// TryOpenHTTPServices opens only standard HTTP/HTTPS (shared-443 model).
func TryOpenHTTPServices() {
	_ = exec.Command("firewall-cmd", "--permanent", "--add-service=http").Run()
	_ = exec.Command("firewall-cmd", "--permanent", "--add-service=https").Run()
	_ = exec.Command("firewall-cmd", "--reload").Run()
}

func AssignGatewayPorts(settings models.NginxSettings, gateways []models.Gateway, free func(int) bool) (changed map[string]int, err error) {
	start, max := DomainPortRange(settings)
	reserved := ReservedPorts(settings)
	used := map[int]struct{}{}
	for _, g := range gateways {
		if g.ListenPort > 0 {
			used[g.ListenPort] = struct{}{}
		}
	}
	changed = map[string]int{}
	for i := range gateways {
		g := &gateways[i]
		if !g.Enabled {
			continue
		}
		if g.ListenPort > 0 {
			continue
		}
		port, perr := PickPort(start, max, 0, used, reserved, 0, free)
		if perr != nil {
			return changed, perr
		}
		g.ListenPort = port
		used[port] = struct{}{}
		if g.ID != "" {
			changed[g.ID] = port
		}
	}
	return changed, nil
}
