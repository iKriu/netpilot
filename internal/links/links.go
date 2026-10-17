// Package links разбирает ссылки на узлы (vless://, vmess://, trojan://, ss://,
// hysteria2://, tuic://) и подписки.
package links

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"netpilot/internal/model"
)

// ParseMany разбирает текст с несколькими ссылками (по одной в строке).
// Если текст целиком в base64 (формат подписок), он сначала декодируется.
func ParseMany(text string) (nodes []model.Node, errs []error) {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "://") {
		if dec, err := decodeB64(text); err == nil {
			text = string(dec)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := Parse(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", shorten(line), err))
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, errs
}

func Parse(link string) (model.Node, error) {
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return model.Node{}, errors.New("это не ссылка")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESSLike(link, "vless")
	case "trojan":
		return parseVLESSLike(link, "trojan")
	case "vmess":
		return parseVMess(link)
	case "ss":
		return parseSS(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	case "tuic":
		return parseTUIC(link)
	}
	return model.Node{}, fmt.Errorf("протокол %s пока не поддерживается", scheme)
}

// vless://uuid@host:port?type=ws&security=reality&...#name
// trojan://password@host:port?security=tls&...#name
func parseVLESSLike(link, proto string) (model.Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return model.Node{}, err
	}
	n, err := base(u, proto)
	if err != nil {
		return n, err
	}
	secret := u.User.Username()
	if proto == "vless" {
		n.UUID = secret
		n.Flow = q(u, "flow")
	} else {
		n.Password = secret
	}
	applyCommon(&n, u.Query())
	if proto == "trojan" && n.Security == "" {
		n.Security = "tls" // trojan без TLS не бывает
	}
	if n.UUID == "" && n.Password == "" {
		return n, errors.New("нет идентификатора пользователя")
	}
	return n, nil
}

func applyCommon(n *model.Node, v url.Values) {
	n.Transport = strings.ToLower(v.Get("type"))
	switch n.Transport {
	case "", "raw":
		n.Transport = "tcp"
	case "splithttp":
		n.Transport = "xhttp" // старое название XHTTP
	}
	if n.Transport == "xhttp" {
		n.XHTTPMode = v.Get("mode")
	}
	n.Security = strings.ToLower(v.Get("security"))
	n.SNI = first(v.Get("sni"), v.Get("peer"))
	n.Host = v.Get("host")
	n.Path = v.Get("path")
	n.ServiceName = v.Get("serviceName")
	n.Fingerprint = v.Get("fp")
	n.PublicKey = v.Get("pbk")
	n.ShortID = v.Get("sid")
	n.Insecure = isTrue(v.Get("allowInsecure")) || isTrue(v.Get("insecure"))
	if a := v.Get("alpn"); a != "" {
		n.ALPN = strings.Split(a, ",")
	}
	// TCP с HTTP-маскировкой в ядрах соответствует транспорту http.
	if n.Transport == "tcp" && v.Get("headerType") == "http" {
		n.Transport = "http"
	}
}

type vmessJSON struct {
	PS   string          `json:"ps"`
	Add  string          `json:"add"`
	Port json.RawMessage `json:"port"`
	ID   string          `json:"id"`
	Aid  json.RawMessage `json:"aid"`
	Scy  string          `json:"scy"`
	Net  string          `json:"net"`
	Type string          `json:"type"`
	Host string          `json:"host"`
	Path string          `json:"path"`
	TLS  string          `json:"tls"`
	SNI  string          `json:"sni"`
	ALPN string          `json:"alpn"`
	FP   string          `json:"fp"`
}

// vmess://base64(json)
func parseVMess(link string) (model.Node, error) {
	raw, err := decodeB64(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		return model.Node{}, errors.New("не удалось декодировать base64")
	}
	var v vmessJSON
	if err := json.Unmarshal(raw, &v); err != nil {
		return model.Node{}, fmt.Errorf("неверный JSON: %w", err)
	}
	n := model.Node{
		Protocol:    "vmess",
		Name:        v.PS,
		Server:      v.Add,
		Port:        jsonInt(v.Port),
		UUID:        v.ID,
		AlterID:     jsonInt(v.Aid),
		Method:      first(v.Scy, "auto"),
		Transport:   first(strings.ToLower(v.Net), "tcp"),
		Host:        v.Host,
		Path:        v.Path,
		SNI:         v.SNI,
		Fingerprint: v.FP,
	}
	if n.Transport == "grpc" {
		n.ServiceName, n.Path = v.Path, ""
	}
	if n.Transport == "tcp" && v.Type == "http" {
		n.Transport = "http"
	}
	if strings.EqualFold(v.TLS, "tls") {
		n.Security = "tls"
	}
	if v.ALPN != "" {
		n.ALPN = strings.Split(v.ALPN, ",")
	}
	return n, check(n)
}

// ss://base64(method:password)@host:port#name   (SIP002)
// ss://base64(method:password@host:port)#name   (старый формат)
func parseSS(link string) (model.Node, error) {
	body, name, _ := strings.Cut(strings.TrimPrefix(link, "ss://"), "#")
	body, _, _ = strings.Cut(body, "?") // плагины пока не поддерживаются
	if !strings.Contains(body, "@") {
		dec, err := decodeB64(body)
		if err != nil {
			return model.Node{}, errors.New("не удалось декодировать base64")
		}
		body = string(dec)
	}
	at := strings.LastIndex(body, "@")
	if at < 0 {
		return model.Node{}, errors.New("нет адреса сервера")
	}
	userinfo, hostport := body[:at], body[at+1:]
	if dec, err := decodeB64(userinfo); err == nil && strings.Contains(string(dec), ":") {
		userinfo = string(dec)
	} else if un, err := url.PathUnescape(userinfo); err == nil {
		userinfo = un // SIP002 допускает незакодированный method:password
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		return model.Node{}, errors.New("нет метода шифрования")
	}
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return model.Node{}, err
	}
	n := model.Node{
		Protocol: "shadowsocks", Name: unescape(name),
		Server: host, Port: port, Method: method, Password: password,
	}
	return n, check(n)
}

