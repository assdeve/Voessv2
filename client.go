//go:build !server

package main

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

func dialServer(cfg *Config) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	raw, err := d.Dial("tcp", cfg.Server)
	if err != nil {
		return nil, err
	}
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	sni := cfg.SNI
	if sni == "" {
		sni, _, _ = net.SplitHostPort(cfg.Server)
	}

	var c net.Conn
	switch strings.ToLower(cfg.Fingerprint) {
	case "go", "":
		tc := tls.Client(raw, &tls.Config{
			ServerName: sni, InsecureSkipVerify: cfg.Insecure,
			NextProtos: []string{"http/1.1"}, MinVersion: tls.VersionTLS12,
		})
		if err := tc.Handshake(); err != nil {
			raw.Close()
			return nil, err
		}
		c = tc
	default:
		id := utls.HelloChrome_Auto
		switch strings.ToLower(cfg.Fingerprint) {
		case "firefox":
			id = utls.HelloFirefox_Auto
		case "safari":
			id = utls.HelloSafari_Auto
		}
		uc := utls.UClient(raw, &utls.Config{ServerName: sni, InsecureSkipVerify: cfg.Insecure}, id)
		if err := uc.Handshake(); err != nil {
			raw.Close()
			return nil, err
		}
		c = uc
	}
	return c, nil
}

func socksReply(c net.Conn, code byte) {
	c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
}

func socksHandshake(c net.Conn) (string, uint16, error) {
	var b [262]byte
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		return "", 0, err
	}
	if b[0] != 5 {
		return "", 0, errBadReq
	}
	if _, err := io.ReadFull(c, b[:int(b[1])]); err != nil {
		return "", 0, err
	}
	if _, err := c.Write([]byte{5, 0}); err != nil { // без авторизации
		return "", 0, err
	}
	if _, err := io.ReadFull(c, b[:4]); err != nil {
		return "", 0, err
	}
	if b[0] != 5 || b[1] != 1 { // поддерживается только CONNECT
		socksReply(c, 7)
		return "", 0, errBadReq
	}
	var host string
	switch b[3] {
	case 1:
		if _, err := io.ReadFull(c, b[:4]); err != nil {
			return "", 0, err
		}
		host = net.IP(b[:4]).String()
	case 4:
		if _, err := io.ReadFull(c, b[:16]); err != nil {
			return "", 0, err
		}
		host = net.IP(b[:16]).String()
	case 3:
		if _, err := io.ReadFull(c, b[:1]); err != nil {
			return "", 0, err
		}
		n := int(b[0])
		if _, err := io.ReadFull(c, b[:n]); err != nil {
			return "", 0, err
		}
		host = string(b[:n])
	default:
		socksReply(c, 8)
		return "", 0, errBadReq
	}
	if _, err := io.ReadFull(c, b[:2]); err != nil {
		return "", 0, err
	}
	return host, uint16(b[0])<<8 | uint16(b[1]), nil
}

func handleSocks(c net.Conn, cfg *Config, id UUID) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	host, port, err := socksHandshake(c)
	if err != nil {
		return
	}
	up, err := dialServer(cfg)
	if err != nil {
		log.Printf("сервер недоступен: %v", err)
		socksReply(c, 5)
		return
	}
	defer up.Close()
	if _, err := up.Write(encodeRequest(id, host, port)); err != nil {
		socksReply(c, 1)
		return
	}
	var st [1]byte
	if _, err := io.ReadFull(up, st[:]); err != nil || st[0] != statusOK {
		socksReply(c, 5)
		return
	}
	socksReply(c, 0)
	c.SetDeadline(time.Time{})
	up.SetDeadline(time.Time{})
	relay(c, up)
}

func runClient(ctx context.Context, cfg *Config) error {
	id, err := parseUUID(cfg.UUID)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.SocksListen)
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); ln.Close() }()
	log.Printf("voess2 client: SOCKS5 на %s -> %s (отпечаток: %s)", cfg.SocksListen, cfg.Server, cfg.Fingerprint)
	for _, f := range cfg.Forwards {
		go runForward(ctx, cfg, id, f)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleSocks(c, cfg, id)
	}
}

// ---- проброс портов: локальный порт -> адрес в сети сервера ----

func runForward(ctx context.Context, cfg *Config, id UUID, f Forward) {
	host, ps, err := net.SplitHostPort(f.Target)
	pn, err2 := strconv.Atoi(ps)
	if err != nil || err2 != nil || pn < 1 || pn > 65535 {
		log.Printf("forward %q: неверный target", f.Target)
		return
	}
	ln, err := net.Listen("tcp", f.Listen)
	if err != nil {
		log.Printf("forward %s: %v", f.Listen, err)
		return
	}
	go func() { <-ctx.Done(); ln.Close() }()
	log.Printf("forward: %s -> %s (через сервер)", f.Listen, f.Target)
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			up, err := dialServer(cfg)
			if err != nil {
				log.Printf("сервер недоступен: %v", err)
				return
			}
			defer up.Close()
			if _, err := up.Write(encodeRequest(id, host, uint16(pn))); err != nil {
				return
			}
			var st [1]byte
			if _, err := io.ReadFull(up, st[:]); err != nil || st[0] != statusOK {
				return
			}
			up.SetDeadline(time.Time{})
			relay(c, up)
		}()
	}
}
