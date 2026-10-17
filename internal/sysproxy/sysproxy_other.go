//go:build !windows

package sysproxy

// ClearStale — на других системах NetPilot системный прокси не оставляет.
func ClearStale(listenAddr string, mixedPort int) (bool, error) { return false, nil }
