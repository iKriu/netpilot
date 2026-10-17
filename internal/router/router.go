// Package router — режим роутера: подключение к провайдеру (PPPoE, DHCP,
// статический IP), NAT и раздача адресов в локальной сети.
//
// Бэкенды:
//   - openwrt — OpenWrt уже роутер, NetPilot меняет его настройки через uci;
//   - linux — обычный Linux (ZimaOS и т.п.): NetPilot сам запускает pppd/udhcpc,
//     dnsmasq и настраивает NAT в nftables.
package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"

	"netpilot/internal/model"
)

type Backend interface {
	Name() string
	// Check — чего не хватает на этой системе для режима роутера.
	Check() Support
	Interfaces() []Iface
	Apply(cfg model.Router, leases []Lease) error
	Disable() error
	Status(ctx context.Context) WANStatus
	Reconnect() error
	// UpdateLeases обновляет закреплённые адреса, не перезапуская подключение.
	UpdateLeases(leases []Lease) error
	// LeaseFile — где DHCP-сервер хранит выданные адреса ("" — не наш).
	LeaseFile() string
}

type Support struct {
	Available bool     `json:"available"`
	Managed   string   `json:"managed"` // "netpilot" — всё делает NetPilot; "openwrt" — настройки OpenWrt
	Problems  []string `json:"problems,omitempty"`
}

type Iface struct {
	Name    string   `json:"name"`
	MAC     string   `json:"mac"`
	Up      bool     `json:"up"`
	Carrier bool     `json:"carrier"` // кабель подключён
	Addrs   []string `json:"addrs"`
}

// Lease — закреплённый адрес (статическая аренда DHCP).
type Lease struct {
	MAC, IP, Name string
}

