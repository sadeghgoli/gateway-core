package nginxctl

import (
	"net"
	"testing"

	"sabzevar.ir/gateway-core/internal/models"
)

func TestPickPortSkipsReservedAndUsed(t *testing.T) {
	busy := map[int]bool{8000: true, 8001: true}
	free := func(p int) bool { return !busy[p] }
	used := map[int]struct{}{8004: {}}
	reserved := map[int]struct{}{8002: {}, 8003: {}}
	got, err := PickPort(8000, 8010, 0, used, reserved, 0, free)
	if err != nil {
		t.Fatal(err)
	}
	if got != 8005 {
		t.Fatalf("got %d want 8005", got)
	}
}

func TestPickPortKeepsOwnBusyPort(t *testing.T) {
	free := func(int) bool { return false }
	got, err := PickPort(8000, 8010, 8004, nil, map[int]struct{}{8003: {}}, 8004, free)
	if err != nil {
		t.Fatal(err)
	}
	if got != 8004 {
		t.Fatalf("got %d want keep 8004", got)
	}
}

func TestPickPortPreferredIfFree(t *testing.T) {
	got, err := PickPort(8000, 8010, 8006, nil, map[int]struct{}{8003: {}}, 0, func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got != 8006 {
		t.Fatalf("got %d want 8006", got)
	}
}

func TestTCPPortFreeDetectsListener(t *testing.T) {
	ln, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if TCPPortFree(port) {
		t.Fatalf("port %d should be busy", port)
	}
}

func TestAssignGatewayPortsFrom8000(t *testing.T) {
	ns := models.NginxSettings{
		DomainPortStart:  8000,
		ListenAdminHTTPS: 8003,
		GatewayUpstream:  "127.0.0.1:8002",
	}
	list := []models.Gateway{
		{ID: "a", Enabled: true},
		{ID: "b", Enabled: true},
		{ID: "c", Enabled: true},
	}
	busy := map[int]bool{}
	changed, err := AssignGatewayPorts(ns, list, func(p int) bool { return !busy[p] })
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ListenPort != 8000 || list[1].ListenPort != 8001 || list[2].ListenPort != 8004 {
		t.Fatalf("ports=%d,%d,%d want 8000,8001,8004 (skip 8002/8003)", list[0].ListenPort, list[1].ListenPort, list[2].ListenPort)
	}
	if changed["a"] != 8000 || changed["c"] != 8004 {
		t.Fatalf("changed=%v", changed)
	}
}
