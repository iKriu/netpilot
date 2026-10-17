package scan

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"
)

// mdnsName спрашивает имя у самого устройства через mDNS (Bonjour): обратный
// запрос PTR на его порт 5353. Запрос с обычного порта — «legacy unicast»
// (RFC 6762, 6.7), ответ приходит обратно нам. Отвечают iPhone, iPad, Mac,
// многие принтеры, телевизоры и Linux с Avahi: «Ivans-iPhone.local».
func mdnsName(ctx context.Context, ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return ""
	}
	b := addr.As4()
	qname := fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", b[3], b[2], b[1], b[0])

	conn, err := net.Dial("udp4", net.JoinHostPort(ip, "5353"))
	if err != nil {
		return ""
	}
	defer conn.Close()
	deadline := time.Now().Add(900 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	id := uint16(rand.UintN(0xffff) + 1)
	if _, err := conn.Write(ptrQuery(id, qname)); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return ""
		}
		if name := parsePTR(buf[:n], id); name != "" {
			return cleanName(name)
		}
	}
}

// ptrQuery — DNS-запрос PTR класса IN.
func ptrQuery(id uint16, name string) []byte {
	msg := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(msg[0:], id)
	binary.BigEndian.PutUint16(msg[4:], 1) // один вопрос
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0, 0, 12, 0, 1) // конец имени, тип PTR, класс IN
	return msg
}

// parsePTR достаёт имя из первой записи PTR в ответе с нужным id.
func parsePTR(msg []byte, id uint16) string {
	if len(msg) < 12 || binary.BigEndian.Uint16(msg) != id || msg[2]&0x80 == 0 {
		return ""
	}
	qd, an := int(binary.BigEndian.Uint16(msg[4:])), int(binary.BigEndian.Uint16(msg[6:]))
	off := 12
	for i := 0; i < qd; i++ {
		_, next, ok := readName(msg, off)
		if !ok || next+4 > len(msg) {
			return ""
		}
		off = next + 4
	}
	for i := 0; i < an; i++ {
		_, next, ok := readName(msg, off)
		if !ok || next+10 > len(msg) {
			return ""
		}
		typ := binary.BigEndian.Uint16(msg[next:])
		rdlen := int(binary.BigEndian.Uint16(msg[next+8:]))
		rdata := next + 10
		if rdata+rdlen > len(msg) {
			return ""
		}
		if typ == 12 {
			if name, _, ok := readName(msg, rdata); ok {
				return name
			}
		}
		off = rdata + rdlen
	}
	return ""
}

// readName разбирает доменное имя с учётом сжатия (ссылок на уже встреченные имена).
func readName(msg []byte, off int) (string, int, bool) {
	var labels []string
	next := -1
	for jumps := 0; jumps < 32; {
		if off >= len(msg) {
			return "", 0, false
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, "."), next, true
		case l&0xc0 == 0xc0:
			if off+1 >= len(msg) {
				return "", 0, false
			}
			if next < 0 {
				next = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3fff)
			jumps++
		default:
			if off+1+l > len(msg) {
				return "", 0, false
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
	return "", 0, false
}

// rdnsVia — обратный DNS через конкретный сервер. Роутер обычно знает имена
// устройств, которым сам выдал адреса по DHCP.
func rdnsVia(ctx context.Context, server, ip string) string {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
	}}
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	names, err := r.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return cleanName(names[0])
}
