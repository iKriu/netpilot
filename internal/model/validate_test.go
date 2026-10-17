package model

import (
	"strings"
	"testing"
)

// check сверяет ошибку с ожиданием: want == "" — ошибки быть не должно,
// иначе она должна содержать want.
func check(t *testing.T, name string, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("%s: неожиданная ошибка %v", name, err)
	case want != "" && err == nil:
		t.Errorf("%s: ошибка не найдена (ожидалось «%s»)", name, want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Errorf("%s: ошибка %q не содержит «%s»", name, err, want)
	}
}

func TestValidateSettings(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *Settings)
		want string
	}{
		{"по умолчанию", func(*Settings) {}, ""},
		{"режим", func(s *Settings) { s.Mode = "tun" }, "режим работы"},
		{"охват", func(s *Settings) { s.TransparentScope = "some" }, "кого перехватывать"},
		{"ipv6 пусто", func(s *Settings) { s.IPv6 = "" }, ""},
		{"ipv6", func(s *Settings) { s.IPv6 = "on" }, "IPv6"},
		{"действие", func(s *Settings) { s.DefaultAction = "block" }, "действие по умолчанию"},
		{"порт 0", func(s *Settings) { s.MixedPort = 0 }, "порт прокси"},
		{"порт 65536", func(s *Settings) { s.MixedPort = 65536 }, "порт прокси"},
		{"tproxy 0 в режиме прокси", func(s *Settings) { s.TProxyPort = 0 }, ""},
		{"tproxy 0 в прозрачном", func(s *Settings) { s.Mode, s.TProxyPort = "transparent", 0 }, "TPROXY"},
		{"tproxy = mixed", func(s *Settings) { s.Mode, s.TProxyPort = "transparent", s.MixedPort }, "совпадает"},
		{"tproxy 70000", func(s *Settings) { s.TProxyPort = 70000 }, "TPROXY"},
		{"listen пусто", func(s *Settings) { s.ListenAddr = "" }, ""},
		{"listen ipv6", func(s *Settings) { s.ListenAddr = "::" }, ""},
		{"listen имя", func(s *Settings) { s.ListenAddr = "localhost" }, "не IP"},
		{"ping", func(s *Settings) { s.PingMethod = "udp" }, "проверки задержки"},
		{"ping пусто", func(s *Settings) { s.PingMethod = "" }, ""},
		{"журнал", func(s *Settings) { s.LogLevel = "trace" }, "уровень журнала"},
		{"dns", func(s *Settings) { s.DNSRemote = "  " }, "DNS для прокси"},
		{"dns direct", func(s *Settings) { s.DNSDirect = "" }, "прямого"},
		{"test url", func(s *Settings) { s.TestURL = "ftp://x" }, "http"},
		{"перевод строки", func(s *Settings) { s.DNSRemote = "1.1.1.1\nserver=evil" }, "dnsRemote"},
		{"NUL", func(s *Settings) { s.Core = "a\x00" }, "core"},
	}
	for _, c := range cases {
		s := Defaults().Settings
		c.edit(&s)
		check(t, c.name, ValidateSettings(&s), c.want)
	}
}

func TestValidateNode(t *testing.T) {
	ok := Node{ID: "n", Name: "DE 1", Protocol: "vless", Server: "de.example", Port: 443, ALPN: []string{"h2"}}
	cases := []struct {
		name string
		edit func(n *Node)
		want string
	}{
		{"правильный", func(*Node) {}, ""},
		{"порт 0", func(n *Node) { n.Port = 0 }, "сервер «DE 1»: неверный порт"},
		{"порт 65536", func(n *Node) { n.Port = 65536 }, "неверный порт"},
		{"без адреса", func(n *Node) { n.Server = " " }, "не указан адрес"},
		{"без протокола", func(n *Node) { n.Protocol = "" }, "протокол"},
		{"без имени — по адресу", func(n *Node) { n.Name, n.Port = "", 0 }, "сервер «de.example:0»"},
		{"перевод строки в alpn", func(n *Node) { n.ALPN = []string{"h2\r"} }, "alpn"},
		{"перевод строки в пути", func(n *Node) { n.Path = "/ws\n" }, "path"},
		{"отрицательная скорость", func(n *Node) { n.UpMbps = -1 }, "отрицательное"},
	}
	for _, c := range cases {
		n := ok
		n.ALPN = append([]string(nil), ok.ALPN...)
		c.edit(&n)
		check(t, c.name, ValidateNode(&n), c.want)
	}
}

func TestValidateGroup(t *testing.T) {
	cases := []struct {
		g    Group
		want string
	}{
		{Group{Name: "DE", Type: "auto", Filter: "DE|Germany|🇩🇪"}, ""},
		{Group{Name: "DE", Type: "select", Exclude: "LTE"}, ""},
		{Group{Name: "DE"}, ""},
		{Group{Name: " "}, "название"},
		{Group{Name: "DE", Type: "fallback"}, "тип"},
		{Group{Name: "DE", Filter: "(DE"}, "группа «DE»: ошибка в фильтре"},
		{Group{Name: "DE", Exclude: "[a"}, "исключении"},
		{Group{Name: "DE", NodeIDs: []string{"a\nb"}}, "nodeIds"},
	}
	for i, c := range cases {
		check(t, "группа "+string(rune('0'+i)), ValidateGroup(&c.g), c.want)
	}
}

