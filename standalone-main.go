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
	"github.com/project-mithra/bale-transport/bale"
)

// ============================================================
// CONFIGURATION
// ============================================================

var (
	listenAddr = flag.String("listen", "127.0.0.1:1984", "Local TCP address to listen on")
	workerURL  = flag.String("worker", "", "Cloudflare Worker WebSocket URL (required)")
	workerHost = flag.String("host", "", "Host header for Worker (defaults to URL host)")
	origin     = flag.String("origin", "https://web.bale.ai", "Origin header")
	pingMs     = flag.Int("ping", 25000, "Keepalive ping interval in milliseconds")
	jitterMs   = flag.Int("jitter", 3000, "Ping timing jitter in milliseconds")
	verbose    = flag.Bool("v", false, "Verbose logging")
)

// ============================================================
// SESSION — mirrors Kotlin BaleWrapperService.handleSession()
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

	// Buffer for TCP data arriving before handshake completes
	// Matches Kotlin: bufferedData = mutableListOf<ByteArray>()
	bufMu        sync.Mutex
	bufferedData [][]byte
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

	// 2. Send Bale handshake (matches Kotlin onOpen callback)
	hsReq := bale.EncodeHandshakeRequest()
	if err := s.wsSend(hsReq); err != nil {
		logf("%s Handshake send failed: %v", s.label, err)
		return
	}

	// 3. Start WebSocket read loop (handles handshake response, data, pings)
	go s.wsReadLoop()

	// 4. Start TCP read loop IMMEDIATELY — buffer data until handshake completes
	//    This matches the Kotlin behavior: VLESS client sends data right away,
	//    we buffer it and flush after the Bale handshake response arrives.
	s.tcpReadLoop()
}

func (s *Session) connectWS() error {
	header := http.Header{}
	header.Set("Origin", *origin)
	header.Set("Accept-Language", "fa-IR,fa;q=0.9,en-US;q=0.8,en;q=0.7")
	header.Set("Sec-WebSocket-Protocol", "binary")
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

func (s *Session) isHandshakeDone() bool {
	select {
	case <-s.handshakeDone:
		return true
	default:
		return false
	}
}

// ============================================================
// WS READ LOOP — matches Kotlin onMessage callback
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

			// Flush buffered data — matches Kotlin:
			// synchronized(bufferedData) {
			//     for (data in bufferedData) { sendWrapped(ws, data, requestIndex) }
			//     bufferedData.clear()
			// }
			s.bufMu.Lock()
			for _, data := range s.bufferedData {
				idx := int(atomic.AddInt32(&s.requestIndex, 1))
				wrapped := bale.WrapTunnelData(data, idx)
				if err := s.wsSend(wrapped); err != nil {
					s.bufMu.Unlock()
					logf("%s Buffer flush error: %v", s.label, err)
					return
				}
			}
			s.bufferedData = nil
			s.bufMu.Unlock()

			// Signal handshake complete
			select {
			case <-s.handshakeDone:
			default:
				close(s.handshakeDone)
			}

			// Start keepalive pings — matches Kotlin pingJob
			go s.pingLoop()

		case "response", "update":
			// Extract payload from Bale ServerEnvelope
			payload, err := bale.ExtractPayload(env)
			if err != nil {
				logf("%s Extract error: %v", s.label, err)
				continue
			}
			if payload == nil {
				vlogf("%s extractPayload returned nil", s.label)
				continue
			}

			// Server adds length-prefix padding to responses (addPadding in bale-unwrapper.js)
			// We must strip it here to get clean tunnel data
			clean := bale.StripPadding(payload)
			vlogf("%s Response: padded=%d clean=%d", s.label, len(payload), len(clean))

			if _, err := s.tcp.Write(clean); err != nil {
				if atomic.LoadInt32(&s.closed) == 0 {
					vlogf("%s TCP write error: %v", s.label, err)
				}
				return
			}

		case "pong":
			vlogf("%s Pong received", s.label)

		case "terminate":
			logf("%s Server terminated", s.label)
			return
		}
	}
}

// ============================================================
// TCP READ LOOP — matches Kotlin tcpInput.read() loop
// ============================================================

