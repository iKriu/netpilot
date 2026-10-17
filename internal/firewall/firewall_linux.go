package firewall

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Check проверяет, что всё нужное для прозрачного режима есть.
func Check() Status {
	st := Status{Supported: true}
	for _, bin := range []string{"nft", "ip"} {
		if _, err := exec.LookPath(bin); err != nil {
			st.Supported = false
			st.Problems = append(st.Problems, fmt.Sprintf("не найдена программа %s (пакеты nftables, iproute2)", bin))
		}
	}
	if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward"); err == nil && strings.TrimSpace(string(b)) != "1" {
		st.Problems = append(st.Problems, "пересылка пакетов выключена (net.ipv4.ip_forward=0): устройства не смогут выходить в интернет через этот шлюз")
	}
	if st.Supported {
		st.Active = exec.Command("nft", "list", "table", "ip", Table).Run() == nil
	}
	return st
}

// Apply включает перехват. Повторный вызов обновляет правила (например,
// после изменения маршрутов устройств) без перезапуска ядра.
func Apply(s Spec) error {
	gatewaySysctls()
	if err := routing(true); err != nil {
		return err
	}
	if s.IPv6 == IPv6Intercept {
		if err := routing6(true); err != nil {
			_ = routing(false)
			return err
		}
	} else {
		_ = routing6(false)
	}
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(Ruleset(s))
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = routing(false)
		_ = routing6(false)
		return fmt.Errorf("nftables: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Remove выключает перехват. Вызывается и при старте программы: если прошлый
// запуск завершился аварийно, правила вели бы трафик в несуществующий сокет
// и сеть устройств перестала бы работать.
func Remove() error {
	var errs []error
	for _, t := range [][2]string{{"ip", Table}, {"ip6", Table6}} {
		if exec.Command("nft", "list", "table", t[0], t[1]).Run() == nil {
			if out, err := exec.Command("nft", "delete", "table", t[0], t[1]).CombinedOutput(); err != nil {
				errs = append(errs, fmt.Errorf("nftables: %v: %s", err, out))
			}
		}
	}
	if err := routing(false); err != nil {
		errs = append(errs, err)
	}
	_ = routing6(false)
	return errors.Join(errs...)
}

// DNSFallback включает (upstream != "") или выключает (upstream == "")
// пересылку DNS устройств на upstream, пока ядро прокси не работает.
func DNSFallback(upstream string) error {
	if upstream == "" {
		if exec.Command("nft", "list", "table", "ip", DNSFallbackTable).Run() != nil {
			return nil
		}
		if out, err := exec.Command("nft", "delete", "table", "ip", DNSFallbackTable).CombinedOutput(); err != nil {
			return fmt.Errorf("nftables: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	script, err := DNSFallbackRuleset(upstream)
	if err != nil {
		return err
	}
	gatewaySysctls()
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nftables: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SystemDNS — DNS-сервер, которым пользуется сама система (роутер или
// провайдер): у systemd-resolved настоящие серверы — в /run/systemd/resolve,
// в /etc/resolv.conf там заглушка 127.0.0.53. "" — не найден.
func SystemDNS() string {
	for _, f := range []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if ip := firstNameserver(string(b)); ip != "" {
			return ip
		}
	}
	return ""
}

// gatewaySysctls — настройки ядра для шлюза. Ошибки не критичны (в контейнере
// /proc/sys может быть только для чтения — там их задаёт docker через sysctls).
//   - пересылка пакетов: без неё устройства не выйдут в интернет;
//   - без ICMP-редиректов: трафик приходит и уходит через один порт, и Linux
//     подсказывал бы устройствам «идите напрямую через роутер» — они начали бы
//     обходить шлюз.
func gatewaySysctls() {
	write := func(path, v string) { _ = os.WriteFile(path, []byte(v), 0o644) }
	write("/proc/sys/net/ipv4/ip_forward", "1")
	confs, _ := filepath.Glob("/proc/sys/net/ipv4/conf/*/send_redirects")
	for _, p := range confs {
		write(p, "0")
	}
}

// routing добавляет или убирает правило «помеченные пакеты — доставить локально».
func routing(on bool) error { return policyRoute("-4", "0.0.0.0/0", on) }

func routing6(on bool) error { return policyRoute("-6", "::/0", on) }

func policyRoute(family, all string, on bool) error {
	mark := "0x" + strconv.FormatInt(Mark, 16)
	table := strconv.Itoa(RouteTable)
	// Удаляем все свои правила (их могло остаться несколько после сбоев).
	for i := 0; i < 10; i++ {
		if exec.Command("ip", family, "rule", "del", "fwmark", mark, "table", table).Run() != nil {
			break
		}
	}
	_ = exec.Command("ip", family, "route", "flush", "table", table).Run()
	if !on {
		return nil
	}
	if out, err := exec.Command("ip", family, "rule", "add", "fwmark", mark, "table", table).CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s rule: %v: %s", family, err, out)
	}
	if out, err := exec.Command("ip", family, "route", "add", "local", all, "dev", "lo", "table", table).CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s route: %v: %s", family, err, out)
	}
	return nil
}
