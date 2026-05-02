package v2raybale

import (
	"context"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/projectmithra/bale-transport/bale"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	sHTTP "github.com/sagernet/sing/protocol/http"
	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
)

var _ adapter.V2RayClientTransport = (*Client)(nil)

// Client is the SingBox V2RayClientTransport implementation for Bale protocol mimicry.
type Client struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
	requestURL url.URL
	headers    http.Header
	options    option.V2RayBaleOptions
}

// NewClient returns a client configured to dial a Bale-camouflaged WebSocket endpoint.
func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayBaleOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	if tlsConfig != nil {
		if len(tlsConfig.NextProtos()) == 0 {
			tlsConfig.SetNextProtos([]string{"http/1.1"})
		}
		dialer = tls.NewDialer(dialer, tlsConfig)
	}

	var requestURL url.URL
	if tlsConfig == nil {
		requestURL.Scheme = "ws"
	} else {
		requestURL.Scheme = "wss"
	}
	requestURL.Host = serverAddr.String()

	path := options.Path
	if path == "" {
		path = "/w"
	}
	if err := sHTTP.URLSetPath(&requestURL, path); err != nil {
		return nil, E.Cause(err, "parse path")
	}
	if !strings.HasPrefix(requestURL.Path, "/") {
		requestURL.Path = "/" + requestURL.Path
	}

	headers := options.Headers.Build()

	origin := options.Origin
	if origin == "" {
		origin = "https://web.bale.ai"
	}
	headers.Set("Origin", origin)

	acceptLanguage := options.AcceptLanguage
	if acceptLanguage == "" {
		acceptLanguage = "fa-IR,fa;q=0.9,en-US;q=0.8,en;q=0.7"
	}
	headers.Set("Accept-Language", acceptLanguage)
	headers.Set("X-Bale-Proto", "1")
	// headers.Set("Sec-WebSocket-Protocol", "binary") // disabled: CF Workers dont echo subprotocol

	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	}

	if host := headers.Get("Host"); host != "" {
		headers.Del("Host")
		requestURL.Host = host
	}
	if options.WorkerHost != "" {
		requestURL.Host = options.WorkerHost
	}

	return &Client{
		dialer:     dialer,
		serverAddr: serverAddr,
		requestURL: requestURL,
		headers:    headers,
		options:    options,
	}, nil
}

// DialContext opens a TCP connection, upgrades to WebSocket, and performs the
// Bale protobuf handshake. The returned net.Conn transparently wraps and unwraps
// all subsequent payloads in Bale ClientEnvelope / ServerEnvelope protobuf frames.
func (c *Client) DialContext(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.serverAddr)
	if err != nil {
		return nil, err
	}

	d := ws.Dialer{Header: ws.HandshakeHeaderHTTP(c.headers)}
	if _, _, err := d.Upgrade(conn, &c.requestURL); err != nil {
		conn.Close()
		return nil, E.Cause(err, "bale ws upgrade")
	}

	wc := &baleWs{
		conn:   conn,
		state:  ws.StateClientSide,
		reader: &wsutil.Reader{Source: conn, State: ws.StateClientSide, OnIntermediate: wsutil.ControlFrameHandler(conn, ws.StateClientSide)},
		ctrlH:  wsutil.ControlFrameHandler(conn, ws.StateClientSide),
	}

	if err := baleHandshake(wc); err != nil {
		conn.Close()
		return nil, E.Cause(err, "bale handshake")
	}

	return newBaleConn(wc, c.serverAddr), nil
}

// Close is a no-op: per-connection cleanup happens on the baleConn itself.
func (c *Client) Close() error { return nil }

// ----------------------------------------------------------------------------
// low-level WebSocket wrapper
// ----------------------------------------------------------------------------

type baleWs struct {
	conn   net.Conn
	state  ws.State
	reader *wsutil.Reader
	ctrlH  wsutil.FrameHandlerFunc
	wmu    sync.Mutex
}

func (w *baleWs) write(data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return wsutil.WriteMessage(w.conn, w.state, ws.OpBinary, data)
}

