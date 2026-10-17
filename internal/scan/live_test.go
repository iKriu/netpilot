package scan

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Ручная проверка на живой сети: NETPILOT_LIVE_IPS="ip1 ip2" NETPILOT_LIVE_GW=192.168.0.1 go test -run Live
func TestLiveNames(t *testing.T) {
	ips := strings.Fields(os.Getenv("NETPILOT_LIVE_IPS"))
	if len(ips) == 0 {
		t.Skip("NETPILOT_LIVE_IPS не задан")
	}
	gw := os.Getenv("NETPILOT_LIVE_GW")
	for _, ip := range ips {
		m := mdnsName(context.Background(), ip)
		r := ""
		if gw != "" {
			r = rdnsVia(context.Background(), gw, ip)
		}
		t.Logf("%-15s mdns=%-25q router=%q", ip, m, r)
	}
}
