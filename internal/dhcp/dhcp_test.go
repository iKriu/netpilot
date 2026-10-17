package dhcp

import (
	"strings"
	"testing"

	"netpilot/internal/model"
)

func TestUCIScript(t *testing.T) {
	show := "dhcp.lan=dhcp\ndhcp.np_abc12=host\ndhcp.np_abc12.ip='192.168.1.9'\ndhcp.cfg01=host\n"
	got, skipped := uciScript(ownSections(show), []model.Device{
		{ID: "d1", Name: "Телевизор", Hostname: "LG-webOS TV", MAC: "aa:bb:cc:dd:ee:01", IP: "192.168.1.50", Pinned: true},
		{ID: "d2", Name: "Phone", MAC: "aa:bb:cc:dd:ee:02", IP: "192.168.1.51"},               // не закреплён
		{ID: "d3", Name: "Без MAC", IP: "192.168.1.52", Pinned: true},                         // нечего закреплять
		{ID: "d4", Name: "Кухня", MAC: "aa:bb:cc:dd:ee:04", IP: "192.168.1.53", Pinned: true}, // имя без латиницы
	})
	want := "delete dhcp.np_abc12\n" +
		"set dhcp.np_d1=host\nset dhcp.np_d1.name='LG-webOS-TV'\nset dhcp.np_d1.mac='aa:bb:cc:dd:ee:01'\nset dhcp.np_d1.ip='192.168.1.50'\n" +
		"set dhcp.np_d4=host\nset dhcp.np_d4.name='device-d4'\nset dhcp.np_d4.mac='aa:bb:cc:dd:ee:04'\nset dhcp.np_d4.ip='192.168.1.53'\n" +
		"commit dhcp\n"
	if got != want {
		t.Errorf("скрипт:\n%s\nожидалось:\n%s", got, want)
	}
	if len(skipped) != 0 {
		t.Errorf("лишние пропуски: %v", skipped)
	}
}

func TestUCIScriptRejectsInjection(t *testing.T) {
	got, skipped := uciScript(nil, []model.Device{
		{ID: "a", MAC: "aa:bb:cc:dd:ee:01\nset system.@system[0].hostname=x", IP: "192.168.1.50", Pinned: true},
		{ID: "b", MAC: "aa:bb:cc:dd:ee:02", IP: "192.168.1.51\ncommit", Pinned: true},
		{ID: "c", MAC: "aa:bb:cc:dd:ee:03", IP: "fe80::1", Pinned: true},                                                // не IPv4
		{ID: "d", MAC: "00:00:00:00:fe:80:00:00:00:00:00:00:00:00:00:01:00:00:00:01", IP: "192.168.1.53", Pinned: true}, // 20 байт
		{ID: "e", MAC: "AA-BB-CC-DD-EE-05", IP: "192.168.1.55", Pinned: true},                                           // приводится к виду uci
	})
	want := "set dhcp.np_e=host\nset dhcp.np_e.name='device-e'\nset dhcp.np_e.mac='aa:bb:cc:dd:ee:05'\nset dhcp.np_e.ip='192.168.1.55'\ncommit dhcp\n"
	if got != want {
		t.Errorf("скрипт:\n%s\nожидалось:\n%s", got, want)
	}
	if strings.Join(skipped, ",") != "a,b,c,d" {
		t.Errorf("пропущены %v", skipped)
	}
}
