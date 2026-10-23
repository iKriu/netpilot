package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSListeners(t *testing.T) {
	udp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n" +
		" 1: 3500007F:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000   193        0 1 2 0 0\n" + // 127.0.0.53 — systemd-resolved
		" 2: 4900A8C0:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 2 2 0 0\n" + // 192.168.0.73:53 — dnsmasq
		" 3: 00000000:0044 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 3 2 0 0\n" // :68 — DHCP
	got := DNSListeners(udp, false)
	if len(got) != 1 || got[0].String() != "192.168.0.73" {
		t.Errorf("UDP: %v", got)
	}

	tcp := "  sl  local_address rem_address   st\n" +
		" 0: 00000000:0035 00000000:0000 0A 00000000:00000000\n" + // 0.0.0.0:53 LISTEN
		" 1: 4900A8C0:0035 0100A8C0:C350 01 00000000:00000000\n" // установленное соединение — не слушатель
	if got := DNSListeners(tcp, true); len(got) != 1 || got[0].String() != "0.0.0.0" {
		t.Errorf("TCP: %v", got)
	}

	udp6 := "  sl  local_address                         remote_address                        st\n" +
		" 0: 00000000000000000000000001000000:0035 00000000000000000000000000000000:0000 07\n" + // ::1 — loopback
		" 1: 00000000000000000000000000000000:0035 00000000000000000000000000000000:0000 07\n" // [::]:53 — принимает и IPv4
	if got := DNSListeners(udp6, false); len(got) != 1 || got[0].String() != "::" {
		t.Errorf("UDP6: %v", got)
	}
}

func TestLocalResolver(t *testing.T) {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		t.Skip("тест не для OpenWrt")
	}
	dir := t.TempDir()
	prev := procNetDir
	procNetDir = dir
	t.Cleanup(func() { procNetDir = prev })

	// Только заглушка systemd-resolved — запасной DNS можно.
	_ = os.WriteFile(filepath.Join(dir, "udp"), []byte("sl\n 1: 3500007F:0035 00000000:0000 07\n"), 0o644)
	if why := LocalResolver(); why != "" {
		t.Errorf("заглушка на loopback не мешает, а причина %q", why)
	}
	// Pi-hole на всех адресах — нельзя.
	_ = os.WriteFile(filepath.Join(dir, "tcp"), []byte("sl\n 0: 00000000:0035 00000000:0000 0A\n"), 0o644)
	if why := LocalResolver(); !strings.Contains(why, "DNS-сервер") {
		t.Errorf("причина: %q", why)
	}
}
