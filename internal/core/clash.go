package core

import (
	"context"
	"io"
	"net/http"
)

// Clash — адрес и ключ Clash-совместимого API ядра. Ключ обязателен: API
// слушает 127.0.0.1, но без ключа любой локальный процесс (другой
// пользователь Windows, контейнер с сетью хоста) мог бы переключать серверы,
// читать и закрывать соединения.
type Clash struct {
	Addr   string // "" — API нет
	Secret string
}

// Request готовит запрос к API с ключом. Все обращения к Clash API идут
// через него, чтобы заголовок нельзя было забыть.
func (c Clash) Request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.Addr+path, body)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
