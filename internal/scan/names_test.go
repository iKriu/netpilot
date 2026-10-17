package scan

import (
	"encoding/binary"
	"testing"
)

func TestPTRRoundTrip(t *testing.T) {
	q := ptrQuery(0x1234, "50.0.168.192.in-addr.arpa")
	// Ответ: вопрос из запроса + одна запись PTR со ссылкой на имя вопроса.
	resp := append([]byte{}, q...)
	resp[2] = 0x84                          // ответ, авторитетный
	binary.BigEndian.PutUint16(resp[6:], 1) // одна запись
	resp = append(resp, 0xc0, 12, 0, 12, 0, 1, 0, 0, 0, 120)
	rdata := []byte{12}
	rdata = append(rdata, "Ivans-iPhone"...)
	rdata = append(rdata, 5)
	rdata = append(rdata, "local"...)
	rdata = append(rdata, 0)
	resp = binary.BigEndian.AppendUint16(resp, uint16(len(rdata)))
	resp = append(resp, rdata...)

	if got := parsePTR(resp, 0x1234); got != "Ivans-iPhone.local" {
		t.Fatalf("parsePTR = %q", got)
	}
	if got := parsePTR(resp, 0x9999); got != "" {
		t.Fatalf("чужой id принят: %q", got)
	}
	if got := cleanName(parsePTR(resp, 0x1234)); got != "Ivans-iPhone" {
		t.Fatalf("cleanName = %q", got)
	}
}

func TestParseDHCPRequest(t *testing.T) {
	b := make([]byte, 240)
	b[0], b[1], b[2] = 1, 1, 6
	copy(b[28:], []byte{0x02, 0x11, 0x22, 0x04, 0x4e, 0x7b})
	binary.BigEndian.PutUint32(b[236:], 0x63825363)
	b = append(b, 53, 1, 3) // тип: DHCPREQUEST
	b = append(b, 12, 10)   // имя узла
	b = append(b, "Galaxy-S23"...)
	b = append(b, 255)
	mac, name := parseDHCPRequest(b)
	if mac != "02:11:22:04:4e:7b" || name != "Galaxy-S23" {
		t.Fatalf("got %q %q", mac, name)
	}
	if m, _ := parseDHCPRequest(b[:100]); m != "" {
		t.Fatal("короткий пакет принят")
	}
}
