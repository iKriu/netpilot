// Package scan находит устройства в локальной сети: будит соседей UDP-пакетами,
// читает таблицу ARP и узнаёт имена (DHCP, mDNS, обратный DNS, NetBIOS).
package scan

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Host struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Hostname  string `json:"hostname,omitempty"`
	Interface string `json:"interface,omitempty"`
	Gateway   bool   `json:"gateway,omitempty"` // шлюз по умолчанию — роутер сети
}

type Result struct {
	Hosts    []Host   `json:"hosts"`
	Networks []string `json:"networks"`
	Warnings []string `json:"warnings,omitempty"`
	// Isolated — программа не видит локальную сеть (контейнер с сетью Docker):
	// найденные «устройства» — служебные адреса Docker, добавлять их не нужно.
	Isolated bool `json:"isolated,omitempty"`
	// Neighbors6 — IPv6-адреса соседей по MAC (Linux): нужны, чтобы правила
	// «напрямую» и «без интернета» работали и для IPv6.
	Neighbors6 map[string][]string `json:"-"`
}

// Environment определяет, где запущена программа: от этого зависит,
// видит ли она локальную сеть и может ли управлять DHCP.
func Environment() string {
	switch {
	case runtime.GOOS == "windows":
		return "windows"
	case fileExists("/etc/openwrt_release"):
		return "openwrt"
	case fileExists("/.dockerenv"):
		return "docker"
	default:
		return runtime.GOOS
	}
}

// Scan находит устройства. active=true — сначала опросить все адреса подсетей
// (дольше, но находит спящие устройства); false — только прочитать ARP.
func Scan(ctx context.Context, active bool) (Result, error) {
	var res Result
	nets := localNetworks()
	for _, n := range nets {
		res.Networks = append(res.Networks, fmt.Sprintf("%s %s", n.iface, n.prefix))
	}
	if len(nets) == 0 {
		res.Warnings = append(res.Warnings, "Не найдено ни одной локальной сети")
	}
	// NETPILOT_ASSUME_LAN=1 — сеть 172.16.0.0/12 настоящая, а не служебная сеть Docker.
	if Environment() == "docker" && allDockerNets(nets) && os.Getenv("NETPILOT_ASSUME_LAN") != "1" {
		res.Isolated = true
		res.Warnings = append(res.Warnings,
			"Программа запущена в изолированном контейнере и не видит локальную сеть. "+
				"Запустите контейнер с network_mode: host на Linux (ZimaOS, OpenWrt) или используйте версию для Windows")
	}

	if active {
		probe(ctx, nets)
	}

	entries, err := readARP(ctx)
	if err != nil {
		return res, fmt.Errorf("таблица ARP: %w", err)
	}
	leases := readLeases()
	for mac, name := range dhcpNames() {
		if leases[mac] == "" {
			leases[mac] = name
		}
	}
	gw := gateways(ctx)
	res.Neighbors6 = readNeighbors6(ctx)

	own := map[string]bool{}
	for _, n := range nets {
		own[n.self.String()] = true
	}
	seen := map[string]bool{}
	for _, e := range entries {
		ip, err := netip.ParseAddr(e.IP)
		if err != nil || !ip.Is4() || own[e.IP] || seen[e.IP] || !usableMAC(e.MAC) {
			continue
		}
		n := networkOf(nets, ip)
		if n == nil {
			continue // соседи из чужих сетей (VPN, виртуальные адаптеры)
		}
		seen[e.IP] = true
		res.Hosts = append(res.Hosts, Host{IP: e.IP, MAC: e.MAC, Interface: n.iface, Hostname: leases[e.MAC], Gateway: gw[e.IP]})
	}

	resolveNames(ctx, res.Hosts)
	sort.Slice(res.Hosts, func(i, j int) bool {
		a, _ := netip.ParseAddr(res.Hosts[i].IP)
		b, _ := netip.ParseAddr(res.Hosts[j].IP)
		return a.Less(b)
	})
	return res, nil
}

type localNet struct {
	iface  string
	self   netip.Addr
	prefix netip.Prefix
}

// Виртуальные адаптеры (Hyper-V, WSL, Docker) сканировать бессмысленно.
var skipIface = []string{"vethernet", "veth", "docker", "br-", "virbr", "vmnet", "loopback", "tailscale", "wg", "tun", "utun"}

func localNetworks() []localNet {
	ifaces, _ := net.Interfaces()
	var out []localNet
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		if hasPrefixAny(name, skipIface) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP.To4())
			if !ok || !ip.IsPrivate() {
				continue
			}
			bits, _ := ipn.Mask.Size()
			// Большие сети (/16 у Docker) опрашиваем только в пределах своей /24.
			if bits < 22 {
				bits = 24
			}
			out = append(out, localNet{iface: ifc.Name, self: ip, prefix: netip.PrefixFrom(ip, bits).Masked()})
		}
	}
	return out
}

