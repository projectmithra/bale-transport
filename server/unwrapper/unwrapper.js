#!/usr/bin/env node
// ============================================================
// bale-unwrapper — Server-side Bale protobuf decoder
//
// Accepts WebSocket connections on port 80 (or PORT env).
// Auto-detects Bale-wrapped vs legacy (raw) connections.
// For Bale connections: strips protobuf, forwards clean tunnel
// data to the backend proxy server (Xray/SingBox) via WebSocket.
//
// Usage:
//   BACKEND=ws://127.0.0.1:8443 node unwrapper.js
//   BACKEND=ws://127.0.0.1:8443 PORT=8080 node unwrapper.js
// ============================================================

const http = require('http');
const { WebSocket, WebSocketServer } = require('ws');

const PORT = parseInt(process.env.PORT || '80');
const BACKEND = process.env.BACKEND || 'ws://127.0.0.1:8443';
const BACKEND_PATH = process.env.BACKEND_PATH || '/api/v4/sync/data-stream';
const VERBOSE = process.env.VERBOSE === '1';

// ============================================================
// PROTOBUF CODEC (matches Go bale/ package exactly)
// ============================================================

class PbReader {
  constructor(buf) {
    this.buf = Buffer.isBuffer(buf) ? buf : Buffer.from(buf);
    this.pos = 0;
  }
  readVarint() {
    let r = 0, s = 0;
    while (this.pos < this.buf.length) {
      const b = this.buf[this.pos++];
      r |= (b & 0x7f) << s;
      if ((b & 0x80) === 0) return r >>> 0;
      s += 7;
      if (s > 35) throw new Error('Varint too long');
    }
    throw new Error('Unexpected end');
  }
  readTag() {
    const t = this.readVarint();
    return { fieldNumber: t >>> 3, wireType: t & 0x07 };
  }
  readBytes() {
    const len = this.readVarint();
    const bytes = this.buf.slice(this.pos, this.pos + len);
    this.pos += len;
    return bytes;
  }
  readString() { return this.readBytes().toString('utf8'); }
  skip(wt) {
    switch (wt) {
      case 0: this.readVarint(); break;
      case 1: this.pos += 8; break;
      case 2: this.pos += this.readVarint(); break;
      case 5: this.pos += 4; break;
      default: throw new Error(`Unknown wire type: ${wt}`);
    }
  }
  hasMore() { return this.pos < this.buf.length; }
}

class PbWriter {
  constructor() { this.chunks = []; }
  writeVarint(v) {
    const buf = [];
    v = v >>> 0;
    while (v > 0x7f) { buf.push((v & 0x7f) | 0x80); v >>>= 7; }
    buf.push(v & 0x7f);
    this.chunks.push(Buffer.from(buf));
    return this;
  }
  writeTag(fn, wt) { return this.writeVarint((fn << 3) | wt); }
  writeBytes(fn, data) {
    this.writeTag(fn, 2);
    this.writeVarint(data.length);
    this.chunks.push(Buffer.isBuffer(data) ? data : Buffer.from(data));
    return this;
  }
  writeInt32(fn, v) {
    if (v !== 0) { this.writeTag(fn, 0); this.writeVarint(v); }
    return this;
  }
  finish() { return Buffer.concat(this.chunks); }
}

// Length-prefix padding (unified with Go client and Worker)
function addPadding(data) {
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  const dataLen = buf.length;
  let targetSize;
  if (dataLen < 50) targetSize = 50 + Math.floor(Math.random() * 150);
  else if (dataLen < 500) targetSize = Math.max(dataLen, 200) + Math.floor(Math.random() * 300);
  else if (dataLen < 4096) targetSize = dataLen + Math.floor(Math.random() * 512);
  else targetSize = dataLen + Math.floor(Math.random() * 64);

  if (targetSize < dataLen) targetSize = dataLen;
  const paddingLen = targetSize - dataLen;
  const result = Buffer.alloc(2 + dataLen + paddingLen);
  result.writeUInt16BE(dataLen, 0);
  buf.copy(result, 2);
  for (let i = 2 + dataLen; i < result.length; i++) {
    result[i] = Math.floor(Math.random() * 256);
  }
  return result;
}

function stripPadding(data) {
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  if (buf.length < 2) return buf;
  const realLen = buf.readUInt16BE(0);
  if (realLen + 2 > buf.length) return buf;
  return buf.slice(2, 2 + realLen);
}

function decodeClientEnvelope(buffer) {
  const r = new PbReader(buffer);
  const result = { type: 'unknown' };
  while (r.hasMore()) {
    const { fieldNumber, wireType } = r.readTag();
    switch (fieldNumber) {
      case 1: result.request = r.readBytes(); result.type = 'request'; break;
      case 2: result.ping = r.readBytes(); result.type = 'ping'; break;
      case 3: result.handshakeRequest = r.readBytes(); result.type = 'handshake'; break;
      default: r.skip(wireType);
    }
  }
  return result;
}

function decodeRequest(buffer) {
  const r = new PbReader(buffer);
  const result = { serviceName: '', method: '', payload: null, index: 0 };
  while (r.hasMore()) {
    const { fieldNumber, wireType } = r.readTag();
    switch (fieldNumber) {
      case 1: result.serviceName = r.readString(); break;
      case 2: result.method = r.readString(); break;
      case 3: result.payload = r.readBytes(); break;
      case 4: r.skip(wireType); break;
      case 5: result.index = r.readVarint(); break;
      default: r.skip(wireType);
    }
  }
  return result;
}

