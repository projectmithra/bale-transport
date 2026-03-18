# Bale Transport

**Protocol mimicry transport layer for censorship circumvention.**

Wraps tunnel traffic inside [Bale messenger](https://bale.ai)'s exact protobuf wire format. Iran's DPI sees what looks like a legitimate Bale session — protobuf envelopes, real service names, proper handshake sequence, padded frame sizes, and Bale-timed keepalive pings.

Part of [Project Mithra](https://github.com/projectmithra) — infrastructure mimicry for the circumvention ecosystem.

## How It Works

Two independent evasion layers:

1. **Open IP Lane** (routing) — Traffic routes through Cloudflare IPs shared with whitelisted Iranian financial services. DPI sees traffic going to a financial service address.

2. **Bale Protocol Mimicry** (content) — Every WebSocket frame is a valid Bale protobuf envelope. The connection begins with Bale's handshake, carries data inside `ClientEnvelope`/`ServerEnvelope` structures with real Bale gRPC service names, and maintains keepalive at Bale's exact interval.

These layers are independent — the routing disguise and the content disguise solve different detection problems simultaneously.

## Architecture

```
User's Proxy Client (Hiddify / V2RayNG / Nekobox / Outline)
  │ plain TCP to localhost:1984
  ▼
Bale Transport (standalone binary or native SingBox transport)
  │ WSS with Bale protobuf to Cloudflare
  ▼
Cloudflare Worker (strips protobuf, active probing resistance)
  │ WSS to origin server
  ▼
bale-unwrapper (Node.js, auto-detects Bale vs legacy)
  │ raw tunnel data
  ▼
Xray / SingBox Server → Open Internet
```

## Quick Start

### Option 1: Standalone Binary (works with any proxy client)

```bash
# Build
go build -o bale-transport ./cmd/bale-transport/

# Run
./bale-transport -worker wss://your-worker.com/w -v

# Point your proxy client at 127.0.0.1:1984
```

### Option 2: Native SingBox Transport

Use `"type": "bale"` in your SingBox config's transport section. See [examples/singbox-native-bale.json](examples/singbox-native-bale.json).

Requires a SingBox build with the Bale transport compiled in. See [docs/SINGBOX-INTEGRATION.md](docs/SINGBOX-INTEGRATION.md).

### Server Setup (Docker)

```bash
cd server/docker

# Edit xray-config.json with your UUID
# Edit docker-compose.yml if needed

docker compose up -d
```

This starts the `bale-unwrapper` on port 80 and Xray on port 8443. Point your Cloudflare Worker at port 80.

## Repository Structure

```
bale-transport/
├── bale/                          # Core protobuf codec (Go package)
│   ├── proto.go                   # Encoder/decoder + padding
│   ├── proto_test.go              # Comprehensive tests
│   └── errors.go                  # Error definitions
├── cmd/
│   └── bale-transport/
│       └── main.go                # Standalone binary
├── transport/
│   └── v2raybale/                 # Native SingBox transport
│       ├── client.go              # SingBox adapter.V2RayClientTransport
│       └── conn.go                # net.Conn over Bale-wrapped WebSocket
├── worker/
│   └── worker.js                  # Cloudflare Worker
├── server/
│   ├── unwrapper/
│   │   ├── unwrapper.js           # Node.js bale-unwrapper
│   │   └── package.json
│   └── docker/
│       ├── Dockerfile
│       ├── docker-compose.yml
│       └── xray-config.json
├── examples/                      # Ready-to-use configs
├── scripts/                       # Build & deployment scripts
├── docs/                          # Integration guides
└── README.md
```

## Integration Paths

| Tool | Integration | Effort |
|------|------------|--------|
| Hiddify / SingBox | Native transport: `"type": "bale"` | PR merge + rebuild |
| V2RayNG / MasaNG | Standalone binary + VLESS TCP config | Binary + config |
| Self-hosted V2Ray | Standalone binary + server unwrapper | Tutorial + binary |
| Outline | Standalone binary as middleware | Config + guide |

## What DPI Sees

| Layer | What DPI Sees | Bale Match |
|-------|--------------|------------|
| IP destination | Cloudflare IP shared with Iranian financial services | ✓ |
| TLS handshake | Chrome browser fingerprint via uTLS | ✓ |
| WebSocket path | `/w` | ✓ |
| Origin header | `web.bale.ai` | ✓ |
| Accept-Language | `fa-IR` | ✓ |
| First WS message | Protobuf HandshakeRequest | ✓ |
| Data frames | ClientEnvelope with Bale service names | ✓ |
| Frame sizes | Padded to Bale distribution | ✓ |
| Keepalive | Ping/Pong every ~25s ±3s | ✓ |

## Testing

```bash
# Run unit tests
go test ./bale/ -v

# Run with benchmarks
go test ./bale/ -v -bench=.
```

## Building

```bash
# Single platform
go build -o bale-transport ./cmd/bale-transport/

# All platforms
bash scripts/build-all.sh
```

## Security

The Bale protobuf codec was reconstructed from Bale's production JavaScript client (`beta.bale.ai/static/js/index.f68af2e0.js`). The padding scheme uses deterministic length-prefix encoding (`uint16_be` + data + random) — not marker-byte scanning — to prevent data corruption from payload bytes colliding with the padding delimiter.

## License

MIT — see [LICENSE](LICENSE).
