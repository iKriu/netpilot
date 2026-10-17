package sysproxy

import "testing"

func TestIsOurs(t *testing.T) {
	cases := []struct {
		server, listen string
		port           int
		want           bool
	}{
		{"127.0.0.1:7890", "127.0.0.1", 7890, true},
		{"http://127.0.0.1:7890", "127.0.0.1", 7890, true},
		{"localhost:7890", "0.0.0.0", 7890, true},
		{"[::1]:7890", "::", 7890, true},
		{"http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:7890", "127.0.0.1", 7890, true},
		{"192.168.1.5:7890", "192.168.1.5", 7890, true},
		{"127.0.0.2:7890", "127.0.0.1", 7890, true}, // весь 127/8 — loopback

		{"127.0.0.1:8080", "127.0.0.1", 7890, false},                 // другой порт
		{"proxy.corp:7890", "127.0.0.1", 7890, false},                // корпоративный прокси
		{"192.168.1.5:7890", "0.0.0.0", 7890, false},                 // чужой адрес в сети
		{"http=127.0.0.1:7890;https=10.0.0.1:3128", "", 7890, false}, // смешанные — не трогаем
		{"", "127.0.0.1", 7890, false},
		{"127.0.0.1", "127.0.0.1", 7890, false},
		{"127.0.0.1:7890", "127.0.0.1", 0, false},
	}
	for _, c := range cases {
		if got := IsOurs(c.server, c.listen, c.port); got != c.want {
			t.Errorf("IsOurs(%q, %q, %d) = %v, want %v", c.server, c.listen, c.port, got, c.want)
		}
	}
}
