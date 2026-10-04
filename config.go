package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type User struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// Forward: локальный порт на клиенте -> адрес в сети сервера (как ssh -L).
type Forward struct {
	Listen string `json:"listen"` // например 127.0.0.1:2222
	Target string `json:"target"` // например 192.168.1.50:22
}

type Config struct {
	// ---- сервер ----
	Listen       string   `json:"listen"`
	Domain       string   `json:"domain"`        // для автоматического сертификата Let's Encrypt
	CertCache    string   `json:"cert_cache"`    // папка для сертификатов
	CertFile     string   `json:"cert_file"`     // либо свой сертификат
	KeyFile      string   `json:"key_file"`
	Users        []User   `json:"users"`
	WebRoot      string   `json:"web_root"`      // папка сайта-маскировки (пусто = страница по умолчанию)
	FallbackAddr string   `json:"fallback_addr"` // либо адрес реального веб-сервера
	LogAccess    bool     `json:"log_access"`    // писать в лог адреса назначения
	Allow        []string `json:"allow"`         // разрешённые адреса дома: "192.168.1.50:22", "192.168.1.0/24:*"
	HomeOnly     bool     `json:"home_only"`     // true = разрешено ТОЛЬКО то, что в allow
	// ---- клиент ----
	Server      string    `json:"server"`       // host:443
	SNI         string    `json:"sni"`          // по умолчанию host из server
	UUID        string    `json:"uuid"`
	SocksListen string    `json:"socks_listen"` // по умолчанию 127.0.0.1:1080
	Fingerprint string    `json:"fingerprint"`  // chrome | firefox | safari | go
	Insecure    bool      `json:"insecure"`     // не проверять сертификат (только для тестов)
	Forwards    []Forward `json:"forwards"`
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{Listen: ":443", CertCache: "/var/lib/voess/certs", SocksListen: "127.0.0.1:1080", Fingerprint: "chrome"}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}
