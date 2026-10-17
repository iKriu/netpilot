package router

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"netpilot/internal/core/external"
	"netpilot/internal/model"
)

const pppDev = "ppp-wan"

// linuxBackend — всё делает NetPilot: pppd или udhcpc для провайдера,
// dnsmasq для локальной сети, nftables для NAT и защиты.
type linuxBackend struct {
	dir string

	mu       sync.Mutex
	cfg      model.Router
	on       bool
	wan      external.Process
	dns      external.Process
	lanAdded string // адрес, который мы добавили на LAN-порт (его и уберём)
	wanAdded string // статический адрес на WAN-порту
	upSince  time.Time
}

func newLinux(dataDir string) *linuxBackend {
	return &linuxBackend{dir: filepath.Join(dataDir, "router")}
}

func (b *linuxBackend) Name() string        { return "NetPilot (pppd, udhcpc, dnsmasq, nftables)" }
func (b *linuxBackend) Interfaces() []Iface { return listIfaces() }
func (b *linuxBackend) LeaseFile() string   { return filepath.Join(b.dir, "dnsmasq.leases") }

func (b *linuxBackend) Check() Support {
	s := Support{Managed: "netpilot"}
	if runtime.GOOS != "linux" {
		s.Problems = []string{"Режим роутера работает на Linux: ZimaOS, OpenWrt и других"}
		return s
	}
	s.Available = true
	for bin, why := range map[string]string{
		"ip": "iproute2", "nft": "nftables", "dnsmasq": "dnsmasq (раздача адресов и DNS)", "udhcpc": "udhcpc (подключение по DHCP)",
	} {
		if _, err := exec.LookPath(bin); err != nil {
			s.Available = false
			s.Problems = append(s.Problems, "не найдена программа "+bin+" — "+why)
		}
	}
	if _, err := exec.LookPath("pppd"); err != nil || pppPlugin() == "" {
		s.Problems = append(s.Problems, "PPPoE недоступно: нет pppd с модулем pppoe")
	} else if _, err := os.Stat("/dev/ppp"); err != nil {
		s.Problems = append(s.Problems, "PPPoE недоступно: нет /dev/ppp (контейнеру нужно devices: /dev/ppp)")
	}
	return s
}

