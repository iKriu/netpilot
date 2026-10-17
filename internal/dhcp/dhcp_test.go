package dhcp

import (
	"testing"

	"netpilot/internal/model"
)

func TestUCIScript(t *testing.T) {
	show := "dhcp.lan=dhcp\ndhcp.np_abc12=host\ndhcp.np_abc12.ip='192.168.1.9'\ndhcp.cfg01=host\n"
	got := uciScript(ownSections(show), []model.Device{
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
}
