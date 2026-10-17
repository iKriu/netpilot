package links

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"netpilot/internal/model"
)

// Подписки приходят со сторонних серверов, поэтому парсер гоняется фаззером:
// он не должен паниковать и не должен пропускать в узлы строки, ломающие конфиги ядер.

var fuzzLinks = []string{
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=ab12&flow=xtls-rprx-vision#%F0%9F%87%A9%F0%9F%87%AA%20Germany",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@[2001:db8::1]:8443?type=xhttp&mode=packet-up&path=%2Fx&security=tls&alpn=h2,http/1.1#XH",
	"vless://id@h.example.com:80?type=tcp&headerType=http&host=a.com#HTTP",
	"trojan://secret@tr.example.com:8443?type=grpc&serviceName=gun&sni=tr.example.com#TR",
	"trojan://p%40ss@tr.example.com:443?type=ws&path=%2Fws&host=cdn.example.com&allowInsecure=1",
	"vmess://" + base64.StdEncoding.EncodeToString([]byte(
		`{"v":"2","ps":"VM","add":"vm.example.com","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":0,"net":"ws","host":"cdn.example.com","path":"/ws?ed=2048","tls":"tls","sni":"vm.example.com"}`)),
	"vmess://" + base64.RawURLEncoding.EncodeToString([]byte(
		`{"ps":"G","add":"g.example.com","port":443,"id":"x","net":"grpc","path":"svc","alpn":"h2"}`)),
	"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass")) + "@5.6.7.8:8388#SS",
	"ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw@ss.example.com:443")) + "#Old",
	"ss://2022-blake3-aes-128-gcm:YctPZ6U7xPPcU%2Bgp3u%2B0tx%2FtRizJN9K8y%2BuKlW2qjlI%3D@[2001:db8::1]:443#SS2022",
	"ss://YWVzLTEyOC1nY206dGVzdA@192.168.100.1:8888/?plugin=obfs-local%3Bobfs%3Dhttp#Plugin",
	"hysteria2://pw@hy.example.com:443?sni=hy.example.com&obfs=salamander&obfs-password=x&insecure=1#HY",
	"hy2://user:pass@hy.example.com:8443/?alpn=h3#HY2",
	"tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@tu.example.com:443?congestion_control=bbr&alpn=h3&sni=tu.example.com#TU",
	// Сломанные и враждебные входы.
	"",
	"://",
	"vless://",
	"vless://id@host:0",
	"vless://id@host:99999",
	"vless://id@:443",
	"vless://id@host:443#a%0Ab",
	"vless://id@host:443?path=%0Aevil#x",
	"trojan://%00@host:443",
	"vmess://not-base64!!",
	"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"a\nb","port":"1","id":"x"}`)),
	"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"h","port":"-1"}`)),
	"vmess://" + base64.StdEncoding.EncodeToString([]byte(`[1,2,3]`)),
	"ss://@:",
	"ss://bm9jb2xvbg@h:1",
	"hy2://@[::1",
	"tuic://a:b@h:65536",
	"unknown://x",
}

func FuzzParse(f *testing.F) {
	for _, s := range fuzzLinks {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, link string) {
		n, err := Parse(link)
		if err != nil {
			if !reflect.DeepEqual(n, model.Node{}) {
				t.Fatalf("при ошибке вернулся непустой узел: %+v", n)
			}
			return
		}
		checkNode(t, n)
	})
}

func FuzzParseMany(f *testing.F) {
	all := strings.Join(fuzzLinks, "\n")
	f.Add(all)
	f.Add(base64.StdEncoding.EncodeToString([]byte(all)))
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(all)))
	f.Add(base64.StdEncoding.EncodeToString([]byte(strings.Join(fuzzLinks[:6], "\r\n"))))
	f.Add("# комментарий\n// ещё\n\n" + fuzzLinks[0] + "\r\n" + fuzzLinks[3])
	f.Add("   \n\t\n")
	f.Add("!!!не-base64!!!")
	f.Fuzz(func(t *testing.T, text string) {
		nodes, _ := ParseMany(text)
		for _, n := range nodes {
			checkNode(t, n)
		}
	})
}

var fuzzProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true, "hysteria2": true, "tuic": true,
}

func checkNode(t *testing.T, n model.Node) {
	t.Helper()
	if n.Server == "" {
		t.Fatalf("пустой сервер: %+v", n)
	}
	if n.Port < 1 || n.Port > 65535 {
		t.Fatalf("неверный порт %d: %+v", n.Port, n)
	}
	if !fuzzProtocols[n.Protocol] {
		t.Fatalf("неизвестный протокол %q", n.Protocol)
	}
	// Обходим все строковые поля (и ALPN) рефлексией, чтобы новое поле Node
	// не выпало из проверки.
	v := reflect.ValueOf(n)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		var ss []string
		switch f.Kind() {
		case reflect.String:
			ss = []string{f.String()}
		case reflect.Slice:
			if f.Type().Elem().Kind() == reflect.String {
				ss = f.Interface().([]string)
			}
		}
		for _, s := range ss {
			if strings.ContainsAny(s, "\n\r\x00") {
				t.Fatalf("поле %s содержит управляющий символ: %q", v.Type().Field(i).Name, s)
			}
		}
	}
}
