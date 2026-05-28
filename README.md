# Bale Transport

**Protocol mimicry transport layer for censorship circumvention.**

Wraps tunnel traffic inside [Bale messenger](https://bale.ai)'s exact protobuf wire format. DPI sees what looks like a legitimate Bale session — protobuf envelopes, real service names, proper handshake sequence, padded frame sizes, and Bale-timed keepalive pings.

Part of [Project Mithra](https://github.com/projectmithra) — infrastructure mimicry for the circumvention ecosystem.


## How It Works

Two independent evasion layers:

1. **Open IP Lane** (routing) — Traffic routes through Cloudflare IPs shared with whitelisted Iranian financial services. DPI sees traffic going to a financial service address. See [`open-ip-lane`](https://github.com/projectmithra/open-ip-lane) for the scanning methodology that discovers these IPs.

2. **Bale Protocol Mimicry** (content) — Every WebSocket frame is a valid Bale protobuf envelope. The connection begins with Bale's handshake, carries data inside `ClientEnvelope`/`ServerEnvelope` structures with real Bale gRPC service names, and maintains keepalive at Bale's exact interval.

These layers are independent — the routing disguise and the content disguise solve different detection problems simultaneously.

## Architecture

```
User's Proxy Client (Hiddify / V2RayNG / Nekobox / Outline)
  │ plain TCP
  ▼
Bale Transport (standalone binary or native SingBox transport)
  │ WSS with Bale protobuf to CDN edge
  ▼
CDN Edge Relay (see cloudflare-worker repo)
  │ WSS to origin server
  ▼
bale-unwrapper (Node.js, server-side)
  │ raw tunnel data
  ▼
Xray / SingBox Server → Open Internet
```

## Quick Start

### Standalone Binary

The standalone binary accepts local TCP connections and wraps them in Bale protobuf over WebSocket. Works with any proxy client (V2Box, V2RayNG, Hiddify, Outline) without recompiling SingBox.

```bash
# Build
go build -o bale-transport ./cmd/bale-transport/

# Run
./bale-transport -worker wss://your-worker.example.com/w -v

# Configure your proxy client to connect to 127.0.0.1:1984
```

See [examples/singbox-with-standalone.json](examples/singbox-with-standalone.json) for a sample config.

### Native SingBox Transport

Use `"type": "bale"` in your SingBox config's transport section. See [examples/singbox-native-bale.json](examples/singbox-native-bale.json).

Requires a SingBox build with the Bale transport compiled in via the patches in [`patches/`](patches/). See [docs/SINGBOX-INTEGRATION.md](docs/SINGBOX-INTEGRATION.md) for build instructions.

### Server Setup (Docker)

```bash
cd server/docker

# Edit xray-config.json with your UUID
# Edit docker-compose.yml if needed

docker compose up -d
```

This starts the `bale-unwrapper` on port 80 and Xray on port 8443. Point your CDN edge relay at port 80.

For the CDN edge relay, see the [`cloudflare-worker`](https://github.com/projectmithra/cloudflare-worker) repo.

## Repository Structure

```
bale-transport/
├── cmd/
│   └── bale-transport/
│       └── main.go                # Standalone TCP→Bale proxy binary
├── bale/                          # Core protobuf codec (Go package)
│   ├── proto.go                   # Encoder/decoder + padding
│   ├── proto_test.go              # Comprehensive tests
│   └── errors.go                  # Error definitions
├── transport/
│   └── v2raybale/
│       └── client.go              # SingBox adapter.V2RayClientTransport
├── patches/                       # Patches for SingBox integration
│   ├── patched-transport.go
│   ├── patched-v2ray-constant.go
│   └── v2ray_bale.go
├── server/
│   ├── unwrapper/
│   │   ├── unwrapper.js           # Node.js bale-unwrapper
│   │   └── package.json
│   └── docker/
│       ├── Dockerfile
│       ├── docker-compose.yml
│       └── xray-config.json
├── examples/                      # Ready-to-use configs
│   ├── singbox-native-bale.json
│   └── singbox-with-standalone.json
├── scripts/                       # Build scripts
│   └── build-all.sh
├── docs/                          # Integration guides
│   └── SINGBOX-INTEGRATION.md
├── go.mod
├── LICENSE
└── README.md
```

## Integration Paths

| Tool | Integration | Effort |
|------|------------|--------|
| Hiddify / SingBox | Native transport: `"type": "bale"` | PR merge + rebuild |
| V2RayNG / V2Box / Outline | Standalone binary + proxy config | Binary + config |
| Self-hosted | SingBox patches + server unwrapper | Patches + server |

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
# Build all components
bash scripts/build-all.sh

# Or build standalone binary only
go build -o bale-transport ./cmd/bale-transport/
```

## Security

The Bale protobuf codec was reconstructed from Bale's production JavaScript client. The padding scheme uses deterministic length-prefix encoding (`uint32_be` + data + crypto-random bytes) — not marker-byte scanning — to prevent data corruption from payload bytes colliding with a padding delimiter. The 4-byte length prefix supports payloads up to 4 GiB (caller-enforced cap is 4 MiB via `bale.MaxPayloadSize`). Padding content is drawn from `crypto/rand` so it resists statistical analysis on the padding bytes themselves.

## License

MIT — see [LICENSE](LICENSE).

## Related Repositories

- [`open-ip-lane`](https://github.com/projectmithra/open-ip-lane) — Scanning methodology for discovering shared CDN IPs
- [`cloudflare-worker`](https://github.com/projectmithra/cloudflare-worker) — Cloudflare Worker relay with Bale API camouflage
- [`hiddify-sing-box-bale`](https://github.com/projectmithra/hiddify-sing-box-bale) — Native SingBox transport integration