// hysteria2://password@host:port?sni=..&obfs=salamander&obfs-password=..#name
func parseHysteria2(link string) (model.Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return model.Node{}, err
	}
	n, err := base(u, "hysteria2")
	if err != nil {
		return n, err
	}
	n.Password = u.User.Username()
	if p, ok := u.User.Password(); ok { // формат user:pass
		n.Password += ":" + p
	}
	v := u.Query()
	n.Security = "tls"
	n.SNI = v.Get("sni")
	n.Insecure = isTrue(v.Get("insecure"))
	n.Obfs = v.Get("obfs")
	n.ObfsPassword = v.Get("obfs-password")
	if a := v.Get("alpn"); a != "" {
		n.ALPN = strings.Split(a, ",")
	}
	return n, nil
}

// tuic://uuid:password@host:port?congestion_control=bbr&alpn=h3&sni=..#name
func parseTUIC(link string) (model.Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return model.Node{}, err
	}
	n, err := base(u, "tuic")
	if err != nil {
		return n, err
	}
	n.UUID = u.User.Username()
	n.Password, _ = u.User.Password()
	v := u.Query()
	n.Security = "tls"
	n.SNI = v.Get("sni")
	n.Insecure = isTrue(v.Get("allow_insecure")) || isTrue(v.Get("insecure"))
	n.Congestion = v.Get("congestion_control")
	if a := v.Get("alpn"); a != "" {
		n.ALPN = strings.Split(a, ",")
	}
	return n, nil
}

func base(u *url.URL, proto string) (model.Node, error) {
	port, err := strconv.Atoi(u.Port())
	n := model.Node{Protocol: proto, Server: u.Hostname(), Port: port, Name: u.Fragment}
	if err != nil {
		return n, errors.New("неверный порт")
	}
	return n, check(n)
}

func check(n model.Node) error {
	if n.Server == "" {
		return errors.New("нет адреса сервера")
	}
	if n.Port <= 0 || n.Port > 65535 {
		return errors.New("неверный порт")
	}
	return nil
}

func splitHostPort(s string) (string, int, error) {
	s, _, _ = strings.Cut(s, "/")
	host, p, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, errors.New("неверный адрес сервера")
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, errors.New("неверный порт")
	}
	return host, port, nil
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("не base64")
}

func jsonInt(raw json.RawMessage) int {
	s := strings.Trim(string(raw), `" `)
	n, _ := strconv.Atoi(s)
	return n
}

func q(u *url.URL, key string) string { return u.Query().Get(key) }

func isTrue(s string) bool { return s == "1" || strings.EqualFold(s, "true") }

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func shorten(s string) string {
	if len(s) > 48 {
		return s[:48] + "…"
	}
	return s
}
