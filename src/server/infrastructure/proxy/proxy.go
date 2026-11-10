// Package proxy provides a rotating SOCKS5/HTTP proxy dialer and a Tor control
// port helper for the scraping and host-resolution paths.
//
// Configured entirely by environment:
//
//	GODSEND_SCRAPE_PROXIES  comma list, e.g. socks5://127.0.0.1:9050,socks5://user:pass@10.0.0.5:1080
//	                        (http:// is accepted too; plain host:port means socks5)
//	GODSEND_TOR_CONTROL     Tor control port, e.g. 127.0.0.1:9051 (enables NEWNYM rotation)
//	GODSEND_TOR_PASSWORD    control port password, if any
//
// With no proxies and no control port, FromEnv returns nil and callers use the
// default transport, so this costs nothing when unused.
package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pool rotates through configured proxies and, when a Tor control port is set,
// asks Tor for a new exit (NEWNYM) so the next request leaves from a new node.
type Pool struct {
	proxies     []*url.URL
	torControl  string
	torPassword string
	logf        func(format string, args ...any)

	mu   sync.Mutex
	next int
}

// FromEnv builds a Pool from the environment, or nil when none is configured.
func FromEnv(logf func(format string, args ...any)) *Pool {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var proxies []*url.URL
	for _, raw := range strings.Split(os.Getenv("GODSEND_SCRAPE_PROXIES"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "://") {
			raw = "socks5://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			// Never log the raw entry: it may carry user:pass.
			logf("[WARN] proxy: ignoring a malformed proxy entry")
			continue
		}
		proxies = append(proxies, u)
	}
	torControl := strings.TrimSpace(os.Getenv("GODSEND_TOR_CONTROL"))
	if len(proxies) == 0 && torControl == "" {
		return nil
	}
	logf("[INFO] proxy: %d scrape prox%s, Tor control %q", len(proxies), plural(len(proxies), "y", "ies"), torControl)
	return &Pool{
		proxies:     proxies,
		torControl:  torControl,
		torPassword: os.Getenv("GODSEND_TOR_PASSWORD"),
		logf:        logf,
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Rotate advances the proxy list and, when configured, requests a new Tor exit.
func (p *Pool) Rotate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	single := len(p.proxies) <= 1
	p.next++
	p.mu.Unlock()
	// Rotation is meaningful when there are several proxies or a Tor exit to
	// change. With one fixed proxy and no control port there is nothing to do.
	if single && p.torControl == "" {
		return
	}
	if p.torControl != "" {
		if err := p.signalNewNYM(); err != nil {
			p.logf("[WARN] Tor NEWNYM: %v", err)
		} else {
			p.logf("PROXY: requested a new Tor exit (NEWNYM)")
		}
	}
}

// Transport returns base with its DialContext routed through the pool.
func (p *Pool) Transport(base *http.Transport) *http.Transport {
	if p == nil {
		return base
	}
	base.DialContext = p.DialContext
	return base
}

// DialContext dials addr through the next proxy (round-robin), or directly when
// no proxies are configured (Tor control only, via a direct dial).
func (p *Pool) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	px := p.nextProxy()
	if px == nil {
		return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
	}
	switch strings.ToLower(px.Scheme) {
	case "socks5", "socks5h", "socks":
		return socks5Dial(ctx, px, addr)
	case "http", "https":
		return httpConnectDial(ctx, px, addr)
	default:
		return nil, fmt.Errorf("proxy: unsupported scheme %q", px.Scheme)
	}
}

func (p *Pool) nextProxy() *url.URL {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.proxies) == 0 {
		return nil
	}
	px := p.proxies[p.next%len(p.proxies)]
	return px
}

// ── SOCKS5 ───────────────────────────────────────────────────────────────────

