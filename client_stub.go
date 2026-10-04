//go:build server

package main

import (
	"context"
	"errors"
)

// Серверная сборка (go build -tags server) не содержит клиента и библиотеки uTLS: файл меньше.
func runClient(ctx context.Context, cfg *Config) error {
	return errors.New("эта сборка только серверная (tags: server)")
}
