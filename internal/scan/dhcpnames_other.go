//go:build !linux

package scan

import (
	"errors"
	"net"
)

// openDHCP: DHCP-запросы слушаются только на Linux.
func openDHCP() (net.PacketConn, error) {
	return nil, errors.New("прослушивание DHCP поддерживается только на Linux")
}
