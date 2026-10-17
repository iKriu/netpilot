//go:build !linux

package firewall

import "errors"

var errUnsupported = errors.New("прозрачный режим работает только на Linux (OpenWrt, ZimaOS и другие)")

func Check() Status {
	return Status{Problems: []string{errUnsupported.Error()}}
}

func Apply(Spec) error { return errUnsupported }

func Remove() error { return nil }

func DNSFallback(string) error { return nil }

func SystemDNS() string { return "" }