function decodePing(buffer) {
  const r = new PbReader(buffer);
  let id = 0;
  while (r.hasMore()) {
    const { fieldNumber, wireType } = r.readTag();
    if (fieldNumber === 1) id = r.readVarint();
    else r.skip(wireType);
  }
  return { id };
}

function encodeHandshakeResponse() {
  const inner = new PbWriter(); inner.writeInt32(1, 1); inner.writeInt32(2, 1);
  const w = new PbWriter(); w.writeBytes(5, inner.finish());
  return w.finish();
}

function encodePong(id) {
  const inner = new PbWriter();
  if (id !== 0) { inner.writeTag(1, 0); inner.writeVarint(id); }
  const w = new PbWriter(); w.writeBytes(4, inner.finish());
  return w.finish();
}

function encodeResponseEnvelope(data, index) {
  const resp = new PbWriter();
  if (data && data.length > 0) resp.writeBytes(2, data);
  if (index) { resp.writeTag(3, 0); resp.writeVarint(index); }
  const w = new PbWriter(); w.writeBytes(1, resp.finish());
  return w.finish();
}

function encodeUpdateEnvelope(data) {
  const upd = new PbWriter();
  if (data && data.length > 0) upd.writeBytes(1, data);
  const w = new PbWriter(); w.writeBytes(2, upd.finish());
  return w.finish();
}

// ============================================================
// SERVER
// ============================================================

const server = http.createServer((req, res) => {
  // HTTP responses for active probing resistance
  res.setHeader('Server', 'nginx/1.25.3');
  res.setHeader('Content-Type', 'application/json; charset=utf-8');

  if (req.url === '/' || req.url === '') {
    res.writeHead(200);
    res.end(JSON.stringify({
      ok: true,
      result: { version: '5.4.2', apiVersion: 1, mkprotoVersion: 1, serverTime: Date.now() }
    }));
  } else {
    res.writeHead(404);
    res.end(JSON.stringify({ ok: false, error: { code: 404, message: 'Not Found' } }));
  }
});

const wss = new WebSocketServer({ server, path: BACKEND_PATH });

wss.on('connection', (clientWs, req) => {
  const label = `[${req.socket.remoteAddress}]`;
  let isBaleMode = false;
  let handshakeDone = false;
  let lastIndex = 0;
  let backendWs = null;
  let firstMessage = true;

  vlog(`${label} New WS connection`);

  // Connect to backend proxy (Xray/SingBox)
  backendWs = new WebSocket(BACKEND);

  backendWs.on('open', () => {
    vlog(`${label} Backend connected`);
  });

  backendWs.on('error', (err) => {
    console.error(`${label} Backend error: ${err.message}`);
    clientWs.close();
  });

  backendWs.on('close', () => {
    vlog(`${label} Backend closed`);
    clientWs.close();
  });

  // BACKEND → CLIENT
  backendWs.on('message', (data) => {
    try {
      if (isBaleMode) {
        const padded = addPadding(data);
        let wrapped;
        if (lastIndex > 0 && Math.random() > 0.3) {
          wrapped = encodeResponseEnvelope(padded, lastIndex);
        } else {
          wrapped = encodeUpdateEnvelope(padded);
        }
        clientWs.send(wrapped);
      } else {
        clientWs.send(data);
      }
    } catch (err) {
      console.error(`${label} Backend→client error: ${err.message}`);
    }
  });

  // CLIENT → BACKEND
  clientWs.on('message', (data) => {
    try {
      // Auto-detect Bale mode on first message
      if (firstMessage) {
        firstMessage = false;
        try {
          const env = decodeClientEnvelope(data);
          if (env.type === 'handshake') {
            isBaleMode = true;
            console.log(`${label} Bale mode detected`);
            clientWs.send(encodeHandshakeResponse());
            handshakeDone = true;
            return;
          }
        } catch (_) {
          // Not Bale protobuf — legacy mode
        }
      }

      if (isBaleMode) {
        const env = decodeClientEnvelope(data);
        switch (env.type) {
          case 'request': {
            if (!handshakeDone) break;
            const req = decodeRequest(env.request);
            lastIndex = req.index;
            if (req.payload) {
              const clean = stripPadding(req.payload);
              if (backendWs.readyState === WebSocket.OPEN) {
                backendWs.send(clean);
              }
            }
            break;
          }
          case 'ping': {
            const ping = decodePing(env.ping);
            clientWs.send(encodePong(ping.id));
            break;
          }
          case 'handshake': {
            // Late handshake — respond
            clientWs.send(encodeHandshakeResponse());
            handshakeDone = true;
            break;
          }
        }
      } else {
        // Legacy: pass through raw
        if (backendWs.readyState === WebSocket.OPEN) {
          backendWs.send(data);
        }
      }
    } catch (err) {
      console.error(`${label} Client→backend error: ${err.message}`);
    }
  });

  clientWs.on('close', () => {
    vlog(`${label} Client closed`);
    if (backendWs) backendWs.close();
  });

  clientWs.on('error', (err) => {
    console.error(`${label} Client error: ${err.message}`);
    if (backendWs) backendWs.close();
  });
});

server.listen(PORT, () => {
  console.log('');
  console.log('═══════════════════════════════════════════════════');
  console.log('  Mithra · bale-unwrapper');
  console.log('  Server-side Bale protobuf decoder');
  console.log('═══════════════════════════════════════════════════');
  console.log(`  Listen:   0.0.0.0:${PORT}`);
  console.log(`  Backend:  ${BACKEND}`);
  console.log(`  Path:     ${BACKEND_PATH}`);
  console.log('  Mode:     Auto-detect (Bale / Legacy)');
  console.log('═══════════════════════════════════════════════════');
  console.log('');
});

function vlog(...args) {
  if (VERBOSE) console.log(...args);
}
