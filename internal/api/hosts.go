package api

import (
	"net"
	"os"
	"strings"
)

// hostPolicy — какие имена в заголовке Host принимаем. Защита от DNS rebinding:
// чужой сайт может перенаправить своё имя на адрес NetPilot, и браузер
// посчитает запросы «своими» — но в Host останется имя чужого сайта.
type hostPolicy struct {
	any   bool // в NETPILOT_ALLOWED_HOSTS есть «*» — проверка выключена
	names map[string]bool
}

func newHostPolicy() hostPolicy {
	p := hostPolicy{names: map[string]bool{}}
	if h, err := os.Hostname(); err == nil && h != "" {
		h = normHost(h)
		short, _, _ := strings.Cut(h, ".")
		for _, n := range []string{h, short, short + ".local", short + ".lan"} {
			p.names[n] = true
		}
	}
	for _, n := range strings.Split(os.Getenv("NETPILOT_ALLOWED_HOSTS"), ",") {
		n = strings.TrimSpace(n)
		if n == "*" {
			p.any = true
		}
		if n = normHost(n); n != "" {
			p.names[n] = true
		}
	}
	return p
}

func (p hostPolicy) allowed(hostport string) bool {
	if p.any || hostport == "" { // без Host — только HTTP/1.0, не браузер
		return true
	}
	h := normHost(hostport)
	if h == "" {
		return false
	}
	// По IP-адресу DNS rebinding невозможен: имя сайта в Host не попадёт.
	if ip, _, _ := strings.Cut(h, "%"); net.ParseIP(ip) != nil {
		return true
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	return p.names[h]
}

// normHost: нижний регистр, без порта, скобок IPv6 и точки в конце.
func normHost(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	return strings.TrimSuffix(s, ".")
}
