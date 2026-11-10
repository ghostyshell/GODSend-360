package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSocks5 accepts one greeting, one CONNECT, then echoes a marker byte.
func fakeSocks5(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		head := make([]byte, 2)
		if _, err := io.ReadFull(br, head); err != nil {
			return
		}
		methods := make([]byte, head[1])
		io.ReadFull(br, methods)
		c.Write([]byte{0x05, 0x00})
		req := make([]byte, 4)
		if _, err := io.ReadFull(br, req); err != nil {
			return
		}
		switch req[3] {
		case 0x01:
			io.CopyN(io.Discard, br, 4)
		case 0x03:
			n, _ := br.ReadByte()
			io.CopyN(io.Discard, br, int64(n))
		}
		io.CopyN(io.Discard, br, 2) // port
		c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0x1f, 0x90})
		c.Write([]byte{0x42})
	}()
	return ln.Addr().String(), func() { ln.Close(); <-done }
}

func TestSocks5Dial(t *testing.T) {
	addr, closeFn := fakeSocks5(t)
	defer closeFn()

	t.Setenv("GODSEND_SCRAPE_PROXIES", "socks5://"+addr)
	p := FromEnv(nil)
	if p == nil {
		t.Fatal("pool is nil")
	}
	conn, err := p.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := make([]byte, 1)
	if _, err := io.ReadFull(conn, b); err != nil {
		t.Fatal(err)
	}
	if b[0] != 0x42 {
		t.Fatalf("echo = %x, want 42", b)
	}
}

func TestHTTPConnectDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		line, _ := br.ReadString('\n')
		if !strings.HasPrefix(line, "CONNECT example.com:443") {
			return
		}
		for {
			l, _ := br.ReadString('\n')
			if l == "\r\n" || l == "" {
				break
			}
		}
		c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		c.Write([]byte{0x07})
	}()
	t.Setenv("GODSEND_SCRAPE_PROXIES", "http://"+ln.Addr().String())
	p := FromEnv(nil)
	conn, err := p.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	b := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, b); err != nil {
		t.Fatal(err)
	}
	if b[0] != 0x07 {
		t.Fatalf("byte = %x", b[0])
	}
}

func TestFromEnvNilWithoutConfig(t *testing.T) {
	t.Setenv("GODSEND_SCRAPE_PROXIES", "")
	t.Setenv("GODSEND_TOR_CONTROL", "")
	if p := FromEnv(nil); p != nil {
		t.Fatal("pool should be nil with no config")
	}
}

func TestRotateAdvancesProxy(t *testing.T) {
	t.Setenv("GODSEND_SCRAPE_PROXIES", "socks5://127.0.0.1:1,socks5://127.0.0.1:2")
	p := FromEnv(nil)
	if p == nil {
		t.Fatal("nil pool")
	}
	a := p.nextProxy().Host
	b := p.nextProxy().Host
	if a != b {
		t.Fatalf("nextProxy should be stable between rotations: %q %q", a, b)
	}
	p.Rotate()
	c := p.nextProxy().Host
	if c == b {
		t.Fatalf("Rotate did not advance the proxy: %q -> %q", b, c)
	}
}

func TestTorNewNYM(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "PROTOCOLINFO"):
				c.Write([]byte("250-PROTOCOLINFO 1\r\n250 OK\r\n"))
			case strings.HasPrefix(line, "AUTHENTICATE"):
				c.Write([]byte("250 OK\r\n"))
			case strings.HasPrefix(line, "SIGNAL NEWNYM"):
				c.Write([]byte("250 OK\r\n"))
				select {
				case got <- "NEWNYM":
				default:
				}
			}
		}
	}()
	t.Setenv("GODSEND_SCRAPE_PROXIES", "socks5://127.0.0.1:9050")
	t.Setenv("GODSEND_TOR_CONTROL", ln.Addr().String())
	p := FromEnv(nil)
	p.Rotate()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("Tor never received SIGNAL NEWNYM")
	}
}
