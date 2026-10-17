package scan

import (
	"context"
	"net"
	"syscall"
)

// openDHCP открывает порт 67 с SO_REUSEADDR: Go не ставит его для UDP, и без
// него занятый нами порт не дал бы запуститься dnsmasq (тот ставит SO_REUSEADDR
// у себя). SO_REUSEPORT не ставим: тогда ядро делило бы между сокетами и
// адресные запросы (продление аренды), и часть из них не дошла бы до сервера.
func openDHCP() (net.PacketConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		}); err != nil {
			return err
		}
		return serr
	}}
	return lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:67")
}