func (b *linuxBackend) Apply(cfg model.Router, leases []Lease) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.teardownLocked(cfg.LAN.Address)
	if !cfg.Enabled {
		return nil
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return err
	}
	w, l := cfg.WAN, cfg.LAN

	// Локальная сеть: адрес роутера добавляем к порту, ничего не удаляя —
	// через этот порт может идти и управление самим сервером.
	if err := run("ip", "link", "set", "dev", l.Interface, "up"); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", l.Address, "dev", l.Interface); err == nil {
		b.lanAdded = l.Address
	} else if !strings.Contains(err.Error(), "File exists") && !strings.Contains(err.Error(), "already assigned") {
		return fmt.Errorf("адрес локальной сети: %w", err)
	}

	// Порт провайдера.
	if w.MAC != "" {
		_ = run("ip", "link", "set", "dev", w.Interface, "down")
		if err := run("ip", "link", "set", "dev", w.Interface, "address", w.MAC); err != nil {
			return fmt.Errorf("клонирование MAC: %w", err)
		}
	}
	if err := run("ip", "link", "set", "dev", w.Interface, "up"); err != nil {
		return err
	}
	if w.MTU > 0 && w.Proto != "pppoe" {
		if err := run("ip", "link", "set", "dev", w.Interface, "mtu", fmt.Sprint(w.MTU)); err != nil {
			return fmt.Errorf("MTU: %w", err)
		}
	}

	resolv := filepath.Join(b.dir, "resolv.conf")
	if len(w.DNS) > 0 {
		var sb strings.Builder
		for _, d := range w.DNS {
			fmt.Fprintf(&sb, "nameserver %s\n", strings.TrimSpace(d))
		}
		if err := os.WriteFile(resolv, []byte(sb.String()), 0o644); err != nil {
			return err
		}
	} else if w.Proto == "pppoe" {
		resolv = pppResolv() // pppd с usepeerdns пишет DNS провайдера сюда
	}

	wanLog := filepath.Join(b.dir, "wan.log")
	switch w.Proto {
	case "static":
		if err := run("ip", "addr", "add", w.Address, "dev", w.Interface); err != nil && !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("адрес провайдера: %w", err)
		}
		b.wanAdded = w.Address
		if err := run("ip", "route", "replace", "default", "via", w.Gateway, "dev", w.Interface); err != nil {
			return fmt.Errorf("маршрут по умолчанию: %w", err)
		}
	case "dhcp":
		script := filepath.Join(b.dir, "udhcpc.sh")
		if err := os.WriteFile(script, []byte(udhcpcScript(filepath.Join(b.dir, "wan.state"), resolv, len(w.DNS) > 0)), 0o755); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(b.dir, "wan.state"))
		// -t 0: пытаться получить адрес бесконечно (кабель могут подключить позже).
		if err := b.wan.Start("udhcpc", []string{"-i", w.Interface, "-f", "-S", "-t", "0", "-T", "3", "-A", "5", "-s", script, "-x", "hostname:netpilot"}, b.dir, wanLog); err != nil {
			return fmt.Errorf("DHCP-клиент: %w", err)
		}
	case "pppoe":
		opts := filepath.Join(b.dir, "ppp.options")
		text, err := pppOptions(w)
		if err != nil {
			return err
		}
		// Пароль — в файле с правами 600, а не в командной строке, где его видно всем процессам.
		if err := os.WriteFile(opts, []byte(text), 0o600); err != nil {
			return err
		}
		if err := b.wan.Start("pppd", []string{"file", opts}, b.dir, wanLog); err != nil {
			return fmt.Errorf("PPPoE: %w", err)
		}
	}

	wanDevs := []string{w.Interface}
	if w.Proto == "pppoe" {
		wanDevs = append(wanDevs, pppDev)
	}
	if err := nftApply(natRuleset(wanDevs, l.Interface)); err != nil {
		return err
	}

	if err := writeHosts(filepath.Join(b.dir, "dhcp-hosts"), leases); err != nil {
		return err
	}
	if err := b.dns.Start("dnsmasq", dnsmasqArgs(l, b.dir, resolv), b.dir, filepath.Join(b.dir, "dnsmasq.log")); err != nil {
		return fmt.Errorf("dnsmasq: %w", err)
	}
	b.cfg, b.on, b.upSince = cfg, true, time.Time{}
	return nil
}

func (b *linuxBackend) Disable() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.teardownLocked("")
	return nil
}

// teardownLocked убирает всё, что настроили: процессы, NAT, добавленные адреса.
// keepLAN — адрес, который останется и в новой конфигурации (его не трогаем).
func (b *linuxBackend) teardownLocked(keepLAN string) {
	_ = b.wan.Stop()
	_ = b.dns.Stop()
	_ = run("nft", "delete", "table", "ip", natTable)
	_ = run("nft", "delete", "table", "inet", filterTable)
	if b.on {
		w := b.cfg.WAN
		switch w.Proto {
		case "static":
			if b.wanAdded != "" {
				_ = run("ip", "addr", "del", b.wanAdded, "dev", w.Interface)
			}
		case "dhcp":
			_ = run("ip", "-4", "addr", "flush", "dev", w.Interface)
		}
	}
	if b.lanAdded != "" && b.lanAdded != keepLAN {
		_ = run("ip", "addr", "del", b.lanAdded, "dev", b.cfg.LAN.Interface)
		b.lanAdded = ""
	}
	b.on, b.wanAdded = false, ""
}

func (b *linuxBackend) Reconnect() error {
	b.mu.Lock()
	cfg, on := b.cfg, b.on
	b.mu.Unlock()
	if !on {
		return errors.New("режим роутера выключен")
	}
	if cfg.WAN.Proto == "static" {
		return nil
	}
	// Полное повторное применение: простое и надёжное (секунда простоя).
	return b.Apply(cfg, readHosts(filepath.Join(b.dir, "dhcp-hosts")))
}

func (b *linuxBackend) UpdateLeases(leases []Lease) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.on {
		return nil
	}
	if err := writeHosts(filepath.Join(b.dir, "dhcp-hosts"), leases); err != nil {
		return err
	}
	return b.dns.Signal(syscall.SIGHUP) // dnsmasq перечитает файл без перезапуска
}

