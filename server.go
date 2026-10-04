package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

type server struct {
	cfg   *Config
	tls   *tls.Config
	users map[UUID]string
	web   *pipeListener
	allow []allowRule
}

func buildTLS(cfg *Config) (*tls.Config, error) {
	if cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}, nil
	}
	if cfg.Domain == "" {
		return nil, errors.New("укажите domain (авто-сертификат) или cert_file/key_file")
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.Domain),
		Cache:      autocert.DirCache(cfg.CertCache),
	}
	c := m.TLSConfig()
	// только http/1.1 (h2 сломал бы «сырой» поток) + служебный протокол для Let's Encrypt
	c.NextProtos = []string{"http/1.1", acme.ALPNProto}
	c.MinVersion = tls.VersionTLS12
	return c, nil
}

func runServer(ctx context.Context, cfg *Config) error {
	users := map[UUID]string{}
	for _, u := range cfg.Users {
		id, err := parseUUID(u.UUID)
		if err != nil {
			return err
		}
		users[id] = u.Name
	}
	if len(users) == 0 {
		return errors.New("в конфиге нет users")
	}
	tlsCfg, err := buildTLS(cfg)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	rules, err := parseAllow(cfg.Allow)
	if err != nil {
		return err
	}
	s := &server{cfg: cfg, tls: tlsCfg, users: users, web: newPipeListener(), allow: rules}
	web := &http.Server{Handler: decoyHandler(cfg.WebRoot), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go web.Serve(s.web)
	go func() { <-ctx.Done(); ln.Close(); web.Close() }()
	log.Printf("voess2 server: %s, пользователей: %d", cfg.Listen, len(users))

	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handle(raw)
	}
}

func (s *server) handle(raw net.Conn) {
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	tc := tls.Server(raw, s.tls)
	if err := tc.Handshake(); err != nil {
		raw.Close()
		return
	}
	if tc.ConnectionState().NegotiatedProtocol == acme.ALPNProto {
		tc.Close() // проверка домена Let's Encrypt
		return
	}
	br := bufio.NewReaderSize(tc, 4096)
	c := &bufferedConn{Conn: tc, br: br}
	if head, err := br.Peek(17); err == nil && head[0] == version {
		var id UUID
		copy(id[:], head[1:17])
		if name, ok := s.users[id]; ok {
			s.proxy(c, name)
			return
		}
	}
	s.fallback(c)
}

func blockedIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func (s *server) proxy(c *bufferedConn, name string) {
	defer c.Close()
	host, port, err := readRequest(c.br)
	if err != nil {
		return
	}
	c.SetDeadline(time.Time{})
	d := net.Dialer{
		Timeout: 10 * time.Second,
		// по умолчанию запрещены localhost и внутренние сети; исключения задаёт allow
		Control: func(network, address string, _ syscall.RawConn) error {
			h, p, _ := net.SplitHostPort(address)
			pn, _ := strconv.Atoi(p)
			if !s.permit(net.ParseIP(h), pn) {
				return errors.New("blocked address")
			}
			return nil
		},
	}
	t, err := d.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		c.Write([]byte{statusFail})
		return
	}
	defer t.Close()
	if _, err := c.Write([]byte{statusOK}); err != nil {
		return
	}
	if s.cfg.LogAccess {
		log.Printf("%s -> %s:%d", name, host, port)
	}
	relay(c, t)
}

// fallback: всё, что не наш клиент (браузер, сканер), получает обычный сайт.
func (s *server) fallback(c *bufferedConn) {
	c.SetDeadline(time.Time{})
	if s.cfg.FallbackAddr != "" {
		fb, err := net.DialTimeout("tcp", s.cfg.FallbackAddr, 5*time.Second)
		if err != nil {
			c.Close()
			return
		}
		relay(c, fb)
		return
	}
	select {
	case s.web.ch <- c:
	case <-time.After(5 * time.Second):
		c.Close()
	}
}

// ---- встроенный сайт-маскировка ----

const defaultPage = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Welcome</title>
<style>body{font-family:sans-serif;max-width:40em;margin:4em auto;color:#333}</style></head>
<body><h1>Welcome</h1><p>This site is under construction.</p></body></html>`

func decoyHandler(root string) http.Handler {
	if root != "" {
		return http.FileServer(http.Dir(root))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, defaultPage)
	})
}

type pipeListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// ---- список разрешённых адресов (allow) ----

type allowRule struct {
	cidr *net.IPNet
	port int // 0 = любой порт
}

func parseAllow(list []string) ([]allowRule, error) {
	var out []allowRule
	for _, e := range list {
		i := strings.LastIndex(e, ":")
		if i < 0 {
			return nil, fmt.Errorf("allow %q: нужен формат адрес:порт", e)
		}
		host, ps := e[:i], e[i+1:]
		if !strings.Contains(host, "/") {
			if ip := net.ParseIP(host); ip != nil {
				if ip.To4() != nil {
					host += "/32"
				} else {
					host += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(host)
		if err != nil {
			return nil, fmt.Errorf("allow %q: %v", e, err)
		}
		port := 0
		if ps != "*" {
			p, err := strconv.Atoi(ps)
			if err != nil || p < 1 || p > 65535 {
				return nil, fmt.Errorf("allow %q: неверный порт", e)
			}
			port = p
		}
		out = append(out, allowRule{n, port})
	}
	return out, nil
}

func (s *server) permit(ip net.IP, port int) bool {
	if ip == nil {
		return false
	}
	for _, a := range s.allow {
		if a.cidr.Contains(ip) && (a.port == 0 || a.port == port) {
			return true
		}
	}
	if s.cfg.HomeOnly {
		return false
	}
	return !blockedIP(ip)
}