func TestValidateRule(t *testing.T) {
	ok := func() Rule {
		return Rule{ID: "r", Name: "Игры", Enabled: true, Where: Where{Action: "proxy"}}
	}
	cases := []struct {
		name string
		edit func(r *Rule)
		want string
	}{
		{"правильное", func(*Rule) {}, ""},
		{"действие", func(r *Rule) { r.Where.Action = "reject" }, "правило «Игры»: неизвестное действие"},
		{"пустое действие", func(r *Rule) { r.Where.Action = "" }, "действие"},
		{"порты", func(r *Rule) { r.What.Ports = []string{"443", "27000-27100", " 80 "} }, ""},
		{"порт 0", func(r *Rule) { r.What.Ports = []string{"0"} }, "неверный порт «0»"},
		{"порт текст", func(r *Rule) { r.What.Ports = []string{"https"} }, "неверный порт"},
		{"диапазон наоборот", func(r *Rule) { r.What.Ports = []string{"200-100"} }, "диапазон"},
		{"диапазон за пределами", func(r *Rule) { r.What.Ports = []string{"1-70000"} }, "диапазон"},
		{"диапазон равный", func(r *Rule) { r.What.Ports = []string{"53-53"} }, ""},
		{"cidr", func(r *Rule) { r.Who.CIDRs = []string{"192.168.1.0/24", "10.0.0.5", "fd00::/8"} }, ""},
		{"плохой cidr", func(r *Rule) { r.Who.CIDRs = []string{"192.168.1.0/33"} }, "не адрес"},
		{"плохой ip", func(r *Rule) { r.What.IPs = []string{"1.2.3"} }, "не адрес"},
		{"сеть", func(r *Rule) { r.What.Network = "icmp" }, "тип соединений"},
		{"домен с переводом строки", func(r *Rule) { r.What.Domains = []string{"a.com\nb"} }, "what.domains"},
		{"без имени — по id", func(r *Rule) { r.Name, r.Where.Action = "", "x" }, "правило «r»"},
	}
	for _, c := range cases {
		r := ok()
		c.edit(&r)
		check(t, c.name, ValidateRule(&r), c.want)
	}
}

func TestValidateDevice(t *testing.T) {
	cases := []struct {
		name string
		d    Device
		want string
	}{
		{"пустое", Device{ID: "d"}, ""},
		{"правильное", Device{Name: "ТВ", IP: "192.168.1.10", MAC: "AA:BB:CC:DD:EE:FF", Route: "proxy"}, ""},
		{"mac через дефис", Device{MAC: "aa-bb-cc-dd-ee-ff"}, ""},
		{"ipv6", Device{Name: "ТВ", IP: "fd00::1"}, "устройство «ТВ»: «fd00::1» — не IPv4"},
		{"плохой ip", Device{IP: "300.1.1.1"}, "IPv4"},
		{"длинный mac", Device{MAC: "00:00:00:00:fe:80:00:00:00:00:00:00:00:00:00:01"}, "MAC"},
		{"плохой mac", Device{MAC: "zz"}, "MAC"},
		{"маршрут", Device{Route: "tor"}, "маршрут"},
		{"имя с переводом строки", Device{Name: "a\nb"}, "name"},
		{"тег с NUL", Device{Tags: []string{"x\x00"}}, "tags"},
	}
	for _, c := range cases {
		check(t, c.name, ValidateDevice(&c.d), c.want)
	}
}

func TestValidateSubscription(t *testing.T) {
	cases := []struct {
		s    Subscription
		want string
	}{
		{Subscription{Name: "P", URL: "https://example.com/sub?token=1"}, ""},
		{Subscription{Name: "P", URL: "http://10.0.0.1:8080/s"}, ""},
		{Subscription{Name: "P", URL: "ftp://example.com"}, "подписка «P»: адрес"},
		{Subscription{Name: "P", URL: "example.com"}, "http"},
		{Subscription{Name: "P", URL: "https://"}, "http"},
		{Subscription{Name: "P", URL: "https://e.com", Interval: -1}, "интервал"},
		// Текст ошибки от сервера в ядро не попадает — перевод строки там допустим.
		{Subscription{Name: "P", URL: "https://e.com", LastError: "a\nb"}, ""},
		{Subscription{Name: "P\n", URL: "https://e.com"}, "name"},
	}
	for i, c := range cases {
		check(t, "подписка "+string(rune('0'+i)), ValidateSubscription(&c.s), c.want)
	}
}

func TestValidateConfig(t *testing.T) {
	if errs := Validate(Defaults()); len(errs) != 0 {
		t.Fatalf("Defaults не проходит проверку: %v", errs)
	}
	cfg := Defaults()
	cfg.Nodes = []Node{
		{ID: "a", Name: "A", Protocol: "vless", Server: "a", Port: 1},
		{ID: "a", Name: "B", Protocol: "vless", Server: "b", Port: 0},
	}
	cfg.Groups = []Group{{ID: "a", Name: "G"}}
	cfg.Rules = []Rule{{ID: "r", Where: Where{Action: "proxy"}}, {ID: "r", Where: Where{Action: "direct"}}}
	cfg.Router.WAN.Password = "x\nplugin evil.so"
	cfg.Settings.Mode = "x"
	errs := Validate(cfg)
	var all []string
	for _, e := range errs {
		all = append(all, e.Error())
	}
	joined := strings.Join(all, " | ")
	for _, want := range []string{"режим работы", "сервер «B»: неверный порт", "id \"a\" уже занят", "повторяющийся id \"r\"", "режим роутера: поле wan.password"} {
		if !strings.Contains(joined, want) {
			t.Errorf("нет ошибки «%s» в: %s", want, joined)
		}
	}
	if Validate(nil) == nil {
		t.Error("nil принят")
	}
}