func (b *linuxBackend) Status(ctx context.Context) WANStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.on {
		return WANStatus{State: "disabled"}
	}
	w := b.cfg.WAN
	st := WANStatus{Proto: w.Proto, Device: w.Interface}
	switch w.Proto {
	case "pppoe":
		st.Device = pppDev
		st.IP, st.Gateway = pppAddrs()
		st.DNS = resolvServers(pppResolv())
	case "dhcp":
		state := readKV(filepath.Join(b.dir, "wan.state"))
		st.IP, st.Gateway = state["ip"], state["router"]
		st.DNS = strings.Fields(state["dns"])
	case "static":
		st.IP, st.Gateway = w.Address, w.Gateway
	}
	if len(w.DNS) > 0 {
		st.DNS = w.DNS
	}
	switch {
	case st.IP != "":
		st.State = "up"
		if b.upSince.IsZero() {
			b.upSince = time.Now()
		}
		st.Since = b.upSince.Unix()
	case w.Proto != "static" && !b.wan.Running():
		st.State = "error"
		st.Message = explainPPP(tailFile(filepath.Join(b.dir, "wan.log"), 4096))
		if st.Message == "" {
			st.Message = "Подключение остановилось, подробности — в журнале"
		}
		b.upSince = time.Time{}
	default:
		st.State = "connecting"
		st.Message = explainPPP(tailFile(filepath.Join(b.dir, "wan.log"), 2048))
		b.upSince = time.Time{}
	}
	st.RxBytes, st.TxBytes = readCounter(st.Device, "rx_bytes"), readCounter(st.Device, "tx_bytes")
	return st
}

// ---------- pppd ----------

func pppPlugin() string {
	for _, pattern := range []string{"/usr/lib/pppd/*/pppoe.so", "/usr/lib/pppd/*/rp-pppoe.so", "/usr/lib/*/pppd/*/pppoe.so"} {
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

// pppResolv — куда pppd записывает DNS провайдера: 2.5 — /run/ppp, 2.4 — /etc/ppp.
func pppResolv() string {
	if strings.Contains(pppPlugin(), "/2.4") {
		return "/etc/ppp/resolv.conf"
	}
	return "/run/ppp/resolv.conf"
}

func pppOptions(w model.WAN) (string, error) {
	plugin := pppPlugin()
	if plugin == "" {
		return "", errors.New("не найден модуль PPPoE для pppd")
	}
	return pppOptionsFor(plugin, w), nil
}

func pppOptionsFor(plugin string, w model.WAN) string {
	mtu := w.MTU
	if mtu == 0 {
		mtu = 1492
	}
	var b strings.Builder
	line := func(s string, args ...any) { fmt.Fprintf(&b, s+"\n", args...) }
	line("plugin %s", plugin)
	if strings.HasSuffix(plugin, "rp-pppoe.so") {
		line("%s", w.Interface) // pppd 2.4
		if w.Service != "" {
			line("rp_pppoe_service %s", pppQuote(w.Service))
		}
	} else {
		line("nic-%s", w.Interface) // pppd 2.5
		if w.Service != "" {
			line("pppoe-service %s", pppQuote(w.Service))
		}
	}
	line("user %s", pppQuote(w.Username))
	line("password %s", pppQuote(w.Password))
	line("ifname %s", pppDev)
	for _, o := range []string{
		"noauth", "defaultroute", "replacedefaultroute", "noipdefault", "hide-password",
		"persist", "maxfail 0", "holdoff 5", // переподключаться всегда
		"lcp-echo-interval 15", "lcp-echo-failure 4", // заметить обрыв за минуту
		"nodetach", "logfd 2",
	} {
		line("%s", o)
	}
	if len(w.DNS) == 0 {
		line("usepeerdns")
	}
	line("mtu %d", mtu)
	line("mru %d", mtu)
	return b.String()
}

// pppQuote — строка в кавычках для файла опций pppd.
func pppQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// pppAddrs — адрес и шлюз PPP: "inet 10.200.0.10 peer 10.200.0.1/32".
func pppAddrs() (ip, peer string) {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", pppDev).Output()
	if err != nil {
		return "", ""
	}
	f := strings.Fields(string(out))
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "inet":
			ip = f[i+1]
		case "peer":
			peer = strings.Split(f[i+1], "/")[0]
		}
	}
	return ip, peer
}

