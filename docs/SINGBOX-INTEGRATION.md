# Native SingBox Bale Transport Integration

This document describes how to add the Bale transport as a native transport type in SingBox (specifically the [hiddify-sing-box](https://github.com/hiddify/hiddify-sing-box) fork).

## Overview

The integration requires changes to **4 files** in the SingBox codebase plus the addition of the `transport/v2raybale/` package:

1. `constant/v2ray.go` — Add transport type constant
2. `option/v2ray_transport.go` — Add options struct reference
3. `option/v2ray_bale.go` — New file: options struct definition
4. `transport/v2ray/transport.go` — Register the transport in the factory
5. `transport/v2raybale/client.go` — New file: the transport implementation

## Config Format

Once integrated, any SingBox config can use the Bale transport:

```json
{
  "outbounds": [
    {
      "type": "vless",
      "server": "your-server-address",
      "server_port": 443,
      "uuid": "your-uuid",
      "tls": {
        "enabled": true,
        "server_name": "your-server-name",
        "utls": { "enabled": true, "fingerprint": "chrome" }
      },
      "transport": {
        "type": "bale",
        "worker_url": "wss://your-server-address/w",
        "origin": "https://web.bale.ai",
        "path": "/w"
      }
    }
  ]
}
```

## Transport Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `worker_url` | string | (from server) | Full WSS URL to the Cloudflare Worker |
| `worker_host` | string | (from URL) | Host header override |
| `origin` | string | `https://web.bale.ai` | Origin header (Bale's) |
| `accept_language` | string | `fa-IR,fa;q=0.9,...` | Accept-Language header |
| `path` | string | `/w` | WebSocket upgrade path |
| `headers` | map | - | Additional custom headers |

## Building

### Prerequisites

- Go 1.21+
- Git

### Steps

```bash
# Clone hiddify-sing-box
git clone --depth 1 --branch extended https://github.com/hiddify/hiddify-sing-box.git
cd hiddify-sing-box

# Apply patches (see below) or use the build script
bash ~/build-bale-native.sh

# Or manually: copy transport/v2raybale/ and apply the 4 patches
# Then build:
go build -ldflags="-s -w" -tags "with_gvisor,with_quic,with_utls,with_ech" -o sing-box-bale ./cmd/sing-box
```

### Cross-compile for Android

```bash
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o sing-box-bale-android ./cmd/sing-box
```

## Patch Details

### 1. constant/v2ray.go

Add after the last transport type constant:

```go
V2RayTransportTypeBale = "bale"
```

### 2. option/v2ray_bale.go (new file)

```go
package option

import "github.com/sagernet/sing/common/json/badoption"

type V2RayBaleOptions struct {
    WorkerURL      string               `json:"worker_url"`
    WorkerHost     string               `json:"worker_host,omitempty"`
    Origin         string               `json:"origin,omitempty"`
    AcceptLanguage string               `json:"accept_language,omitempty"`
    Path           string               `json:"path,omitempty"`
    Headers        badoption.HTTPHeader `json:"headers,omitempty"`
}
```

### 3. option/v2ray_transport.go

Add `BaleOptions V2RayBaleOptions` field to `_V2RayTransportOptions` struct, and add cases for `C.V2RayTransportTypeBale` in both `MarshalJSON` and `UnmarshalJSON`.

### 4. transport/v2ray/transport.go

Add import for `v2raybale` package, then add:

- Server case: `return nil, E.New("bale transport is client-only")`
- Client case: `return v2raybale.NewClient(ctx, dialer, serverAddr, options.BaleOptions, tlsConfig)`

## How It Works

The transport implements `adapter.V2RayClientTransport`. When SingBox creates an outbound connection:

1. `DialContext()` is called
2. The transport dials TCP to the server, upgrades to WebSocket using `sagernet/ws`
3. Sends the Bale protobuf handshake, waits for response
4. Returns a `BaleConn` (implements `net.Conn`) that:
   - On `Write()`: wraps data in Bale ClientEnvelope protobuf
   - On `Read()`: unwraps ServerEnvelope protobuf, strips padding
   - Runs a background goroutine for keepalive pings (~25s ±3s)

SingBox sees a normal `net.Conn`. The Bale wrapping is completely transparent to the proxy protocol layer above it.