func networkOf(nets []localNet, ip netip.Addr) *localNet {
	for i := range nets {
		if nets[i].prefix.Contains(ip) {
			return &nets[i]
		}
	}
	return nil
}

// probe отправляет по UDP-пакету на каждый адрес подсети. Чтобы отправить
// пакет, система делает ARP-запрос, и ответившие устройства попадают в таблицу ARP.
// Прав администратора для этого не нужно.
func probe(ctx context.Context, nets []localNet) {
	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	for _, n := range nets {
		for ip := n.prefix.Addr().Next(); n.prefix.Contains(ip); ip = ip.Next() {
			if ip == n.self || isBroadcast(n.prefix, ip) {
				continue
			}
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(ip netip.Addr) {
				defer wg.Done()
				defer func() { <-sem }()
				c, err := net.DialTimeout("udp4", netip.AddrPortFrom(ip, 9).String(), time.Second)
				if err == nil {
					_, _ = c.Write([]byte{0})
					_ = c.Close()
				}
			}(ip)
		}
	}
	wg.Wait()
	// Ответы на ARP приходят не сразу.
	select {
	case <-ctx.Done():
	case <-time.After(1500 * time.Millisecond):
	}
}

// resolveNames узнаёт имена устройств, у которых их ещё нет: у самого
// устройства через mDNS (Apple, Android, принтеры), у роутера обратным DNS
// (он знает имена из DHCP), у системного DNS и через NetBIOS (Windows).
func resolveNames(ctx context.Context, hosts []Host) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var gws []string
	for _, h := range hosts {
		if h.Gateway {
			gws = append(gws, h.IP)
		}
	}
	var wg sync.WaitGroup
	for i := range hosts {
		if hosts[i].Hostname != "" {
			continue
		}
		wg.Add(1)
		go func(h *Host) {
			defer wg.Done()
			if n := mdnsName(ctx, h.IP); n != "" {
				h.Hostname = n
				return
			}
			for _, gw := range gws {
				if gw == h.IP {
					continue
				}
				if n := rdnsVia(ctx, gw, h.IP); n != "" {
					h.Hostname = n
					return
				}
			}
			if names, err := net.DefaultResolver.LookupAddr(ctx, h.IP); err == nil && len(names) > 0 {
				if n := cleanName(names[0]); n != "" {
					h.Hostname = n
					return
				}
			}
			if n := netbiosName(ctx, h.IP); n != "" {
				h.Hostname = n
			}
		}(&hosts[i])
	}
	wg.Wait()
}

func cleanName(n string) string {
	n = strings.TrimSuffix(n, ".")
	for _, suf := range []string{".lan", ".local", ".home", ".localdomain", ".router", ".home.arpa"} {
		n = strings.TrimSuffix(n, suf)
	}
	// Имена вида 192-168-1-5.provider.net ничего не говорят.
	if strings.ContainsAny(n, ".") || n == "" {
		return ""
	}
	return n
}

// ExtraLeaseFiles — дополнительные файлы аренд (DHCP-сервер режима роутера).
var ExtraLeaseFiles []string

// readLeases читает аренды dnsmasq (OpenWrt, режим роутера): MAC → имя.
func readLeases() map[string]string {
	out := map[string]string{}
	for _, path := range append([]string{"/tmp/dhcp.leases", "/var/lib/misc/dnsmasq.leases"}, ExtraLeaseFiles...) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			// срок mac ip имя client-id
			f := strings.Fields(line)
			if len(f) >= 4 && f[3] != "*" {
				out[NormalizeMAC(f[1])] = f[3]
			}
		}
	}
	return out
}

// NormalizeMAC приводит MAC к виду aa:bb:cc:dd:ee:ff.
func NormalizeMAC(mac string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	mac = strings.ReplaceAll(mac, "-", ":")
	if hw, err := net.ParseMAC(mac); err == nil {
		return hw.String()
	}
	return mac
}

// RandomMAC — локально администрируемый адрес: так телефоны прячут настоящий MAC.
func RandomMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	return err == nil && len(hw) > 0 && hw[0]&0x02 != 0
}

func usableMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return false
	}
	if hw[0]&0x01 != 0 { // групповой (multicast/broadcast)
		return false
	}
	return hw.String() != "00:00:00:00:00:00"
}

func isBroadcast(p netip.Prefix, ip netip.Addr) bool {
	a := ip.As4()
	host := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	mask := uint32(0xffffffff) >> p.Bits()
	return host&mask == mask
}

func allDockerNets(nets []localNet) bool {
	docker := netip.MustParsePrefix("172.16.0.0/12")
	for _, n := range nets {
		if !docker.Contains(n.self) {
			return false
		}
	}
	return true
}

func hasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// LocalAddrs — адреса этой машины в локальных сетях (без виртуальных адаптеров).
func LocalAddrs() []string {
	var out []string
	for _, n := range localNetworks() {
		out = append(out, n.self.String())
	}
	return out
}
