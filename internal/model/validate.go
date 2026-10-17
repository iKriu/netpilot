package model

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Проверки конфигурации собраны здесь, в одном месте: их используют и API
// (перед сохранением объекта), и восстановление из резервной копии, и загрузка.
// Модель не зависит от ядра, поэтому регулярные выражения групп проверяются
// так же, как их потом соберёт ядро ((?i) + выражение), но без импорта core.

// Допустимые значения настроек. "" там, где пустое значение означает «по умолчанию».
var (
	settingsModes   = []string{"proxy", "transparent"}
	settingsScopes  = []string{"all", "selected"}
	settingsIPv6    = []string{"block", "intercept", "off", ""}
	settingsActions = []string{"proxy", "direct"}
	pingMethods     = []string{"auto", "http", "tcp", "icmp", ""}
	logLevels       = []string{"error", "warn", "info", "debug", ""}
	groupTypes      = []string{"auto", "select", ""}
	ruleActions     = []string{"proxy", "direct", "block"}
	deviceRoutes    = []string{"", "proxy", "direct", "block"}
	ruleNetworks    = []string{"", "tcp", "udp"}
)

// Validate проверяет конфигурацию целиком и возвращает все найденные ошибки.
func Validate(cfg *Config) []error {
	if cfg == nil {
		return []error{errors.New("конфигурация пуста")}
	}
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	add(ValidateSettings(&cfg.Settings))
	for i := range cfg.Nodes {
		add(ValidateNode(&cfg.Nodes[i]))
	}
	for i := range cfg.Groups {
		add(ValidateGroup(&cfg.Groups[i]))
	}
	for i := range cfg.Rules {
		add(ValidateRule(&cfg.Rules[i]))
	}
	for i := range cfg.Devices {
		add(ValidateDevice(&cfg.Devices[i]))
	}
	for i := range cfg.Subscriptions {
		add(ValidateSubscription(&cfg.Subscriptions[i]))
	}
	// Параметры WAN попадают в файлы pppd/udhcpc: перевод строки там — новая опция.
	add(noControl("режим роутера", cfg.Router))

	// Узлы и группы — одно пространство id: правило ссылается на «узел или группу».
	seen := map[string]string{}
	checkID := func(kind, id string) {
		if id == "" {
			errs = append(errs, fmt.Errorf("%s без id", kind))
			return
		}
		if prev, ok := seen[id]; ok {
			errs = append(errs, fmt.Errorf("%s: id %q уже занят (%s)", kind, id, prev))
			return
		}
		seen[id] = kind
	}
	for _, n := range cfg.Nodes {
		checkID(nodeLabel(&n), n.ID)
	}
	for _, g := range cfg.Groups {
		checkID(groupLabel(&g), g.ID)
	}
	for _, kinds := range [][]string{ruleIDs(cfg), deviceIDs(cfg), subIDs(cfg)} {
		ids := map[string]bool{}
		for _, id := range kinds {
			if id != "" && ids[id] {
				errs = append(errs, fmt.Errorf("повторяющийся id %q", id))
			}
			ids[id] = true
		}
	}
	return errs
}

func ValidateSettings(s *Settings) error {
	const what = "настройки"
	if err := noControl(what, *s); err != nil {
		return err
	}
	for _, c := range []struct {
		name, v string
		allowed []string
	}{
		{"режим работы", s.Mode, settingsModes},
		{"кого перехватывать", s.TransparentScope, settingsScopes},
		{"IPv6", s.IPv6, settingsIPv6},
		{"действие по умолчанию", s.DefaultAction, settingsActions},
		{"способ проверки задержки", s.PingMethod, pingMethods},
		{"уровень журнала", s.LogLevel, logLevels},
	} {
		if !slices.Contains(c.allowed, c.v) {
			return fmt.Errorf("%s: неизвестное значение «%s» (%s)", what, c.v, c.name)
		}
	}
	if !validPort(s.MixedPort) {
		return fmt.Errorf("%s: неверный порт прокси %d", what, s.MixedPort)
	}
	// Порт TPROXY нужен только прозрачному режиму; в режиме прокси 0 допустим.
	if s.TProxyPort != 0 || s.Mode == "transparent" {
		if !validPort(s.TProxyPort) {
			return fmt.Errorf("%s: неверный порт TPROXY %d", what, s.TProxyPort)
		}
		if s.Mode == "transparent" && s.TProxyPort == s.MixedPort {
			return fmt.Errorf("%s: порт TPROXY совпадает с портом прокси", what)
		}
	}
	if s.ListenAddr != "" {
		if _, err := netip.ParseAddr(s.ListenAddr); err != nil {
			return fmt.Errorf("%s: адрес прокси «%s» — не IP-адрес", what, s.ListenAddr)
		}
	}
	if strings.TrimSpace(s.DNSRemote) == "" {
		return fmt.Errorf("%s: не указан DNS для прокси", what)
	}
	if strings.TrimSpace(s.DNSDirect) == "" {
		return fmt.Errorf("%s: не указан DNS для прямого трафика", what)
	}
	if s.TestURL != "" && !httpURL(s.TestURL) {
		return fmt.Errorf("%s: адрес проверки задержки должен начинаться с http:// или https://", what)
	}
	return nil
}

