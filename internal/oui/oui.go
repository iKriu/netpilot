// Package oui определяет производителя устройства по первым трём байтам MAC.
// База — реестр IEEE MA-L, встроена в программу (см. gen/main.go).
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed oui.txt.gz
var data []byte

// Хранение компактное: отсортированные префиксы и индексы в список названий.
// Так база занимает в памяти ~0,5 МБ, а не несколько мегабайт у map.
var (
	once     sync.Once
	prefixes []uint32
	nameIdx  []uint16
	names    []string
)

func load() {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	index := map[string]uint16{}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		p, name, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			continue
		}
		i, ok := index[name]
		if !ok {
			i = uint16(len(names))
			index[name] = i
			names = append(names, name)
		}
		prefixes = append(prefixes, uint32(v))
		nameIdx = append(nameIdx, i)
	}
}

// Vendor возвращает производителя или "", если он неизвестен
// (в том числе для случайных MAC, которыми телефоны скрывают настоящий адрес).
func Vendor(mac string) string {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) < 3 || hw[0]&0x02 != 0 {
		return ""
	}
	once.Do(load)
	key := uint32(hw[0])<<16 | uint32(hw[1])<<8 | uint32(hw[2])
	i := sort.Search(len(prefixes), func(i int) bool { return prefixes[i] >= key })
	if i < len(prefixes) && prefixes[i] == key {
		return names[nameIdx[i]]
	}
	return ""
}

// TypeHint — вероятный тип устройства по производителю. "" — не ясно
// (Apple или Samsung выпускают и телефоны, и компьютеры).
func TypeHint(vendor string) string {
	v := strings.ToLower(vendor)
	for _, h := range hints {
		for _, k := range h.keys {
			if strings.Contains(v, k) {
				return h.typ
			}
		}
	}
	return ""
}

var hints = []struct {
	typ  string
	keys []string
}{
	{"iot", []string{"espressif", "tuya", "shelly", "allterco", "sonoff", "itead", "yeelight", "hikvision", "dahua", "ezviz", "ring", "ecobee", "nest labs", "philips lighting", "signify", "roborock", "dreame", "broadlink", "aqara", "lumi united", "wyze", "reolink"}},
	{"console", []string{"sony interactive", "nintendo", "valve"}},
	{"tv", []string{"roku", "vizio", "tcl", "hisense", "skyworth"}},
	{"printer", []string{"canon", "seiko epson", "epson", "brother industries", "kyocera", "xerox", "lexmark", "ricoh"}},
	{"network", []string{"tp-link", "keenetic", "zyxel", "mikrotik", "routerboard", "ubiquiti", "netgear", "d-link", "cisco", "juniper", "aruba", "ruckus", "tenda", "mercusys", "cudy", "arcadyan", "sagemcom", "sercomm", "huawei device"}},
	{"computer", []string{"raspberry pi", "micro-star", "asustek", "gigabyte", "intel corporate", "dell", "lenovo", "hewlett", "synology", "qnap", "zimaboard", "icewhale"}},
}

// Для тестов и отладки.
func Count() int {
	once.Do(load)
	return len(prefixes)
}
