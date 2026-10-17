package scan

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// gateways возвращает адреса шлюзов по умолчанию — это роутеры сети.
func gateways(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	switch runtime.GOOS {
	case "linux":
		for _, gw := range procRouteGateways() {
			out[gw] = true
		}
	case "windows":
		if b, err := exec.CommandContext(ctx, "route", "print", "-4", "0.0.0.0").Output(); err == nil {
			for _, gw := range parseRoutePrint(string(b)) {
				out[gw] = true
			}
		}
	}
	return out
}

// Строка маршрута по умолчанию в "route print":
//
//	0.0.0.0          0.0.0.0      192.168.0.1    192.168.0.219     25
var defaultRoute = regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+(\d{1,3}(?:\.\d{1,3}){3})\s`)

func parseRoutePrint(s string) []string {
	var out []string
	for _, m := range defaultRoute.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// /proc/net/route: шлюз записан в hex в обратном порядке байт.
func procRouteGateways() []string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 3 || fl[1] != "00000000" || fl[2] == "00000000" {
			continue
		}
		b, err := hex.DecodeString(fl[2])
		if err != nil || len(b) != 4 {
			continue
		}
		ip := make(net.IP, 4)
		binary.LittleEndian.PutUint32(ip, binary.BigEndian.Uint32(b))
		out = append(out, ip.String())
	}
	return out
}