func ValidateNode(n *Node) error {
	what := nodeLabel(n)
	if err := noControl(what, *n); err != nil {
		return err
	}
	if strings.TrimSpace(n.Protocol) == "" {
		return fmt.Errorf("%s: не указан протокол", what)
	}
	if strings.TrimSpace(n.Server) == "" {
		return fmt.Errorf("%s: не указан адрес", what)
	}
	if !validPort(n.Port) {
		return fmt.Errorf("%s: неверный порт %d", what, n.Port)
	}
	if n.AlterID < 0 || n.UpMbps < 0 || n.DownMbps < 0 {
		return fmt.Errorf("%s: отрицательное число в параметрах", what)
	}
	return nil
}

func ValidateGroup(g *Group) error {
	what := groupLabel(g)
	if err := noControl(what, *g); err != nil {
		return err
	}
	if strings.TrimSpace(g.Name) == "" {
		return errors.New("укажите название группы")
	}
	if !slices.Contains(groupTypes, g.Type) {
		return fmt.Errorf("%s: неизвестный тип «%s»", what, g.Type)
	}
	for _, f := range []struct{ name, src string }{{"фильтре", g.Filter}, {"исключении", g.Exclude}} {
		if f.src == "" {
			continue
		}
		if _, err := regexp.Compile("(?i)" + f.src); err != nil {
			return fmt.Errorf("%s: ошибка в %s «%s»: %v", what, f.name, f.src, err)
		}
	}
	return nil
}

func ValidateRule(r *Rule) error {
	what := ruleLabel(r)
	if err := noControl(what, *r); err != nil {
		return err
	}
	if !slices.Contains(ruleActions, r.Where.Action) {
		return fmt.Errorf("%s: неизвестное действие «%s»", what, r.Where.Action)
	}
	for _, c := range r.Who.CIDRs {
		if !validAddrOrPrefix(c) {
			return fmt.Errorf("%s: «%s» — не адрес и не подсеть", what, c)
		}
	}
	for _, c := range r.What.IPs {
		if !validAddrOrPrefix(c) {
			return fmt.Errorf("%s: «%s» — не адрес и не подсеть", what, c)
		}
	}
	for _, p := range r.What.Ports {
		if err := validPortSpec(p); err != nil {
			return fmt.Errorf("%s: %v", what, err)
		}
	}
	if !slices.Contains(ruleNetworks, r.What.Network) {
		return fmt.Errorf("%s: неизвестный тип соединений «%s»", what, r.What.Network)
	}
	return nil
}

func ValidateDevice(d *Device) error {
	what := deviceLabel(d)
	if err := noControl(what, *d); err != nil {
		return err
	}
	if d.IP != "" {
		a, err := netip.ParseAddr(d.IP)
		if err != nil || !a.Is4() {
			return fmt.Errorf("%s: «%s» — не IPv4-адрес", what, d.IP)
		}
	}
	if d.MAC != "" {
		hw, err := net.ParseMAC(d.MAC)
		if err != nil || len(hw) != 6 {
			return fmt.Errorf("%s: «%s» — не MAC-адрес", what, d.MAC)
		}
	}
	if !slices.Contains(deviceRoutes, d.Route) {
		return fmt.Errorf("%s: неизвестный маршрут «%s»", what, d.Route)
	}
	return nil
}

