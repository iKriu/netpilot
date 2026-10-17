package router

import (
	"strings"
	"testing"

	"netpilot/internal/model"
)

func good() model.Router {
	return model.Router{
		Enabled: true,
		WAN:     model.WAN{Interface: "eth1", Proto: "pppoe", Username: "user", Password: "p"},
		LAN:     model.LAN{Interface: "eth0", Address: "192.168.10.1/24", DHCP: true, RangeStart: "192.168.10.100", RangeEnd: "192.168.10.250", LeaseHours: 12},
	}
}

func TestValidate(t *testing.T) {
	ifaces := []Iface{{Name: "eth0"}, {Name: "eth1"}}
	if err := Validate(good(), ifaces); err != nil {
		t.Fatalf("корректная конфигурация: %v", err)
	}
	cases := map[string]func(r *model.Router){
		"нет логина":          func(r *model.Router) { r.WAN.Username = "" },
		"один порт на оба":    func(r *model.Router) { r.LAN.Interface = "eth1" },
		"несуществующий порт": func(r *model.Router) { r.WAN.Interface = "eth9" },
		"адрес без маски":     func(r *model.Router) { r.LAN.Address = "192.168.10.1" },
		"роутер внутри DHCP":  func(r *model.Router) { r.LAN.RangeStart = "192.168.10.1" },
		"диапазон вне сети":   func(r *model.Router) { r.LAN.RangeEnd = "192.168.11.5" },
		"диапазон наоборот":   func(r *model.Router) { r.LAN.RangeStart, r.LAN.RangeEnd = "192.168.10.250", "192.168.10.100" },
		"шлюз вне сети": func(r *model.Router) {
			r.WAN.Proto, r.WAN.Address, r.WAN.Gateway = "static", "203.0.113.10/24", "198.51.100.1"
		},
		"сети пересекаются": func(r *model.Router) {
			r.WAN.Proto, r.WAN.Address, r.WAN.Gateway = "static", "192.168.10.50/24", "192.168.10.254"
		},
		"DNS не IP":             func(r *model.Router) { r.WAN.DNS = []string{"dns.google"} },
		"MAC групповой":         func(r *model.Router) { r.WAN.MAC = "01:00:5e:00:00:01" },
		"MTU слишком маленький": func(r *model.Router) { r.WAN.MTU = 100 },
	}
	for name, mutate := range cases {
		r := good()
		mutate(&r)
		if err := Validate(r, ifaces); err == nil {
			t.Errorf("%s: ожидалась ошибка", name)
		}
	}
	off := good()
	off.Enabled, off.WAN.Username = false, ""
	if Validate(off, ifaces) != nil {
		t.Error("выключенный режим роутера не проверяется")
	}
}

func TestNATRuleset(t *testing.T) {
	rs := natRuleset([]string{"eth1", "ppp-wan"}, "eth0")
	for _, want := range []string{
		`oifname { "eth1", "ppp-wan" } masquerade`,
		`iifname { "eth1", "ppp-wan" } oifname "eth0" drop`,
		`tcp option maxseg size set rt mtu`,
		`iifname { "eth1", "ppp-wan" } udp dport 68 accept`,
		`iifname { "eth1", "ppp-wan" } drop`,
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("нет %q:\n%s", want, rs)
		}
	}
	// Защита входа: разрешения должны идти раньше запрета.
	if strings.Index(rs, "ct state established,related accept") > strings.LastIndex(rs, `} drop`) {
		t.Error("established должно разрешаться до запрета входа")
	}
}

func TestPPPOptions(t *testing.T) {
	w := model.WAN{Interface: "eth1", Username: `user"x`, Password: `p\a"ss`, Service: "inet"}
	o := pppOptionsFor("/usr/lib/pppd/2.5.1/pppoe.so", w)
	for _, want := range []string{
		"plugin /usr/lib/pppd/2.5.1/pppoe.so", "nic-eth1", `pppoe-service "inet"`,
		`user "user\"x"`, `password "p\\a\"ss"`, "ifname ppp-wan", "persist", "maxfail 0",
		"replacedefaultroute", "usepeerdns", "mtu 1492", "logfd 2",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("нет %q:\n%s", want, o)
		}
	}
	old := pppOptionsFor("/usr/lib/pppd/2.4.9/rp-pppoe.so", model.WAN{Interface: "eth1", DNS: []string{"1.1.1.1"}, MTU: 1480})
	if !strings.Contains(old, "\neth1\n") || strings.Contains(old, "usepeerdns") || !strings.Contains(old, "mtu 1480") {
		t.Errorf("pppd 2.4 / свои DNS / MTU:\n%s", old)
	}
}

func TestUCI(t *testing.T) {
	r := good()
	r.WAN.Password = "it's"
	r.WAN.DNS = []string{"1.1.1.1"}
	s := uciRouter(r)
	for _, want := range []string{
		"set network.wan.proto='pppoe'", "set network.wan.device='eth1'",
		`set network.wan.password='it'\''s'`, "add_list network.wan.dns='1.1.1.1'", "set network.wan.peerdns='0'",
		"set network.lan.ipaddr='192.168.10.1'", "set network.lan.netmask='255.255.255.0'",
		"set dhcp.lan.start='100'", "set dhcp.lan.limit='151'", "set dhcp.lan.leasetime='12h'",
		"commit network", "commit dhcp",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q:\n%s", want, s)
		}
	}
}

func TestParseIfstatus(t *testing.T) {
	st := parseIfstatus([]byte(`{"up":true,"uptime":120,"proto":"pppoe","l3_device":"pppoe-wan",
		"ipv4-address":[{"address":"100.64.1.2","mask":32}],
		"route":[{"target":"0.0.0.0","mask":0,"nexthop":"100.64.0.1"}],"dns-server":["8.8.8.8"]}`))
	if st.State != "up" || st.IP != "100.64.1.2/32" || st.Gateway != "100.64.0.1" || st.Device != "pppoe-wan" {
		t.Errorf("%+v", st)
	}
	st = parseIfstatus([]byte(`{"up":false,"pending":false,"errors":[{"code":"AUTH_FAILED"}]}`))
	if st.State != "error" || !strings.Contains(st.Message, "пароль") {
		t.Errorf("%+v", st)
	}
}

func TestExplainPPP(t *testing.T) {
	if !strings.Contains(explainPPP("PAP authentication failed"), "пароль") ||
		!strings.Contains(explainPPP("Timeout waiting for PADO packets"), "не отвечает") {
		t.Error("объяснения ошибок pppd")
	}
}
