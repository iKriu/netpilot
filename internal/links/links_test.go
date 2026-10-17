package links

import (
	"encoding/base64"
	"testing"

	"netpilot/internal/model"
)

func TestParse(t *testing.T) {
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(
		`{"v":"2","ps":"VM","add":"vm.example.com","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":0,"net":"ws","host":"cdn.example.com","path":"/ws?ed=2048","tls":"tls","sni":"vm.example.com"}`))
	cases := []struct {
		link  string
		check func(t *testing.T, n model.Node)
	}{
		{"vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=ab12&flow=xtls-rprx-vision#%F0%9F%87%A9%F0%9F%87%AA%20Germany",
			func(t *testing.T, n model.Node) {
				eq(t, n.Protocol, "vless")
				eq(t, n.Name, "🇩🇪 Germany")
				eq(t, n.Security, "reality")
				eq(t, n.PublicKey, "PUBKEY")
				eq(t, n.Flow, "xtls-rprx-vision")
			}},
		{"trojan://secret@tr.example.com:8443?type=grpc&serviceName=gun&sni=tr.example.com#TR",
			func(t *testing.T, n model.Node) {
				eq(t, n.Security, "tls")
				eq(t, n.Transport, "grpc")
				eq(t, n.ServiceName, "gun")
			}},
		{vmess, func(t *testing.T, n model.Node) {
			eq(t, n.Server, "vm.example.com")
			eq(t, n.Transport, "ws")
			eq(t, n.Host, "cdn.example.com")
		}},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass")) + "@5.6.7.8:8388#SS",
			func(t *testing.T, n model.Node) {
				eq(t, n.Method, "aes-256-gcm")
				eq(t, n.Password, "pass")
				eq(t, n.Port, 8388)
			}},
		{"ss://2022-blake3-aes-128-gcm:YctPZ6U7xPPcU%2Bgp3u%2B0tx%2FtRizJN9K8y%2BuKlW2qjlI%3D@[2001:db8::1]:443#SS2022",
			func(t *testing.T, n model.Node) {
				eq(t, n.Method, "2022-blake3-aes-128-gcm")
				eq(t, n.Password, "YctPZ6U7xPPcU+gp3u+0tx/tRizJN9K8y+uKlW2qjlI=")
				eq(t, n.Server, "2001:db8::1")
			}},
		{"hy2://pw@hy.example.com:443?sni=hy.example.com&obfs=salamander&obfs-password=x&insecure=1#HY",
			func(t *testing.T, n model.Node) {
				eq(t, n.Protocol, "hysteria2")
				eq(t, n.Obfs, "salamander")
				eq(t, n.Insecure, true)
			}},
		{"tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@tu.example.com:443?congestion_control=bbr&alpn=h3#TU",
			func(t *testing.T, n model.Node) {
				eq(t, n.Password, "pw")
				eq(t, n.Congestion, "bbr")
			}},
	}
	for _, c := range cases {
		n, err := Parse(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.link, err)
		}
		c.check(t, n)
	}
}

func TestParseManySubscription(t *testing.T) {
	body := "trojan://a@h1.example.com:443#A\nvless://b831381d-6324-4d53-ad4f-8cda48b30811@h2.example.com:443#B\nbad://x\n"
	nodes, errs := ParseMany(base64.StdEncoding.EncodeToString([]byte(body)))
	if len(nodes) != 2 || len(errs) != 1 {
		t.Fatalf("nodes=%d errs=%d", len(nodes), len(errs))
	}
}

// Управляющие символы из подписки не должны попасть в конфиг ядра:
// имя чистится, а узел с мусором в технических полях отбрасывается.
func TestParseControlChars(t *testing.T) {
	n, err := Parse("vless://id@host.example.com:443#a%0D%0Ab%00c")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, n.Name, "a  b c")
	for _, link := range []string{
		"vless://id@host:443?path=%0Aevil",
		"trojan://%00@host:443",
		"tuic://u:p@h:443?alpn=h3,%0A",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"a\nb","port":"1","id":"x"}`)),
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"a b","port":"1","id":"x"}`)),
	} {
		if _, err := Parse(link); err == nil {
			t.Errorf("%q: ожидалась ошибка", link)
		}
	}
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
