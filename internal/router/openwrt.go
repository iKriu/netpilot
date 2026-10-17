package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"time"

	"netpilot/internal/model"
)

// openwrtBackend — OpenWrt уже роутер: подключение, NAT и DHCP делает он сам,
// а NetPilot только меняет его настройки (uci) и показывает состояние.
type openwrtBackend struct{}

func newOpenWrt() *openwrtBackend { return &openwrtBackend{} }

func (openwrtBackend) Name() string               { return "OpenWrt" }
func (openwrtBackend) Interfaces() []Iface        { return listIfaces() }
func (openwrtBackend) LeaseFile() string          { return "/tmp/dhcp.leases" }
func (openwrtBackend) UpdateLeases([]Lease) error { return nil } // закрепления пишет пакет dhcp

func (openwrtBackend) Check() Support {
	s := Support{Available: true, Managed: "openwrt"}
	if _, err := exec.LookPath("uci"); err != nil {
		s.Available = false
		s.Problems = append(s.Problems, "не найдена программа uci")
	}
	return s
}

func (openwrtBackend) Apply(cfg model.Router, _ []Lease) error {
	if !cfg.Enabled {
		return nil // OpenWrt остаётся роутером со своими настройками
	}
	cmd := exec.Command("uci", "batch")
	cmd.Stdin = strings.NewReader(uciRouter(cfg))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uci: %v: %s", err, out)
	}
	for _, svc := range []string{"network", "dnsmasq"} {
		if out, err := exec.Command("/etc/init.d/"+svc, "reload").CombinedOutput(); err != nil {
			return fmt.Errorf("%s reload: %v: %s", svc, err, out)
		}
	}
	return nil
}

func (openwrtBackend) Disable() error {
	return errors.New("на OpenWrt подключение к интернету настраивается, но не отключается из NetPilot")
}

func (openwrtBackend) Reconnect() error {
	if out, err := exec.Command("ifup", "wan").CombinedOutput(); err != nil {
		return fmt.Errorf("ifup wan: %v: %s", err, out)
	}
	return nil
}

func (openwrtBackend) Status(ctx context.Context) WANStatus {
	out, err := exec.CommandContext(ctx, "ifstatus", "wan").Output()
	if err != nil {
		return WANStatus{State: "error", Message: "интерфейс wan не найден в настройках OpenWrt"}
	}
	st := parseIfstatus(out)
	st.RxBytes, st.TxBytes = readCounter(st.Device, "rx_bytes"), readCounter(st.Device, "tx_bytes")
	return st
}

// parseIfstatus разбирает вывод «ifstatus wan» (ubus).
func parseIfstatus(data []byte) WANStatus {
	var s struct {
		Up      bool   `json:"up"`
		Pending bool   `json:"pending"`
		Uptime  int64  `json:"uptime"`
		Proto   string `json:"proto"`
		Device  string `json:"l3_device"`
		Addrs   []struct {
			Address string `json:"address"`
			Mask    int    `json:"mask"`
		} `json:"ipv4-address"`
		Routes []struct {
			Target  string `json:"target"`
			Mask    int    `json:"mask"`
			Nexthop string `json:"nexthop"`
		} `json:"route"`
		DNS    []string `json:"dns-server"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return WANStatus{State: "error", Message: "не удалось разобрать состояние wan"}
	}
	st := WANStatus{Proto: s.Proto, Device: s.Device, DNS: s.DNS}
	if len(s.Addrs) > 0 {
		st.IP = fmt.Sprintf("%s/%d", s.Addrs[0].Address, s.Addrs[0].Mask)
	}
	for _, r := range s.Routes {
		if r.Target == "0.0.0.0" && r.Mask == 0 {
			st.Gateway = r.Nexthop
		}
	}
	switch {
	case s.Up:
		st.State = "up"
		st.Since = time.Now().Add(-time.Duration(s.Uptime) * time.Second).Unix()
	case s.Pending:
		st.State = "connecting"
	default:
		st.State = "down"
	}
	for _, e := range s.Errors {
		switch e.Code {
		case "AUTH_FAILED":
			st.State, st.Message = "error", "Провайдер отклонил логин или пароль PPPoE"
		case "NO_DEVICE":
			st.State, st.Message = "error", "Порт провайдера не найден"
		}
	}
	return st
}

// uciRouter — команды «uci batch» для wan/lan и DHCP.
func uciRouter(r model.Router) string {
	var b strings.Builder
	set := func(k, v string) { fmt.Fprintf(&b, "set %s=%s\n", k, uciQuote(v)) }
	// delete несуществующего параметра — не ошибка для uci batch, но пишет
	// предупреждение; поэтому сначала задаём пустое значение, затем удаляем.
	del := func(k string) { fmt.Fprintf(&b, "set %s=''\ndelete %s\n", k, k) }
	w, l := r.WAN, r.LAN

	b.WriteString("set network.wan=interface\n")
	set("network.wan.proto", w.Proto)
	if w.Interface != "" {
		set("network.wan.device", w.Interface)
	}
	for _, k := range []string{"username", "password", "service", "ipaddr", "netmask", "gateway"} {
		del("network.wan." + k)
	}
	switch w.Proto {
	case "pppoe":
		set("network.wan.username", w.Username)
		set("network.wan.password", w.Password)
		if w.Service != "" {
			set("network.wan.service", w.Service)
		}
	case "static":
		if p, err := netip.ParsePrefix(w.Address); err == nil {
			set("network.wan.ipaddr", p.Addr().String())
			set("network.wan.netmask", net.IP(net.CIDRMask(p.Bits(), 32)).String())
		}
		set("network.wan.gateway", w.Gateway)
	}
	del("network.wan.dns")
	if len(w.DNS) > 0 {
		for _, d := range w.DNS {
			fmt.Fprintf(&b, "add_list network.wan.dns=%s\n", uciQuote(strings.TrimSpace(d)))
		}
		set("network.wan.peerdns", "0")
	} else {
		set("network.wan.peerdns", "1")
	}
	if w.MTU > 0 {
		set("network.wan.mtu", fmt.Sprint(w.MTU))
	} else {
		del("network.wan.mtu")
	}
	if w.MAC != "" {
		set("network.wan.macaddr", w.MAC)
	} else {
		del("network.wan.macaddr")
	}

	if p, err := netip.ParsePrefix(l.Address); err == nil {
		set("network.lan.ipaddr", p.Addr().String())
		set("network.lan.netmask", net.IP(net.CIDRMask(p.Bits(), 32)).String())
		if l.DHCP {
			start, e1 := netip.ParseAddr(l.RangeStart)
			end, e2 := netip.ParseAddr(l.RangeEnd)
			if e1 == nil && e2 == nil {
				set("dhcp.lan.ignore", "0")
				set("dhcp.lan.start", fmt.Sprint(offset(p.Masked().Addr(), start)))
				set("dhcp.lan.limit", fmt.Sprint(offset(start, end)+1))
			}
			hours := l.LeaseHours
			if hours <= 0 {
				hours = 12
			}
			set("dhcp.lan.leasetime", fmt.Sprintf("%dh", hours))
		} else {
			set("dhcp.lan.ignore", "1")
		}
	}
	b.WriteString("commit network\ncommit dhcp\n")
	return b.String()
}

func offset(from, to netip.Addr) int {
	a, b := from.As4(), to.As4()
	x := int(a[0])<<24 | int(a[1])<<16 | int(a[2])<<8 | int(a[3])
	y := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	return y - x
}

// uciQuote — значение в одинарных кавычках для uci batch.
func uciQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
