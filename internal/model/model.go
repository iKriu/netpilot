// Package model — единая модель конфигурации. Она не зависит от ядра:
// адаптеры (sing-box, Xray, ...) переводят её в собственный формат.
package model

// Config — всё состояние приложения, хранится одним JSON-файлом.
type Config struct {
	Settings      Settings       `json:"settings"`
	Nodes         []Node         `json:"nodes"`
	Groups        []Group        `json:"groups"`
	Rules         []Rule         `json:"rules"`
	Devices       []Device       `json:"devices"`
	Subscriptions []Subscription `json:"subscriptions"`
	Router        Router         `json:"router"`
}

// Router — режим роутера: NetPilot сам подключается к провайдеру (WAN)
// и раздаёт интернет в локальную сеть (LAN) с NAT и DHCP.
type Router struct {
	Enabled bool `json:"enabled"`
	WAN     WAN  `json:"wan"`
	LAN     LAN  `json:"lan"`
}

type WAN struct {
	Interface string `json:"interface"` // порт, в который включён кабель провайдера
	Proto     string `json:"proto"`     // pppoe | dhcp | static

	// PPPoE: логин и пароль из договора с провайдером.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Service  string `json:"service,omitempty"` // имя сервиса — нужно редко

	// Статический IP.
	Address string `json:"address,omitempty"` // 203.0.113.10/24
	Gateway string `json:"gateway,omitempty"`

	DNS []string `json:"dns"`           // пусто — DNS провайдера
	MTU int      `json:"mtu,omitempty"` // 0 — автоматически (для PPPoE 1492)
	MAC string   `json:"mac,omitempty"` // клонирование MAC (провайдер привязан к MAC старого роутера)
}

type LAN struct {
	Interface  string `json:"interface"` // порт локальной сети
	Address    string `json:"address"`   // адрес роутера в сети: 192.168.10.1/24
	DHCP       bool   `json:"dhcp"`      // раздавать адреса устройствам
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	LeaseHours int    `json:"leaseHours"`
}

type Settings struct {
	// Core — "auto" или имя ядра ("sing-box", ...).
	Core string `json:"core"`
	// AutoStart — запускать ядро при старте приложения.
	AutoStart bool `json:"autoStart"`

	// Mode — "proxy": устройства указывают прокси сами; "transparent": NetPilot —
	// шлюз сети и перехватывает трафик устройств (только Linux: nftables + TPROXY).
	Mode string `json:"mode"`
	// TransparentScope — кого перехватывать: "all" — все устройства сети,
	// "selected" — только с маршрутом «Через прокси».
	TransparentScope string `json:"transparentScope"`
	TProxyPort       int    `json:"tproxyPort"`
	// HijackDNS — перехватывать DNS-запросы устройств: без этого провайдер видит,
	// какие сайты открываются, даже если сам трафик идёт через прокси.
	HijackDNS bool `json:"hijackDns"`
	// IPv6 в прозрачном режиме: "block" — отклонять (устройства перейдут на IPv4),
	// "intercept" — перехватывать как IPv4, "off" — не трогать.
	IPv6 string `json:"ipv6"`
	// CoreFailure — что делать, если ядро неожиданно завершилось: "open" (или "") —
	// снять перехват, устройства идут напрямую; "closed" — оставить перехват,
	// интернет заблокирован, пока ядро не вернётся (kill switch).
	CoreFailure string `json:"coreFailure"`

	// Прокси-вход (HTTP + SOCKS5 на одном порту).
	ListenAddr  string `json:"listenAddr"`
	MixedPort   int    `json:"mixedPort"`
	SystemProxy bool   `json:"systemProxy"` // выставить системный прокси (Windows/macOS)

	// Что делать с трафиком, не попавшим ни под одно правило.
	DefaultAction string `json:"defaultAction"` // "proxy" | "direct"
	DefaultTarget string `json:"defaultTarget"` // id узла или группы; "" — первая группа/узел
	BypassLAN     bool   `json:"bypassLan"`     // локальные адреса всегда напрямую

	DNSRemote string `json:"dnsRemote"` // DNS для проксируемого трафика
	DNSDirect string `json:"dnsDirect"` // DNS для прямого трафика
	// DNSViaProxy — отвечать устройствам через DNS для прокси (идёт через узел).
	// По умолчанию выключено: для шлюза всей сети DNS не должен зависеть от
	// работоспособности узлов. Сайты через прокси узел всё равно открывает по имени.
	DNSViaProxy bool `json:"dnsViaProxy"`

	TestURL string `json:"testUrl"` // URL для проверки задержки узлов
	// PingMethod — как проверять задержку кнопкой «Проверить»: auto | http | tcp | icmp.
	// Автогруппы всегда проверяют настоящим запросом через сервер.
	PingMethod string `json:"pingMethod"`

	// Оформление панели: classic | m3 (Material 3 Expressive). Для m3 — палитра
	// (indigo | green | coral) и тема (system | light | dark).
	Design       string `json:"design,omitempty"`
	DesignScheme string `json:"designScheme,omitempty"`
	DesignMode   string `json:"designMode,omitempty"`
	LogLevel     string `json:"logLevel"`
}

