//go:build windows

package sysproxy

import (
	"errors"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const settingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const (
	internetOptionRefresh         = 37
	internetOptionSettingsChanged = 39
)

var procInternetSetOption = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")

// ClearStale выключает прокси пользователя, если он включён и указывает на
// NetPilot. Вызывать при запуске, до старта ядра. Возвращает true, если прокси был убран.
func ClearStale(listenAddr string, mixedPort int) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, settingsKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return false, nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || !IsOurs(server, listenAddr, mixedPort) {
		return false, nil
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return false, err
	}
	if err := k.DeleteValue("ProxyServer"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return true, err
	}
	// Без уведомления браузеры и WinINet продолжат пользоваться старыми настройками.
	if err := procInternetSetOption.Find(); err == nil {
		_, _, _ = procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
		_, _, _ = procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
	}
	return true, nil
}
