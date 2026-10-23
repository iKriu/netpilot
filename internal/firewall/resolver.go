package firewall

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// Запасной DNS перенаправляет (DNAT) все запросы устройств к порту 53 этого
// сервера. Если здесь уже работает свой DNS-сервер — dnsmasq, Pi-hole,
// AdGuard Home, или это OpenWrt со своим dnsmasq, — такое правило перехватило
// бы его запросы и молча увело бы сеть мимо него (и мимо его блокировок).
// Поэтому при чужом DNS-сервере на этом компьютере запасной DNS не включаем:
// устройствам и так есть кому отвечать.

// Состояния сокетов в /proc/net/{tcp,udp}: TCP_LISTEN и «UDP без соединения».
const (
	tcpListen = "0A"
	udpOpen   = "07"
)

// DNSListeners разбирает /proc/net/{tcp,udp,tcp6,udp6} и возвращает адреса,
// на которых кто-то слушает порт 53, кроме loopback (127.0.0.53 у
// systemd-resolved и ::1 обслуживают только сам сервер, устройства до них не
// достают). Адрес записан в hex словами по 4 байта в порядке байт процессора,
// порт — обычным hex: 00000000:0035 = 0.0.0.0:53.
func DNSListeners(content string, tcp bool) []netip.Addr {
	state := udpOpen
	if tcp {
		state = tcpListen
	}
	var out []netip.Addr
	for _, line := range strings.Split(content, "\n") {
		f := strings.Fields(line)
		// sl local_address rem_address st ...
		if len(f) < 4 || f[3] != state {
			continue
		}
		host, port, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		if p, err := strconv.ParseUint(port, 16, 16); err != nil || p != 53 {
			continue
		}
		ip, ok := procAddr(host)
		if !ok || ip.IsLoopback() {
			continue
		}
		out = append(out, ip)
	}
	return out
}

func procAddr(s string) (netip.Addr, bool) {
	b, err := hex.DecodeString(s)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.Addr{}, false
	}
	// Каждое 32-битное слово — в порядке байт процессора (little-endian).
	for i := 0; i+4 <= len(b); i += 4 {
		binary.BigEndian.PutUint32(b[i:], binary.LittleEndian.Uint32(b[i:]))
	}
	ip, _ := netip.AddrFromSlice(b)
	return ip.Unmap(), true
}

// procNetDir — каталог таблиц сокетов (подменяется в тестах).
var procNetDir = "/proc/net"

// LocalResolver — почему запасной DNS включать нельзя: на этом компьютере
// уже работает DNS-сервер для сети. "" — можно. Вызывается, когда ядро
// NetPilot остановлено, так что слушатель порта 53 — заведомо чужой.
func LocalResolver() string {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return "это OpenWrt — DNS сети обслуживает его dnsmasq"
	}
	for _, f := range []struct {
		name string
		tcp  bool
	}{{"udp", false}, {"udp6", false}, {"tcp", true}, {"tcp6", true}} {
		b, err := os.ReadFile(procNetDir + "/" + f.name)
		if err != nil {
			continue
		}
		if ips := DNSListeners(string(b), f.tcp); len(ips) > 0 {
			return fmt.Sprintf("на этом сервере уже работает DNS-сервер (порт 53 на %s)", ips[0])
		}
	}
	return ""
}
