// releasekey — ключ подписи релизов NetPilot (ed25519).
//
//	go run ./tools/releasekey -gen [-out <папка>]          — создать пару ключей
//	go run ./tools/releasekey -sign [-key <ключ>] <файл>   — записать <файл>.sig
//	go run ./tools/releasekey -verify -pub <release.pub> <файл> — проверить <файл>.sig
//
// Закрытый ключ по умолчанию лежит вне репозитория: %USERPROFILE%\.netpilot\release.key.
// Открытый (release.pub) кладётся в корень репозитория — build.ps1 встраивает
// его в программу, и она ставит только релизы, подписанные этим ключом.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	gen := flag.Bool("gen", false, "создать пару ключей")
	sign := flag.Bool("sign", false, "подписать файл")
	verify := flag.Bool("verify", false, "проверить подпись файла")
	out := flag.String("out", "", "папка для новых ключей (по умолчанию ~/.netpilot)")
	key := flag.String("key", "", "закрытый ключ (по умолчанию ~/.netpilot/release.key)")
	pub := flag.String("pub", "release.pub", "открытый ключ для -verify")
	flag.Parse()

	var err error
	switch {
	case *gen:
		dir := *out
		if dir == "" {
			dir, err = defaultDir()
		}
		if err == nil {
			var p string
			if p, err = generate(dir); err == nil {
				fmt.Println("Открытый ключ:", p)
				fmt.Printf("Закрытый ключ: %s — храните его в тайне и сделайте резервную копию.\n", filepath.Join(dir, "release.key"))
				fmt.Printf("Скопируйте %s в корень репозитория и закоммитьте.\n", filepath.Join(dir, "release.pub"))
			}
		}
	case *sign && flag.NArg() == 1:
		k := *key
		if k == "" {
			k, err = defaultKeyPath()
		}
		if err == nil {
			err = signFile(k, flag.Arg(0))
		}
		if err == nil {
			fmt.Println("Подпись:", flag.Arg(0)+".sig")
		}
	case *verify && flag.NArg() == 1:
		if err = verifyFile(*pub, flag.Arg(0)); err == nil {
			fmt.Println("Подпись верна:", flag.Arg(0))
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func defaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".netpilot"), nil
}

// defaultKeyPath — закрытый ключ по умолчанию, вне репозитория.
func defaultKeyPath() (string, error) {
	dir, err := defaultDir()
	return filepath.Join(dir, "release.key"), err
}

// generate создаёт release.key (0600) и release.pub в dir, не перезаписывая
// существующие: потеря старого ключа означает, что установленные программы
// перестанут принимать обновления.
func generate(dir string) (string, error) {
	keyPath, pubPath := filepath.Join(dir, "release.key"), filepath.Join(dir, "release.pub")
	for _, p := range []string{keyPath, pubPath} {
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("%s уже существует — не перезаписываю. Программы с прежним ключом примут только релизы, подписанные им", p)
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	enc := base64.StdEncoding.EncodeToString
	// O_EXCL — на случай гонки с другим запуском.
	f, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(enc(priv) + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	p := enc(pub)
	if err := os.WriteFile(pubPath, []byte(p+"\n"), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

func readB64(path string, size int, what string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != size {
		return nil, fmt.Errorf("%s: это не %s", path, what)
	}
	return raw, nil
}

// signFile пишет <file>.sig — подпись base64 ровно байтов файла — и сразу
// проверяет её, чтобы испорченный ключ не дал выпустить неустанавливаемый релиз.
func signFile(keyPath, file string) error {
	priv, err := readB64(keyPath, ed25519.PrivateKeySize, "закрытый ключ ed25519")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), data)
	if !ed25519.Verify(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey), data, sig) {
		return errors.New("подпись не проходит проверку — ключ повреждён")
	}
	return os.WriteFile(file+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

// verifyFile проверяет <file>.sig открытым ключом — так же, как программа при обновлении.
func verifyFile(pubPath, file string) error {
	pub, err := readB64(pubPath, ed25519.PublicKeySize, "открытый ключ ed25519")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig, err := readB64(file+".sig", ed25519.SignatureSize, "подпись ed25519")
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return fmt.Errorf("подпись %s не соответствует ключу %s", file, pubPath)
	}
	return nil
}