func socks5Dial(ctx context.Context, px *url.URL, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 30 * time.Second}
	c, err := d.DialContext(ctx, "tcp", px.Host)
	if err != nil {
		return nil, fmt.Errorf("socks5 dial %s: %w", px.Host, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	} else {
		c.SetDeadline(time.Now().Add(30 * time.Second))
	}
	defer c.SetDeadline(time.Time{})

	methods := []byte{0x00} // no auth
	if px.User != nil {
		methods = append(methods, 0x02) // username/password
	}
	if _, err := c.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		c.Close()
		return nil, err
	}
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		c.Close()
		return nil, err
	}
	if reply[0] != 0x05 {
		c.Close()
		return nil, fmt.Errorf("socks5: bad version %d", reply[0])
	}
	switch reply[1] {
	case 0x00:
	case 0x02:
		if px.User == nil {
			c.Close()
			return nil, fmt.Errorf("socks5: proxy wants authentication")
		}
		user := px.User.Username()
		pass, _ := px.User.Password()
		if len(user) > 255 || len(pass) > 255 {
			c.Close()
			return nil, fmt.Errorf("socks5: credentials too long")
		}
		buf := []byte{0x01, byte(len(user))}
		buf = append(buf, user...)
		buf = append(buf, byte(len(pass)))
		buf = append(buf, pass...)
		if _, err := c.Write(buf); err != nil {
			c.Close()
			return nil, err
		}
		var auth [2]byte
		if _, err := io.ReadFull(c, auth[:]); err != nil {
			c.Close()
			return nil, err
		}
		if auth[1] != 0x00 {
			c.Close()
			return nil, fmt.Errorf("socks5: authentication rejected")
		}
	default:
		c.Close()
		return nil, fmt.Errorf("socks5: no acceptable auth method (%d)", reply[1])
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		c.Close()
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		c.Close()
		return nil, err
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	} else if ip := net.ParseIP(host); ip != nil {
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	} else {
		if len(host) > 255 {
			c.Close()
			return nil, fmt.Errorf("socks5: host too long")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	req = append(req, portBytes[:]...)
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		c.Close()
		return nil, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		c.Close()
		return nil, fmt.Errorf("socks5: connect failed (code %d)", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			c.Close()
			return nil, err
		}
		skip = int(n[0])
	default:
		c.Close()
		return nil, fmt.Errorf("socks5: bad address type %d", head[3])
	}
	if _, err := io.CopyN(io.Discard, c, int64(skip+2)); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// ── HTTP CONNECT ─────────────────────────────────────────────────────────────

func httpConnectDial(ctx context.Context, px *url.URL, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 30 * time.Second}
	c, err := d.DialContext(ctx, "tcp", px.Host)
	if err != nil {
		return nil, fmt.Errorf("http proxy dial %s: %w", px.Host, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	} else {
		c.SetDeadline(time.Now().Add(30 * time.Second))
	}
	defer c.SetDeadline(time.Time{})

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if px.User != nil {
		pass, _ := px.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(px.User.Username() + ":" + pass))
		req += "Proxy-Authorization: Basic " + auth + "\r\n"
	}
	req += "\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "2") {
		c.Close()
		return nil, fmt.Errorf("http proxy CONNECT: %s", strings.TrimSpace(line))
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			c.Close()
			return nil, err
		}
		if l == "\r\n" || l == "\n" {
			break
		}
	}
	return &bufferedConn{Conn: c, r: br}, nil
}

type bufferedConn struct {
	net.Conn
	r io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// ── Tor control ──────────────────────────────────────────────────────────────

func (p *Pool) signalNewNYM() error {
	c, err := net.DialTimeout("tcp", p.torControl, 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)

	// Tor requires PROTOCOLINFO before AUTHENTICATE on some versions. It also
	// tells us whether the control port wants a password, a cookie, or nothing.
	fmt.Fprintf(c, "PROTOCOLINFO 1\r\n")
	var proto strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		proto.WriteString(line)
		if strings.HasPrefix(strings.TrimSpace(line), "5") {
			return fmt.Errorf("tor control: %s", strings.TrimSpace(line))
		}
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
	}
	info := proto.String()
	switch {
	case p.torPassword != "":
		fmt.Fprintf(c, "AUTHENTICATE %q\r\n", p.torPassword)
	case strings.Contains(info, "METHODS=") && strings.Contains(info, "COOKIE"):
		cookieFile := between(info, `COOKIEFILE="`, `"`)
		if cookieFile == "" {
			return fmt.Errorf("tor control: cookie auth advertised without COOKIEFILE")
		}
		data, err := os.ReadFile(cookieFile)
		if err != nil {
			return fmt.Errorf("tor cookie: %w", err)
		}
		fmt.Fprintf(c, "AUTHENTICATE %s\r\n", hex.EncodeToString(data))
	default:
		fmt.Fprint(c, "AUTHENTICATE\r\n")
	}
	if err := readTorReply(br); err != nil {
		return err
	}
	fmt.Fprint(c, "SIGNAL NEWNYM\r\n")
	if err := readTorReply(br); err != nil {
		return err
	}
	fmt.Fprint(c, "QUIT\r\n")
	// Tor needs a moment to build a new circuit before the exit changes.
	time.Sleep(1200 * time.Millisecond)
	return nil
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func readTorReply(br *bufio.Reader) error {
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "5") {
			return fmt.Errorf("tor control: %s", strings.TrimSpace(line))
		}
		if len(line) >= 4 && line[3] == ' ' {
			return nil
		}
	}
}
