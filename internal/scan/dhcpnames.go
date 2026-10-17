package scan

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"strings"
	"sync"
)

// Имена из DHCP-запросов. Подключаясь к сети, устройство рассылает всем
// DHCP-запрос и в нём называет себя (опция 12: «Galaxy-S23», «iPhone»).
// Адреса раздаёт роутер, но запрос широковещательный — его видит и NetPilot.
var dhcpSeen sync.Map // MAC → имя

// ListenDHCP слушает DHCP-запросы в сети, ничего не отвечая. Если порт 67
// занят (NetPilot сам раздаёт адреса в режиме роутера), имена берутся из аренд.
func ListenDHCP(ctx context.Context) {
	if runtime.GOOS != "linux" {
		return
	}
	pc, err := net.ListenPacket("udp4", "0.0.0.0:67")
	if err != nil {
		return
	}
	go func() { <-ctx.Done(); pc.Close() }()
	buf := make([]byte, 1500)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		if mac, name := parseDHCPRequest(buf[:n]); mac != "" && name != "" {
			dhcpSeen.Store(mac, name)
		}
	}
}

// dhcpNames — имена, услышанные в DHCP-запросах: MAC → имя.
func dhcpNames() map[string]string {
	out := map[string]string{}
	dhcpSeen.Range(func(k, v any) bool { out[k.(string)] = v.(string); return true })
	return out
}

func parseDHCPRequest(b []byte) (mac, name string) {
	// BOOTREQUEST, Ethernet, MAC длиной 6, «волшебное число» DHCP.
	if len(b) < 240 || b[0] != 1 || b[1] != 1 || b[2] != 6 || binary.BigEndian.Uint32(b[236:]) != 0x63825363 {
		return "", ""
	}
	mac = net.HardwareAddr(b[28:34]).String()
	var host, fqdn string
	for i := 240; i < len(b); {
		code := b[i]
		if code == 0 {
			i++
			continue
		}
		if code == 255 || i+1 >= len(b) {
			break
		}
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		v := b[i+2 : i+2+l]
		switch code {
		case 12: // имя узла
			host = string(v)
		case 81: // полное имя: флаги, 2 байта, затем имя (текстом или в формате DNS)
			if len(v) > 3 {
				if v[0]&0x04 != 0 {
					fqdn, _, _ = readName(append(append([]byte{}, v[3:]...), 0), 0)
				} else {
					fqdn = string(v[3:])
				}
			}
		}
		i += 2 + l
	}
	if host == "" {
		host, _, _ = strings.Cut(fqdn, ".")
	}
	return mac, cleanName(strings.TrimSpace(strings.Trim(host, "\x00")))
}
