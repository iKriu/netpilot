package model

// DNSAuto — прямой DNS выбирает NetPilot из DirectDNSAuto: первый отвечающий,
// с переключением на следующий, если текущий перестал отвечать.
const DNSAuto = "auto"

// DirectDNSAuto — серверы для DNSAuto в порядке приоритета: сначала
// зарубежные (их ответы не подменяются местными блокировками), Яндекс —
// последний запасной, когда зарубежные DoH недоступны.
var DirectDNSAuto = []string{
	"https://1.1.1.1/dns-query",
	"https://8.8.8.8/dns-query",
	"https://9.9.9.9/dns-query",
	"https://77.88.8.8/dns-query",
}
