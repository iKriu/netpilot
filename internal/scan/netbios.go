package scan

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"time"
)

// netbiosName запрашивает у устройства список его NetBIOS-имён (NBSTAT, UDP 137).
// Так отвечают компьютеры Windows и многие NAS.
func netbiosName(ctx context.Context, ip string) string {
	conn, err := net.Dial("udp4", net.JoinHostPort(ip, "137"))
	if err != nil {
		return ""
	}
	defer conn.Close()
	deadline := time.Now().Add(800 * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(nbstatQuery()); err != nil {
		return ""
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return ""
	}
	return parseNBSTAT(buf[:n])
}

func nbstatQuery() []byte {
	q := []byte{
		0x13, 0x37, // id
		0x00, 0x00, // флаги
		0x00, 0x01, // вопросов: 1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x20, // длина закодированного имени
	}
	// Имя "*" дополняется нулями до 16 байт и кодируется полубайтами ('A' + ниббл).
	name := make([]byte, 16)
	name[0] = '*'
	for _, b := range name {
		q = append(q, 'A'+b>>4, 'A'+b&0x0f)
	}
	return append(q, 0x00, 0x00, 0x21, 0x00, 0x01) // тип NBSTAT, класс IN
}

// parseNBSTAT возвращает первое уникальное имя рабочей станции (суффикс 0x00).
func parseNBSTAT(b []byte) string {
	// Заголовок 12 байт + имя 34 байта + тип, класс, TTL, длина = 10 байт.
	const off = 12 + 34 + 10
	if len(b) < off+1 {
		return ""
	}
	count := int(b[off])
	p := off + 1
	for i := 0; i < count && p+18 <= len(b); i++ {
		name := strings.TrimRight(string(b[p:p+15]), " \x00")
		suffix := b[p+15]
		flags := binary.BigEndian.Uint16(b[p+16 : p+18])
		p += 18
		if suffix == 0x00 && flags&0x8000 == 0 && name != "" {
			return name
		}
	}
	return ""
}