func ValidateSubscription(s *Subscription) error {
	what := subLabel(s)
	if err := noControl(what, *s); err != nil {
		return err
	}
	if !httpURL(s.URL) {
		return fmt.Errorf("%s: адрес должен начинаться с http:// или https://", what)
	}
	if s.Interval < 0 {
		return fmt.Errorf("%s: отрицательный интервал обновления", what)
	}
	return nil
}

// Подписи объектов для сообщений: по имени, а если его нет — по тому, что есть.

func nodeLabel(n *Node) string {
	switch {
	case n.Name != "":
		return "сервер «" + n.Name + "»"
	case n.Server != "":
		return "сервер «" + net.JoinHostPort(n.Server, strconv.Itoa(n.Port)) + "»"
	}
	return "сервер «" + n.ID + "»"
}

func groupLabel(g *Group) string { return "группа «" + orID(g.Name, g.ID) + "»" }
func ruleLabel(r *Rule) string   { return "правило «" + orID(r.Name, r.ID) + "»" }
func subLabel(s *Subscription) string {
	return "подписка «" + orID(s.Name, s.ID) + "»"
}

func deviceLabel(d *Device) string {
	name := d.Name
	for _, v := range []string{d.Hostname, d.IP, d.MAC, d.ID} {
		if name == "" {
			name = v
		}
	}
	return "устройство «" + name + "»"
}

func orID(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// validPortSpec — порт "443" или диапазон "27000-27100".
func validPortSpec(s string) error {
	s = strings.TrimSpace(s)
	num := func(v string) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil && validPort(n)
	}
	if a, b, ok := strings.Cut(s, "-"); ok {
		x, okx := num(a)
		y, oky := num(b)
		if !okx || !oky || x > y {
			return fmt.Errorf("неверный диапазон портов «%s»", s)
		}
		return nil
	}
	if _, ok := num(s); !ok {
		return fmt.Errorf("неверный порт «%s»", s)
	}
	return nil
}

func validAddrOrPrefix(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		_, err := netip.ParsePrefix(s)
		return err == nil
	}
	_, err := netip.ParseAddr(s)
	return err == nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// noControlSkip — поля, которые в конфигурацию ядра не попадают и могут
// законно содержать перевод строки (текст ошибки от сервера подписки).
var noControlSkip = map[string]bool{"lastError": true}

// noControl проверяет все строковые поля (и строки в срезах) объекта:
// \r, \n и NUL недопустимы — строки попадают в файлы конфигурации ядер,
// nftables, dnsmasq и pppd, где перевод строки означает новую директиву.
func noControl(what string, v any) error {
	if field := badStringField(reflect.ValueOf(v), ""); field != "" {
		return fmt.Errorf("%s: поле %s содержит недопустимые символы (перевод строки или NUL)", what, field)
	}
	return nil
}

func badStringField(v reflect.Value, name string) string {
	switch v.Kind() {
	case reflect.String:
		if strings.ContainsAny(v.String(), "\r\n\x00") {
			return name
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if f := badStringField(v.Index(i), name); f != "" {
				return f
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if f := badStringField(it.Key(), name); f != "" {
				return f
			}
			if f := badStringField(it.Value(), name); f != "" {
				return f
			}
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return badStringField(v.Elem(), name)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			fname, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
			if fname == "" || fname == "-" {
				fname = sf.Name
			}
			if noControlSkip[fname] {
				continue
			}
			full := fname
			if name != "" {
				full = name + "." + fname
			}
			if f := badStringField(v.Field(i), full); f != "" {
				return f
			}
		}
	}
	return ""
}

func ruleIDs(cfg *Config) []string {
	out := make([]string, len(cfg.Rules))
	for i, r := range cfg.Rules {
		out[i] = r.ID
	}
	return out
}

func deviceIDs(cfg *Config) []string {
	out := make([]string, len(cfg.Devices))
	for i, d := range cfg.Devices {
		out[i] = d.ID
	}
	return out
}

func subIDs(cfg *Config) []string {
	out := make([]string, len(cfg.Subscriptions))
	for i, s := range cfg.Subscriptions {
		out[i] = s.ID
	}
	return out
}
