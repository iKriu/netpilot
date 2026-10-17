package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"netpilot/internal/model"
)

// Ручная проверка: NETPILOT_LIVE_PING="1.1.1.1 ya.ru" go test -run LivePing -v
func TestLivePing(t *testing.T) {
	hosts := strings.Fields(os.Getenv("NETPILOT_LIVE_PING"))
	if len(hosts) == 0 {
		t.Skip("NETPILOT_LIVE_PING не задан")
	}
	for _, host := range hosts {
		n := model.Node{ID: host, Server: host, Port: 443}
		i := icmpPing(context.Background(), n)
		c := tcpPing(context.Background(), n)
		t.Logf("%-12s icmp=%d мс %q · tcp=%d мс %q", host, i.Ms, i.Error, c.Ms, c.Error)
	}
}
