package app

import "testing"

func TestPingTimeParse(t *testing.T) {
	for out, want := range map[string]string{
		"64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.4 ms": "12.4",
		"Ответ от 77.88.8.8: число байт=32 время=41мс TTL=57":   "41",
		"Reply from 192.168.0.1: bytes=32 time<1ms TTL=64":      "1",
		"Antwort von 8.8.8.8: Bytes=32 Zeit=18ms TTL=117":       "18",
	} {
		m := pingTime.FindStringSubmatch(out)
		if m == nil || m[1] != want {
			t.Errorf("%q → %v, ждали %s", out, m, want)
		}
	}
	if pingTime.MatchString("Request timed out.") {
		t.Error("таймаут принят за ответ")
	}
}
