// Package dhcp закрепляет IP за устройствами на DHCP-сервере. Это возможно
// только там, где адреса раздаёт сама система (OpenWrt). В остальных случаях
// закрепление хранится в NetPilot, и адрес нужно зарезервировать на роутере.
package dhcp

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"netpilot/internal/model"
)

type Backend interface {
	Name() string
	// Managed — может ли программа сама закреплять адреса.
	Managed() bool
	Sync(devices []model.Device) error
}

func Detect() Backend {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		if _, err := exec.LookPath("uci"); err == nil {
			return OpenWrt{}
		}
	}
	return None{}
}

type None struct{}

func (None) Name() string                      { return "внешний роутер" }
func (None) Managed() bool                     { return false }
func (None) Sync(devices []model.Device) error { return nil }

// OpenWrt пишет статические аренды в /etc/config/dhcp через uci.
// Свои записи помечаются префиксом секции np_, чужие не трогаются.
type OpenWrt struct{}

func (OpenWrt) Name() string  { return "OpenWrt (dnsmasq)" }
func (OpenWrt) Managed() bool { return true }

func (OpenWrt) Sync(devices []model.Device) error {
	out, err := exec.Command("uci", "-q", "show", "dhcp").Output()
	if err != nil && len(out) == 0 {
		return fmt.Errorf("uci show dhcp: %w", err)
	}
	script := uciScript(ownSections(string(out)), devices)
	cmd := exec.Command("uci", "batch")
	cmd.Stdin = strings.NewReader(script)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uci batch: %v: %s", err, msg)
	}
	if msg, err := exec.Command("/etc/init.d/dnsmasq", "reload").CombinedOutput(); err != nil {
		return fmt.Errorf("перезагрузка dnsmasq: %v: %s", err, msg)
	}
	return nil
}

var sectionLine = regexp.MustCompile(`(?m)^dhcp\.(np_[a-z0-9]+)=host$`)

func ownSections(uciShow string) []string {
	var out []string
	for _, m := range sectionLine.FindAllStringSubmatch(uciShow, -1) {
		out = append(out, m[1])
	}
	return out
}

// uciScript пересоздаёт свои записи: так удалённые и открепленные устройства
// исчезают из DHCP без отдельной логики.
func uciScript(existing []string, devices []model.Device) string {
	var b strings.Builder
	for _, s := range existing {
		fmt.Fprintf(&b, "delete dhcp.%s\n", s)
	}
	for _, d := range devices {
		if !d.Pinned || d.MAC == "" || d.IP == "" || !safeID.MatchString(d.ID) {
			continue
		}
		sec := "np_" + d.ID
		fmt.Fprintf(&b, "set dhcp.%s=host\n", sec)
		fmt.Fprintf(&b, "set dhcp.%s.name='%s'\n", sec, hostname(d))
		fmt.Fprintf(&b, "set dhcp.%s.mac='%s'\n", sec, quote(d.MAC))
		fmt.Fprintf(&b, "set dhcp.%s.ip='%s'\n", sec, quote(d.IP))
	}
	b.WriteString("commit dhcp\n")
	return b.String()
}

var (
	safeID      = regexp.MustCompile(`^[a-z0-9]+$`)
	notHostname = regexp.MustCompile(`[^A-Za-z0-9-]+`)
)

// hostname — имя для DNS: только латиница, цифры и дефис.
func hostname(d model.Device) string {
	src := d.Hostname
	if src == "" {
		src = d.Name
	}
	n := strings.Trim(notHostname.ReplaceAllString(src, "-"), "-")
	if len(n) > 63 {
		n = n[:63]
	}
	if n == "" {
		n = "device-" + d.ID
	}
	return n
}

func quote(s string) string { return strings.ReplaceAll(s, "'", "") }
