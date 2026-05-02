package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/projectmithra/bale-transport/bale"
)

var (
	listenAddr = flag.String("listen", "127.0.0.1:1984", "Local TCP address to listen on")
	workerURL  = flag.String("worker", "", "Cloudflare Worker WebSocket URL (required)")
	workerHost = flag.String("host", "", "Host header for Worker (defaults to URL host)")
	origin     = flag.String("origin", "https://web.bale.ai", "Origin header")
	pingMs     = flag.Int("ping", 25000, "Keepalive ping interval in milliseconds")
	jitterMs   = flag.Int("jitter", 3000, "Ping timing jitter in milliseconds")
	verbose    = flag.Bool("v", false, "Verbose logging")
)

func logf(format string, args ...interface{}) {
	if *verbose {
		log.Printf(format, args...)
	}
}

func main() {
	flag.Parse()
	if *workerURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: bale-transport -worker wss://your-worker.example.com/w [-listen 127.0.0.1:1984] [-v]")
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Listen failed: %v", err)
	}

	fmt.Println("")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("  Mithra · Bale Transport Proxy")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("  Listen: %s\n", *listenAddr)
	fmt.Printf("  Worker: %s\n", *workerURL)
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go NewSession(conn).Run()
	}
}

// ============================================================
// SESSION
// ============================================================

type Session struct {
	tcp           net.Conn
	ws            *websocket.Conn
	wsMu          sync.Mutex
	pingID        int32
	requestIndex  int32
	handshakeDone chan struct{}
	closed        int32
	label         string
	bufMu         sync.Mutex
	bufferedData  [][]byte
}

func NewSession(tcp net.Conn) *Session {
	return &Session{
		tcp:           tcp,
		handshakeDone: make(chan struct{}),
		label:         fmt.Sprintf("[%s]", tcp.RemoteAddr()),
		bufferedData:  make([][]byte, 0),
	}
}

func (s *Session) Run() {
	defer s.cleanup()

	logf("%s New connection", s.label)

	// 1. Connect WebSocket to Cloudflare Worker
	if err := s.connectWS(); err != nil {
		logf("%s WS connect failed: %v", s.label, err)
		return
	}
	logf("%s WS connected → handshake", s.label)

	// 2. Send Bale handshake
	hsReq := bale.EncodeHandshakeRequest()
	if err := s.wsSend(hsReq); err != nil {
		logf("%s Handshake send failed: %v", s.label, err)
		return
	}

	// 3. Start WebSocket read loop (handles handshake response, data, pings)
	go s.wsReadLoop()

	// 4. Start TCP read loop — buffers data until handshake completes
	s.tcpReadLoop()
}

func (s *Session) connectWS() error {
	header := http.Header{}
	header.Set("Origin", *origin)
	header.Set("Accept-Language", "fa-IR,fa;q=0.9,en-US;q=0.8,en;q=0.7")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	header.Set("X-Bale-Proto", "1")

	if *workerHost != "" {
		header.Set("Host", *workerHost)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, _, err := dialer.Dial(*workerURL, header)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	s.ws = conn
	return nil
}

func (s *Session) wsSend(data []byte) error {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.ws == nil {
		return fmt.Errorf("ws closed")
	}
	return s.ws.WriteMessage(websocket.BinaryMessage, data)
}

func (s *Session) cleanup() {
	if !atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return
	}
	if s.tcp != nil {
		s.tcp.Close()
	}
	s.wsMu.Lock()
	if s.ws != nil {
		s.ws.Close()
	}
	s.wsMu.Unlock()
	logf("%s Connection closed", s.label)
}

// ============================================================
// WS READ LOOP
// ============================================================

func (s *Session) wsReadLoop() {
	defer s.cleanup()

	for atomic.LoadInt32(&s.closed) == 0 {
		_, message, err := s.ws.ReadMessage()
		if err != nil {
			if atomic.LoadInt32(&s.closed) == 0 {
				logf("%s WS read error: %v", s.label, err)
			}
			return
		}

		env, err := bale.DecodeServerEnvelope(message)
		if err != nil {
			logf("%s Decode error: %v", s.label, err)
			continue
		}

		switch env.Type {
		case "handshake":
			logf("%s Handshake OK", s.label)

			// Flush buffered data
			s.bufMu.Lock()
			for _, data := range s.bufferedData {
				idx := int(atomic.AddInt32(&s.requestIndex, 1))
				wrapped := bale.WrapTunnelData(data, idx)
				if err := s.wsSend(wrapped); err != nil {
					s.bufMu.Unlock()
					return
				}
			}
			s.bufferedData = nil
			s.bufMu.Unlock()

			// Signal handshake complete
			close(s.handshakeDone)

			// Start keepalive pings
			go s.keepaliveLoop()

		case "response":
			payload, err := bale.ExtractPayload(env)
			if err != nil || payload == nil {
				continue
			}
			clean := bale.StripPadding(payload)
			if len(clean) == 0 {
				continue
			}
			logf("%s Response: padded=%d clean=%d", s.label, len(payload), len(clean))
			if _, err := s.tcp.Write(clean); err != nil {
				return
			}

		case "update":
			payload, err := bale.ExtractPayload(env)
			if err != nil || payload == nil {
				continue
			}
			clean := bale.StripPadding(payload)
			if len(clean) == 0 {
				continue
			}
			logf("%s Update: padded=%d clean=%d", s.label, len(payload), len(clean))
			if _, err := s.tcp.Write(clean); err != nil {
				return
			}

		case "pong":
			logf("%s Pong received", s.label)

		case "terminate":
			logf("%s Terminate received", s.label)
			return
		}
	}
}

// ============================================================
// TCP READ LOOP
// ============================================================

func (s *Session) tcpReadLoop() {
	buf := make([]byte, 32*1024)

	for atomic.LoadInt32(&s.closed) == 0 {
		n, err := s.tcp.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			select {
			case <-s.handshakeDone:
				// Handshake done — send directly
				idx := int(atomic.AddInt32(&s.requestIndex, 1))
				wrapped := bale.WrapTunnelData(data, idx)
				if wErr := s.wsSend(wrapped); wErr != nil {
					return
				}
			default:
				// Handshake pending — buffer
				s.bufMu.Lock()
				s.bufferedData = append(s.bufferedData, data)
				s.bufMu.Unlock()
			}
		}
		if err != nil {
			if err != io.EOF {
				logf("%s TCP read error: %v", s.label, err)
			}
			return
		}
	}
}

// ============================================================
// KEEPALIVE
// ============================================================

func (s *Session) keepaliveLoop() {
	done := make(chan struct{})
	go func() {
		<-s.handshakeDone
		for atomic.LoadInt32(&s.closed) == 0 {
			jitter := time.Duration(rand.Intn(*jitterMs*2)-*jitterMs) * time.Millisecond
			interval := time.Duration(*pingMs)*time.Millisecond + jitter

			select {
			case <-time.After(interval):
				id := int(atomic.AddInt32(&s.pingID, 1))
				ping := bale.EncodePing(id)
				if err := s.wsSend(ping); err != nil {
					return
				}
				logf("%s Ping sent (id=%d)", s.label, id)
			case <-done:
				return
			}
		}
	}()

	// Wait for session close
	for atomic.LoadInt32(&s.closed) == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	close(done)
}
