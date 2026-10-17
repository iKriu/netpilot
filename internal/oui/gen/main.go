//go:build ignore

// Генератор базы производителей из реестра IEEE (MA-L).
//
//	curl -o oui.csv https://standards-oui.ieee.org/oui/oui.csv
//	go run internal/oui/gen/main.go oui.csv internal/oui/oui.txt.gz
package main

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"unicode"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("использование: gen <oui.csv> <oui.txt.gz>")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	m := map[string]string{}
	for _, row := range rows[1:] {
		if len(row) < 3 || row[0] != "MA-L" || len(row[1]) != 6 {
			continue
		}
		if name := Short(row[2]); name != "" {
			m[strings.ToUpper(row[1])] = name
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, m[k])
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	_ = out.Close()
	log.Printf("записей: %d", len(keys))
}

var suffixes = map[string]bool{
	"inc": true, "incorporated": true, "corp": true, "corporation": true, "co": true, "company": true,
	"ltd": true, "limited": true, "llc": true, "l.l.c": true, "gmbh": true, "ag": true, "sa": true, "s.a": true,
	"bv": true, "b.v": true, "oy": true, "ab": true, "as": true, "a/s": true, "sas": true, "spa": true, "s.p.a": true,
	"pty": true, "plc": true, "kg": true, "srl": true, "s.r.l": true, "sro": true, "s.r.o": true, "nv": true, "n.v": true,
	"technologies": true, "technology": true, "tech": true, "electronics": true, "electronic": true,
	"communications": true, "communication": true, "international": true, "group": true, "holdings": true,
	"industrial": true, "industries": true, "systems": true, "products": true, "solutions": true, "enterprise": true,
	"co.ltd": true, "co.,ltd": true, "coltd": true, "manufacturing": true, "mobile": true, "telecommunication": true,
	"telecommunications": true, "digital": true, "devices": true, "ind": true, "precision": true,
}

// Short сокращает юридическое название до узнаваемого: «Samsung Electronics Co.,Ltd» → «Samsung».
func Short(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, ","); i > 0 {
		name = name[:i]
	}
	words := strings.Fields(name)
	for len(words) > 1 {
		last := strings.ToLower(strings.Trim(words[len(words)-1], ".()"))
		if !suffixes[last] {
			break
		}
		words = words[:len(words)-1]
	}
	for i, w := range words {
		words[i] = titleIfShouting(w)
	}
	return strings.Join(words, " ")
}

// HUAWEI → Huawei; короткие аббревиатуры (HP, LG, TP-LINK) не трогаем.
func titleIfShouting(w string) string {
	if len([]rune(w)) < 5 || strings.ContainsAny(w, "-0123456789&") {
		return w
	}
	for _, r := range w {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return w
		}
	}
	rs := []rune(strings.ToLower(w))
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}