func (w *baleWs) read() ([]byte, error) {
	for {
		hdr, err := w.reader.NextFrame()
		if err != nil {
			return nil, err
		}
		if hdr.OpCode.IsControl() {
			if err := w.ctrlH(hdr, w.reader); err != nil {
				return nil, err
			}
			continue
		}
		if hdr.OpCode&ws.OpBinary == 0 {
			if err := w.reader.Discard(); err != nil {
				return nil, err
			}
			continue
		}
		buf := make([]byte, hdr.Length)
		if _, err := io.ReadFull(w.reader, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
}

func baleHandshake(w *baleWs) error {
	if err := w.write(bale.EncodeHandshakeRequest()); err != nil {
		return err
	}
	if err := w.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	defer w.conn.SetReadDeadline(time.Time{})

	for {
		data, err := w.read()
		if err != nil {
			return err
		}
		env, err := bale.DecodeServerEnvelope(data)
		if err != nil {
			return err
		}
		if env.Type == "handshake" {
			return nil
		}
	}
}

// ----------------------------------------------------------------------------
// baleConn — net.Conn over protobuf-wrapped WebSocket
// ----------------------------------------------------------------------------

type baleConn struct {
	w      *baleWs
	rbuf   []byte
	ridx   int32
	pid    int32
	closed int32
	done   chan struct{}
	sa     M.Socksaddr
}

func newBaleConn(w *baleWs, sa M.Socksaddr) *baleConn {
	c := &baleConn{w: w, done: make(chan struct{}), sa: sa}
	go c.pinger()
	return c
}

func (c *baleConn) Read(b []byte) (int, error) {
	if len(c.rbuf) > 0 {
		n := copy(b, c.rbuf)
		c.rbuf = c.rbuf[n:]
		return n, nil
	}

	for {
		if atomic.LoadInt32(&c.closed) != 0 {
			return 0, io.EOF
		}
		data, err := c.w.read()
		if err != nil {
			return 0, err
		}
		env, err := bale.DecodeServerEnvelope(data)
		if err != nil {
			continue
		}

		switch env.Type {
		case "response", "update":
			payload, err := bale.ExtractPayload(env)
			if err != nil || payload == nil {
				continue
			}
			clean := bale.StripPadding(payload)
			if len(clean) == 0 {
				continue
			}
			n := copy(b, clean)
			if n < len(clean) {
				c.rbuf = clean[n:]
			}
			return n, nil
		case "terminate":
			return 0, io.EOF
		}
	}
}

func (c *baleConn) Write(b []byte) (int, error) {
	if atomic.LoadInt32(&c.closed) != 0 {
		return 0, io.ErrClosedPipe
	}
	idx := int(atomic.AddInt32(&c.ridx, 1))
	if err := c.w.write(bale.WrapTunnelData(b, idx)); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *baleConn) Close() error {
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		return nil
	}
	close(c.done)
	return c.w.conn.Close()
}

func (c *baleConn) LocalAddr() net.Addr                { return c.w.conn.LocalAddr() }
func (c *baleConn) RemoteAddr() net.Addr               { return c.w.conn.RemoteAddr() }
func (c *baleConn) SetDeadline(t time.Time) error      { return os.ErrInvalid }
func (c *baleConn) SetReadDeadline(t time.Time) error  { return os.ErrInvalid }
func (c *baleConn) SetWriteDeadline(t time.Time) error { return os.ErrInvalid }
func (c *baleConn) NeedAdditionalReadDeadline() bool   { return true }
func (c *baleConn) Upstream() any                      { return c.w.conn }

// pinger mimics Bale's production web client: one warm-up ping within the first
// 20–30 seconds, then pings at 25s ±3s. Uses math/rand because the jitter is
// only a timing-distribution obfuscator, not a secret.
func (c *baleConn) pinger() {
	initial := 20*time.Second + time.Duration(rand.Int63n(int64(10*time.Second)))
	select {
	case <-time.After(initial):
	case <-c.done:
		return
	}
	for atomic.LoadInt32(&c.closed) == 0 {
		id := int(atomic.AddInt32(&c.pid, 1))
		if err := c.w.write(bale.EncodePing(id)); err != nil {
			return
		}
		jitter := time.Duration(rand.Int63n(6000))*time.Millisecond - 3*time.Second
		select {
		case <-time.After(25*time.Second + jitter):
		case <-c.done:
			return
		}
	}
}