// Node — один прокси-сервер. Плоская структура: так её проще показывать
// в интерфейсе и переводить в любое ядро.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // vless | vmess | trojan | shadowsocks | hysteria2 | tuic
	Server   string `json:"server"`
	Port     int    `json:"port"`

	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Method   string `json:"method,omitempty"` // shadowsocks: шифр; vmess: security
	Flow     string `json:"flow,omitempty"`   // vless: xtls-rprx-vision
	AlterID  int    `json:"alterId,omitempty"`

	Transport   string `json:"transport,omitempty"` // tcp | ws | grpc | http | httpupgrade | xhttp
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	XHTTPMode   string `json:"xhttpMode,omitempty"` // xhttp: auto | packet-up | stream-up | stream-one

	Security    string   `json:"security,omitempty"` // none | tls | reality
	SNI         string   `json:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Insecure    bool     `json:"insecure,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	PublicKey   string   `json:"publicKey,omitempty"` // reality
	ShortID     string   `json:"shortId,omitempty"`   // reality

	Obfs         string `json:"obfs,omitempty"` // hysteria2: salamander
	ObfsPassword string `json:"obfsPassword,omitempty"`
	UpMbps       int    `json:"upMbps,omitempty"`
	DownMbps     int    `json:"downMbps,omitempty"`
	Congestion   string `json:"congestion,omitempty"` // tuic

	SubscriptionID string `json:"subscriptionId,omitempty"`
}

// Group — набор узлов с общей стратегией выбора.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "auto" — быстрейший рабочий; "select" — выбор вручную
	// Узлы группы: явный список и/или фильтр по имени (регулярное выражение,
	// например "DE|Germany|🇩🇪"). Пусто и то и другое — все узлы.
	NodeIDs []string `json:"nodeIds"`
	Filter  string   `json:"filter"`
	// Exclude — какие серверы не брать, даже если подошли по фильтру
	// (регулярное выражение по имени, например "LTE" — мобильные).
	Exclude string `json:"exclude,omitempty"`
}

// Rule — правило «Кто → Что → Куда». Пустой блок означает «любой».
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Who     Who    `json:"who"`
	What    What   `json:"what"`
	Where   Where  `json:"where"`
}

type Who struct {
	DeviceIDs []string `json:"deviceIds"`
	Tags      []string `json:"tags"`  // группы устройств («Дети», «ТВ»)
	CIDRs     []string `json:"cidrs"` // произвольные адреса/подсети
}

type What struct {
	Apps    []string `json:"apps"`    // id профилей приложений
	Lists   []string `json:"lists"`   // id внешних списков (Re:filter и др.)
	Domains []string `json:"domains"` // example.com, full:a.b.c, keyword:xx, regexp:...
	IPs     []string `json:"ips"`     // CIDR
	Geosite []string `json:"geosite"` // категории geosite: youtube, category-ru...
	Geoip   []string `json:"geoip"`   // коды стран: ru, us...
	Ports   []string `json:"ports"`   // "443", "27000-27100"
	Network string   `json:"network"` // "" | "tcp" | "udp"
	// Protocols — протокол, распознанный по содержимому соединения:
	// "bittorrent" — торренты (qBittorrent, Transmission и др.) на любых портах.
	Protocols []string `json:"protocols,omitempty"`
}

type Where struct {
	Action string `json:"action"` // proxy | direct | block
	Target string `json:"target"` // для proxy: id узла или группы; "" — по умолчанию
}

// Device — устройство в сети. Узнаётся по MAC: если IP не закреплён,
// сканер сам обновляет адрес, и правила продолжают работать.
type Device struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"` // computer | laptop | phone | tablet | tv | console | printer | iot | network | other
	IP       string   `json:"ip"`
	MAC      string   `json:"mac"`
	Hostname string   `json:"hostname,omitempty"` // имя, которое сообщает само устройство
	Gateway  bool     `json:"gateway,omitempty"`  // шлюз по умолчанию для этой машины (роутер)
	Vendor   string   `json:"vendor,omitempty"`   // производитель по MAC (реестр IEEE)
	Tags     []string `json:"tags"`

	// Pinned — IP закреплён за устройством (статическая аренда DHCP).
	Pinned bool `json:"pinned"`

	// Route — маршрут всего трафика устройства. Важнее правил.
	// "" — по правилам; proxy | direct | block.
	Route       string `json:"route"`
	RouteTarget string `json:"routeTarget,omitempty"` // для proxy: id узла или группы

	FirstSeen int64 `json:"firstSeen,omitempty"`
	// SeenAt — когда устройство последний раз было в сети, LastUsed — когда
	// последний раз пускало трафик через NetPilot. Сохраняются не чаще раза
	// в 12 часов (на роутере частая запись изнашивает флеш).
	SeenAt   int64 `json:"seenAt,omitempty"`
	LastUsed int64 `json:"lastUsed,omitempty"`
}

type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt int64  `json:"updatedAt"` // unix-время последнего обновления
	LastError string `json:"lastError,omitempty"`
	// Interval — автообновление раз в N часов; 0 — только вручную.
	Interval int `json:"interval"`
}

// Defaults — конфигурация «из коробки».
func Defaults() *Config {
	return &Config{
		Settings: Settings{
			Core:             "auto",
			Mode:             "proxy",
			TransparentScope: "all",
			TProxyPort:       7893,
			HijackDNS:        true,
			IPv6:             "block",
			ListenAddr:       "127.0.0.1",
			MixedPort:        7890,
			DefaultAction:    "proxy",
			BypassLAN:        true,
			DNSRemote:        "https://1.1.1.1/dns-query",
			DNSDirect:        DNSAuto,
			TestURL:          "https://www.gstatic.com/generate_204",
			PingMethod:       "auto",
			LogLevel:         "info",
		},
		Nodes:         []Node{},
		Groups:        []Group{},
		Rules:         []Rule{},
		Devices:       []Device{},
		Subscriptions: []Subscription{},
		Router: Router{
			WAN: WAN{Proto: "pppoe", DNS: []string{}},
			LAN: LAN{
				Address: "192.168.10.1/24", DHCP: true,
				RangeStart: "192.168.10.100", RangeEnd: "192.168.10.250", LeaseHours: 12,
			},
		},
	}
}