type WANStatus struct {
	State   string   `json:"state"` // disabled | connecting | up | down | error
	Proto   string   `json:"proto"`
	Device  string   `json:"device"` // ppp0, eth1, pppoe-wan
	IP      string   `json:"ip"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
	Since   int64    `json:"since,omitempty"` // когда подключилось (unix)
	RxBytes uint64   `json:"rxBytes"`
	TxBytes uint64   `json:"txBytes"`
	Message string   `json:"message,omitempty"` // понятное объяснение проблемы
}

// Detect выбирает бэкенд для этой системы.
func Detect(dataDir string) Backend {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return newOpenWrt()
	}
	return newLinux(dataDir)
}

// Validate проверяет настройки до применения — чтобы не ломать сеть
// заведомо неверной конфигурацией.
func Validate(r model.Router, ifaces []Iface) error {
	if !r.Enabled {
		return nil
	}
	names := map[string]bool{}
	for _, i := range ifaces {
		names[i.Name] = true
	}
	w, l := r.WAN, r.LAN
	if w.Interface == "" {
		return errors.New("выберите порт, в который включён кабель провайдера")
	}
	if len(ifaces) > 0 && !names[w.Interface] {
		return fmt.Errorf("порт %s не найден", w.Interface)
	}
	switch w.Proto {
	case "pppoe":
		if strings.TrimSpace(w.Username) == "" {
			return errors.New("укажите логин PPPoE из договора с провайдером")
		}
	case "dhcp":
	case "static":
		p, err := netip.ParsePrefix(w.Address)
		if err != nil || !p.Addr().Is4() {
			return errors.New("статический адрес укажите с маской: 203.0.113.10/24")
		}
		gw, err := netip.ParseAddr(w.Gateway)
		if err != nil || !p.Masked().Contains(gw) {
			return errors.New("шлюз провайдера должен быть в той же сети, что и адрес")
		}
	default:
		return fmt.Errorf("неизвестный тип подключения %q", w.Proto)
	}
	for _, d := range w.DNS {
		if net.ParseIP(strings.TrimSpace(d)) == nil {
			return fmt.Errorf("DNS-сервер %q — не IP-адрес", d)
		}
	}
	if w.MTU != 0 && (w.MTU < 576 || w.MTU > 9000) {
		return errors.New("MTU должен быть от 576 до 9000")
	}
	if w.MAC != "" {
		if hw, err := net.ParseMAC(w.MAC); err != nil || len(hw) != 6 || hw[0]&1 != 0 {
			return errors.New("неверный MAC-адрес для клонирования")
		}
	}

	if l.Interface == "" {
		return errors.New("выберите порт локальной сети")
	}
	if l.Interface == w.Interface {
		return errors.New("порт провайдера и порт локальной сети должны быть разными")
	}
	if len(ifaces) > 0 && !names[l.Interface] {
		return fmt.Errorf("порт %s не найден", l.Interface)
	}
	lan, err := netip.ParsePrefix(l.Address)
	if err != nil || !lan.Addr().Is4() || lan.Bits() < 8 || lan.Bits() > 30 {
		return errors.New("адрес роутера в локальной сети укажите с маской: 192.168.10.1/24")
	}
	if l.DHCP {
		start, e1 := netip.ParseAddr(l.RangeStart)
		end, e2 := netip.ParseAddr(l.RangeEnd)
		net := lan.Masked()
		if e1 != nil || e2 != nil || !net.Contains(start) || !net.Contains(end) || end.Less(start) {
			return errors.New("диапазон DHCP должен быть внутри локальной сети и идти по возрастанию")
		}
		if self := lan.Addr(); !self.Less(start) && !end.Less(self) { // start <= self <= end
			return errors.New("адрес роутера не должен попадать в диапазон DHCP")
		}
	}
	if w.Proto == "static" {
		if wp, _ := netip.ParsePrefix(w.Address); wp.Masked().Overlaps(lan.Masked()) {
			return errors.New("сети провайдера и локальная сеть пересекаются — выберите другой адрес локальной сети")
		}
	}
	return nil
}

// explainPPP переводит строки журнала pppd в понятное объяснение.
func explainPPP(log string) string {
	switch {
	case strings.Contains(log, "authentication failed"), strings.Contains(log, "Authentication failed"):
		return "Провайдер отклонил логин или пароль PPPoE"
	case strings.Contains(log, "Timeout waiting for PADO"), strings.Contains(log, "Unable to complete PPPoE Discovery"):
		return "Провайдер не отвечает по PPPoE: проверьте кабель и выбранный порт"
	case strings.Contains(log, "Couldn't open the /dev/ppp"), strings.Contains(log, "/dev/ppp"):
		return "Нет доступа к /dev/ppp: запустите контейнер с devices: /dev/ppp"
	case strings.Contains(log, "LCP: timeout"), strings.Contains(log, "No response to"):
		return "Связь с провайдером потеряна, переподключение…"
	case strings.Contains(log, "Plugin") && strings.Contains(log, "not found"):
		return "Не найден модуль PPPoE для pppd"
	}
	return ""
}

func listIfaces() []Iface {
	var out []Iface
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		n := strings.ToLower(ifc.Name)
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) != 6 {
			continue // ppp, туннели, loopback — не физические порты
		}
		skip := false
		for _, p := range []string{"docker", "veth", "br-", "virbr", "vmnet", "tailscale", "wg", "zt"} {
			skip = skip || strings.HasPrefix(n, p)
		}
		if skip {
			continue
		}
		i := Iface{Name: ifc.Name, MAC: ifc.HardwareAddr.String(), Up: ifc.Flags&net.FlagUp != 0}
		if b, err := os.ReadFile("/sys/class/net/" + ifc.Name + "/carrier"); err == nil {
			i.Carrier = strings.TrimSpace(string(b)) == "1"
		} else {
			i.Carrier = i.Up
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				i.Addrs = append(i.Addrs, ipn.String())
			}
		}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

func readCounter(dev, name string) uint64 {
	b, err := os.ReadFile("/sys/class/net/" + dev + "/statistics/" + name)
	if err != nil {
		return 0
	}
	var v uint64
	fmt.Sscan(strings.TrimSpace(string(b)), &v)
	return v
}
