package scan

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

type arpEntry struct {
	IP  string
	MAC string
}

func readARP(ctx context.Context) ([]arpEntry, error) {
	if runtime.GOOS == "linux" {
		return readProcARP()
	}
	out, err := exec.CommandContext(ctx, "arp", "-a").Output()
	if err != nil {
		return nil, err
	}
	return parseArpA(string(out)), nil
}

// /proc/net/arp:
// IP address       HW type     Flags       HW address            Mask     Device
// 192.168.1.10     0x1         0x2         aa:bb:cc:dd:ee:ff     *        br-lan
func readProcARP() ([]arpEntry, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []arpEntry
	sc := bufio.NewScanner(f)
	sc.Scan() // заголовок
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 4 || fl[2] == "0x0" { // 0x0 — запись не завершена, устройство не ответило
			continue
		}
		out = append(out, arpEntry{IP: fl[0], MAC: NormalizeMAC(fl[3])})
	}
	return out, sc.Err()
}

// Вывод "arp -a" на Windows и macOS. Заголовки локализованы, поэтому берём
// из каждой строки только IP и MAC:
//
//	192.168.0.1           d0-27-88-df-d7-20     динамический
//	? (192.168.0.1) at d0:27:88:df:d7:20 on en0 ifscope [ethernet]
var arpLine = regexp.MustCompile(`(\d{1,3}(?:\.\d{1,3}){3})\)?\s+(?:at\s+)?([0-9a-fA-F]{1,2}(?:[:-][0-9a-fA-F]{1,2}){5})`)

func parseArpA(out string) []arpEntry {
	var res []arpEntry
	for _, line := range strings.Split(out, "\n") {
		if m := arpLine.FindStringSubmatch(line); m != nil {
			res = append(res, arpEntry{IP: m[1], MAC: NormalizeMAC(padMAC(m[2]))})
		}
	}
	return res
}

// macOS пишет октеты без ведущего нуля: 0:1b:2:...
func padMAC(mac string) string {
	sep := ":"
	if strings.Contains(mac, "-") {
		sep = "-"
	}
	parts := strings.Split(mac, sep)
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}

// readNeighbors6 читает таблицу соседей IPv6 (только Linux):
//
//	2a02:6b8::5 dev br-lan lladdr aa:bb:cc:dd:ee:ff REACHABLE
func readNeighbors6(ctx context.Context) map[string][]string {
	if runtime.GOOS != "linux" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "ip", "-6", "neigh", "show").Output()
	if err != nil {
		return nil
	}
	return parseNeigh6(string(out))
}

func parseNeigh6(s string) map[string][]string {
	res := map[string][]string{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || strings.HasPrefix(f[0], "fe80:") || strings.Contains(line, "FAILED") || strings.Contains(line, "INCOMPLETE") {
			continue
		}
		for i := 1; i+1 < len(f); i++ {
			if f[i] == "lladdr" {
				mac := NormalizeMAC(f[i+1])
				res[mac] = append(res[mac], f[0])
			}
		}
	}
	return res
}

// LAN6Prefixes — глобальные и ULA IPv6-префиксы на интерфейсах этой машины.
// Устройства в тех же сетях считаются локальными.
func LAN6Prefixes() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || hasPrefixAny(strings.ToLower(ifc.Name), skipIface) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() != nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			bits, _ := ipn.Mask.Size()
			if bits < 48 { // слишком широкий префикс — не сеть «рядом»
				bits = 64
			}
			out = append(out, (&net.IPNet{IP: ipn.IP.Mask(net.CIDRMask(bits, 128)), Mask: net.CIDRMask(bits, 128)}).String())
		}
	}
	return out
}
