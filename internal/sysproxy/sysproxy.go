// Package sysproxy убирает системный прокси Windows, оставшийся от прошлого
// запуска. Если NetPilot завершился аварийно (или Windows убила консоль раньше,
// чем ядро успело остановиться), прокси продолжает указывать на мёртвый порт —
// и у пользователя пропадает интернет во всех программах.
package sysproxy

import (
	"net"
	"strconv"
	"strings"
)

// IsOurs — указывает ли значение ProxyServer только на смешанный порт NetPilot
// на этом компьютере (loopback или адрес, на котором слушает ядро).
// Чужие настройки прокси не трогаем.
//
// Форматы: "127.0.0.1:7890", "http://127.0.0.1:7890",
// "http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:7890".
func IsOurs(proxyServer, listenAddr string, mixedPort int) bool {
	if mixedPort <= 0 {
		return false
	}
	listen := net.ParseIP(strings.Trim(listenAddr, "[]"))
	if listen != nil && listen.IsUnspecified() {
		listen = nil // 0.0.0.0 в прокси не пишут — проверяем только loopback
	}
	found := false
	for _, part := range strings.Split(proxyServer, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "="); i >= 0 {
			part = part[i+1:]
		}
		if i := strings.Index(part, "://"); i >= 0 {
			part = part[i+3:]
		}
		part = strings.TrimSuffix(part, "/")
		host, port, err := net.SplitHostPort(part)
		if err != nil || port != strconv.Itoa(mixedPort) {
			return false
		}
		host = strings.ToLower(host)
		ip := net.ParseIP(host)
		switch {
		case host == "localhost":
		case ip != nil && ip.IsLoopback():
		case ip != nil && listen != nil && ip.Equal(listen):
		default:
			return false
		}
		found = true
	}
	return found
}