// ---------- udhcpc ----------

// udhcpcScript — обработчик событий busybox udhcpc: ставит адрес и маршрут,
// записывает DNS провайдера и состояние для интерфейса.
func udhcpcScript(state, resolv string, staticDNS bool) string {
	keepDNS := "0"
	if staticDNS {
		keepDNS = "1"
	}
	return fmt.Sprintf(`#!/bin/sh
# Создано NetPilot. Вызывается udhcpc при получении и потере адреса.
STATE=%q
RESOLV=%q
KEEP_DNS=%s
case "$1" in
  deconfig)
    ip -4 addr flush dev "$interface"
    ip link set dev "$interface" up
    rm -f "$STATE"
    ;;
  bound|renew)
    ip -4 addr flush dev "$interface"
    ip addr add "$ip/$mask" dev "$interface"
    gw="${router%%%% *}"
    [ -n "$gw" ] && ip route replace default via "$gw" dev "$interface"
    if [ "$KEEP_DNS" != "1" ] && [ -n "$dns" ]; then
      : > "$RESOLV.tmp"
      for d in $dns; do echo "nameserver $d" >> "$RESOLV.tmp"; done
      mv "$RESOLV.tmp" "$RESOLV"
    fi
    { echo "ip=$ip/$mask"; echo "router=$gw"; echo "dns=$dns"; echo "lease=$lease"; } > "$STATE"
    ;;
esac
exit 0
`, state, resolv, keepDNS)
}

// ---------- dnsmasq ----------

func dnsmasqArgs(l model.LAN, dir, resolv string) []string {
	self := strings.Split(l.Address, "/")[0]
	args := []string{
		"-k", "--conf-file=/dev/null", "--pid-file=", "--user=root",
		"--interface=" + l.Interface, "--bind-dynamic", "--except-interface=lo",
		"--resolv-file=" + resolv, "--no-hosts", "--cache-size=1000",
		"--domain-needed", "--bogus-priv",
		"--dhcp-leasefile=" + filepath.Join(dir, "dnsmasq.leases"),
		"--dhcp-hostsfile=" + filepath.Join(dir, "dhcp-hosts"),
	}
	if l.DHCP {
		hours := l.LeaseHours
		if hours <= 0 {
			hours = 12
		}
		mask := "255.255.255.0"
		if p, err := netip.ParsePrefix(l.Address); err == nil {
			mask = net.IP(net.CIDRMask(p.Bits(), 32)).String()
		}
		args = append(args,
			fmt.Sprintf("--dhcp-range=%s,%s,%s,%dh", l.RangeStart, l.RangeEnd, mask, hours),
			"--dhcp-option=option:router,"+self,
			"--dhcp-option=option:dns-server,"+self,
			"--dhcp-authoritative",
		)
	}
	return args
}

func writeHosts(path string, leases []Lease) error {
	var b strings.Builder
	for _, l := range leases {
		if l.MAC == "" || l.IP == "" {
			continue
		}
		name := hostName(l.Name)
		if name != "" {
			fmt.Fprintf(&b, "%s,%s,%s\n", l.MAC, l.IP, name)
		} else {
			fmt.Fprintf(&b, "%s,%s\n", l.MAC, l.IP)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func readHosts(path string) []Lease {
	var out []Lease
	for _, line := range strings.Split(tailFile(path, 1<<20), "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) >= 2 {
			l := Lease{MAC: f[0], IP: f[1]}
			if len(f) > 2 {
				l.Name = f[2]
			}
			out = append(out, l)
		}
	}
	return out
}

// hostName — имя для DNS: латиница, цифры, дефис.
func hostName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == ' ' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ---------- помощники ----------

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func nftApply(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nftables: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func readKV(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[k] = v
		}
	}
	return out
}

func resolvServers(path string) []string {
	var out []string
	for _, line := range strings.Split(tailFile(path, 4096), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}

func tailFile(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > n {
		_, _ = f.Seek(st.Size()-n, 0)
	}
	b := make([]byte, n)
	k, _ := f.Read(b)
	return string(b[:k])
}