func (s *Session) tcpReadLoop() {
	buf := make([]byte, 65536) // 64KB — matches Kotlin readBuffer = ByteArray(65536)

	for atomic.LoadInt32(&s.closed) == 0 {
		n, err := s.tcp.Read(buf)
		if err != nil {
			if err != io.EOF && atomic.LoadInt32(&s.closed) == 0 {
				vlogf("%s TCP read error: %v", s.label, err)
			}
			return
		}
		if n == 0 {
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		// Matches Kotlin:
		// if (!handshakeComplete.get()) {
		//     synchronized(bufferedData) { bufferedData.add(data) }
		// } else {
		//     sendWrapped(webSocket, data, requestIndex)
		// }
		if !s.isHandshakeDone() {
			s.bufMu.Lock()
			s.bufferedData = append(s.bufferedData, data)
			s.bufMu.Unlock()
			vlogf("%s Buffered %d bytes (handshake pending)", s.label, n)
		} else {
			idx := int(atomic.AddInt32(&s.requestIndex, 1))
			wrapped := bale.WrapTunnelData(data, idx)
			if err := s.wsSend(wrapped); err != nil {
				if atomic.LoadInt32(&s.closed) == 0 {
					vlogf("%s WS send error: %v", s.label, err)
				}
				return
			}
		}
	}
}

// ============================================================
// PING LOOP — matches Kotlin pingJob
// ============================================================

func (s *Session) pingLoop() {
	baseInterval := time.Duration(*pingMs) * time.Millisecond
	jitter := time.Duration(*jitterMs) * time.Millisecond

	// First ping after 20-30s — matches Kotlin: delay(20_000L + (Math.random() * 10_000).toLong())
	firstDelay := 20*time.Second + time.Duration(rand.Int63n(int64(10*time.Second)))
	time.Sleep(firstDelay)

	for atomic.LoadInt32(&s.closed) == 0 {
		id := int(atomic.AddInt32(&s.pingID, 1))
		ping := bale.EncodePing(id)

		if err := s.wsSend(ping); err != nil {
			if atomic.LoadInt32(&s.closed) == 0 {
				vlogf("%s Ping send error: %v", s.label, err)
			}
			return
		}
		vlogf("%s Ping %d sent", s.label, id)

		// Matches Kotlin: val jitter = ((Math.random() * 2 - 1) * PING_JITTER_MS).toLong()
		jitterDelta := time.Duration(rand.Int63n(int64(2*jitter))) - jitter
		time.Sleep(baseInterval + jitterDelta)
	}
}

// ============================================================
// CLEANUP — matches Kotlin cleanup()
// ============================================================

func (s *Session) cleanup() {
	if !atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return
	}

	if s.ws != nil {
		s.ws.Close()
	}
	if s.tcp != nil {
		s.tcp.Close()
	}

	logf("%s Closed", s.label)
}

// ============================================================
// MAIN
// ============================================================

func main() {
	flag.Parse()

	if *workerURL == "" {
		fmt.Fprintln(os.Stderr, "Error: -worker URL is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: bale-transport -worker wss://your-worker.com/w [-listen 127.0.0.1:1984]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *listenAddr, err)
	}

	fmt.Println("")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("  Mithra · Bale Transport Proxy")
	fmt.Println("  Protocol Mimicry for Censorship Circumvention")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("  Listen:    %s\n", *listenAddr)
	fmt.Printf("  Worker:    %s\n", *workerURL)
	fmt.Printf("  Origin:    %s\n", *origin)
	fmt.Printf("  Ping:      %dms ±%dms\n", *pingMs, *jitterMs)
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("")
	fmt.Println("  Configure your proxy client to connect to:")
	fmt.Printf("  %s (any protocol: VLESS, Shadowsocks, etc.)\n", *listenAddr)
	fmt.Println("")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		session := NewSession(conn)
		go session.Run()
	}
}

// ============================================================
// LOGGING
// ============================================================

func logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func vlogf(format string, args ...interface{}) {
	if *verbose {
		log.Printf(format, args...)
	}
}
