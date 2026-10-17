package main

import "golang.org/x/sys/windows"

// Консоль Windows по умолчанию в кодировке OEM (cp866) — без этого
// русские сообщения журнала выводятся кракозябрами.
func init() {
	_ = windows.SetConsoleOutputCP(65001) // UTF-8
}
