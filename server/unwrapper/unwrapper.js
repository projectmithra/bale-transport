#!/usr/bin/env node
// 
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
// 

'use strict';

const http = require('http');
const crypto = require('crypto');
const { WebSocket, WebSocketServer } = require('ws');

const PORT = parseInt(process.env.PORT || '80', 10);
const BACKEND = process.env.BACKEND || 'ws://127.0.0.1:8443';
const BACKEND_PATH = process.env.BACKEND_PATH || '/api/v4/sync/data-stream';
const VERBOSE = process.env.VERBOSE === '1';

// Resource limits — tune via env if needed.
const IDLE_TIMEOUT_MS = parseInt(process.env.IDLE_TIMEOUT_MS || '120000', 10); // 120s
const MAX_BUFFERED_BYTES = parseInt(process.env.MAX_BUFFERED_BYTES || '4194304', 10); // 4 MiB
const MAX_PAYLOAD_SIZE = parseInt(process.env.MAX_PAYLOAD_SIZE || '4194304', 10); // 4 MiB
const MAX_FRAME_SIZE = parseInt(process.env.MAX_FRAME_SIZE || '16777216', 10); // 16 MiB

const PADDING_PREFIX = 4; // 4-byte uint32 big-endian length header

// PROTOBUF CODEC (matches Go bale/ package exactly) 

class PbReader {
  constructor(buf) {
    this.buf = Buffer.isBuffer(buf) ? buf : Buffer.from(buf);
    this.pos = 0;
  }

  // readVarint uses Number arithmetic (multiplication) instead of bitwise
  // left-shift so values with shift >= 25 are not silently truncated by
  // JavaScript's 32-bit bitwise semantics. Varints up to 2^53-1 decode
  // correctly; longer encodings throw.
  readVarint() {
    let result = 0;
    let multiplier = 1;
    let bytes = 0;
    while (this.pos < this.buf.length) {
      const b = this.buf[this.pos++];
      result += (b & 0x7f) * multiplier;
      bytes++;
      if ((b & 0x80) === 0) {
        if (!Number.isSafeInteger(result)) {
          throw new Error('Varint exceeds safe integer range');
        }
        return result;
      }
      if (bytes >= 5) {
        // 5 bytes is sufficient for any uint32 field we use; beyond this
        // is malformed for our subset of the wire format.
        throw new Error('Varint too long');
      }
      multiplier *= 128;
    }
    throw new Error('Unexpected end of varint');
  }

  readTag() {
    const t = this.readVarint();
    return { fieldNumber: t >>> 3, wireType: t & 0x07 };
  }

  readBytes() {
    const len = this.readVarint();
    if (len < 0 || this.pos + len > this.buf.length) {
      throw new Error('readBytes length out of range');
    }
    const bytes = this.buf.slice(this.pos, this.pos + len);
    this.pos += len;
    return bytes;
  }

  readString() {
    return this.readBytes().toString('utf8');
  }

  skip(wt) {
    switch (wt) {
      case 0: this.readVarint(); break;
      case 1: this.pos += 8; break;
      case 2: {
        const len = this.readVarint();
        if (this.pos + len > this.buf.length) throw new Error('skip length-delimited out of range');
        this.pos += len;
        break;
      }
      case 5: this.pos += 4; break;
      default: throw new Error(`Unknown wire type: ${wt}`);
    }
  }

  hasMore() {
    return this.pos < this.buf.length;
  }
}

class PbWriter {
  constructor() { this.chunks = []; }

  writeVarint(v) {
    if (v < 0 || !Number.isSafeInteger(v)) {
      throw new Error('writeVarint: value out of range');
    }
    const buf = [];
    while (v > 0x7f) {
      buf.push((v & 0x7f) | 0x80);
      v = Math.floor(v / 128);
    }
    buf.push(v & 0x7f);
    this.chunks.push(Buffer.from(buf));
    return this;
  }

  writeTag(fn, wt) {
    return this.writeVarint((fn << 3) | wt);
  }

  writeBytes(fn, data) {
    this.writeTag(fn, 2);
    this.writeVarint(data.length);
    this.chunks.push(Buffer.isBuffer(data) ? data : Buffer.from(data));
    return this;
  }

  writeInt32(fn, v) {
    if (v !== 0) {
      this.writeTag(fn, 0);
      this.writeVarint(v);
    }
    return this;
  }

  finish() {
    return Buffer.concat(this.chunks);
  }
}

// PADDING — 4-byte length prefix, crypto-random padding bytes
// (unified with Go client and Cloudflare Worker) 

function addPadding(data, prefixSize) {
  if (!prefixSize) prefixSize = PADDING_PREFIX;
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  const dataLen = buf.length;
  if (dataLen > MAX_PAYLOAD_SIZE) {
    throw new Error(`payload exceeds MAX_PAYLOAD_SIZE (${dataLen} > ${MAX_PAYLOAD_SIZE})`);
  }

  let targetSize;
  if (dataLen < 50) targetSize = 50 + Math.floor(Math.random() * 150);
  else if (dataLen < 500) targetSize = Math.max(dataLen, 200) + Math.floor(Math.random() * 300);
  else if (dataLen < 4096) targetSize = dataLen + Math.floor(Math.random() * 512);
  else targetSize = dataLen + Math.floor(Math.random() * 64);
  if (targetSize < dataLen) targetSize = dataLen;

  const paddingLen = targetSize - dataLen;
  const result = Buffer.alloc(prefixSize + dataLen + paddingLen);
  if (prefixSize === 4) {
    result.writeUInt32BE(dataLen, 0);
  } else {
    result.writeUInt16BE(dataLen, 0);
  }
  buf.copy(result, prefixSize);
  if (paddingLen > 0) {
    crypto.randomFillSync(result, prefixSize + dataLen, paddingLen);
  }
  return result;
}

function detectPaddingSize(data) {
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  if (buf.length < 4) return 2;
  // Try 4-byte decode
  const len4 = buf.readUInt32BE(0);
  if (len4 > 0 && len4 + 4 <= buf.length) {
    const firstByte = buf[4];
    // Valid VLESS/TLS first bytes after 4-byte strip
    if (firstByte === 0x00 || (firstByte >= 0x14 && firstByte <= 0x17)) return 4;
  }
  // Try 2-byte decode
  const len2 = buf.readUInt16BE(0);
  if (len2 > 0 && len2 + 2 <= buf.length) {
    const firstByte = buf[2];
    if (firstByte === 0x00 || (firstByte >= 0x14 && firstByte <= 0x17)) return 2;
  }
  // Fallback: if 2-byte gives valid length, use it
  if (len2 > 0 && len2 + 2 <= buf.length) return 2;
  if (len4 > 0 && len4 + 4 <= buf.length) return 4;
  return 2;
}

function stripPadding(data, prefixSize) {
  const buf = Buffer.isBuffer(data) ? data : Buffer.from(data);
  if (buf.length < prefixSize) return buf;
  let realLen;
  if (prefixSize === 4) {
    realLen = buf.readUInt32BE(0);
  } else {
    realLen = buf.readUInt16BE(0);
  }
  if (realLen < 0 || realLen + prefixSize > buf.length) return buf;
  return buf.slice(prefixSize, prefixSize + realLen);
}
 
// ENVELOPE DECODE / ENCODE 

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
  const inner = new PbWriter();
  inner.writeInt32(1, 1);
  inner.writeInt32(2, 1);
  const w = new PbWriter();
  w.writeBytes(5, inner.finish());
  return w.finish();
}

function encodePong(id) {
  const inner = new PbWriter();
  if (id !== 0) {
    inner.writeTag(1, 0);
    inner.writeVarint(id);
  }
  const w = new PbWriter();
  w.writeBytes(4, inner.finish());
  return w.finish();
}

function encodeResponseEnvelope(data, index) {
  const resp = new PbWriter();
  if (data && data.length > 0) resp.writeBytes(2, data);
  if (index) {
    resp.writeTag(3, 0);
    resp.writeVarint(index);
  }
  const w = new PbWriter();
  w.writeBytes(1, resp.finish());
  return w.finish();
}

function encodeUpdateEnvelope(data) {
  const upd = new PbWriter();
  if (data && data.length > 0) upd.writeBytes(1, data);
  const w = new PbWriter();
  w.writeBytes(2, upd.finish());
  return w.finish();
}
 
// SEND HELPERS — apply backpressure and size limits 

function safeSend(socket, data, label) {
  if (!socket || socket.readyState !== WebSocket.OPEN) return false;
  if (socket.bufferedAmount > MAX_BUFFERED_BYTES) {
    console.error(`${label} backpressure: bufferedAmount=${socket.bufferedAmount} > ${MAX_BUFFERED_BYTES}, closing`);
    try { socket.close(1013, 'backpressure'); } catch (_) { /* ignore */ }
    return false;
  }
  try {
    socket.send(data);
    return true;
  } catch (err) {
    console.error(`${label} send error: ${err.message}`);
    return false;
  }
}

// SERVER 

const server = http.createServer((req, res) => {
  // Active-probing response: mimic Bale's public-facing HTTP response.
  res.setHeader('Server', 'nginx/1.25.3');
  res.setHeader('Content-Type', 'application/json; charset=utf-8');

  if (req.url === '/' || req.url === '') {
    res.writeHead(200);
    res.end(JSON.stringify({
      ok: true,
      result: { version: '5.4.2', apiVersion: 1, mkprotoVersion: 1, serverTime: Date.now() },
    }));
  } else {
    res.writeHead(404);
    res.end(JSON.stringify({ ok: false, error: { code: 404, message: 'Not Found' } }));
  }
});

const wss = new WebSocketServer({
  server,
  path: BACKEND_PATH,
  maxPayload: MAX_FRAME_SIZE,
});

wss.on('connection', (clientWs, req) => {
  const label = `[${req.socket.remoteAddress}]`;
  let isBaleMode = false;
  let handshakeDone = false;
  let lastIndex = 0;
  let firstMessage = true;
  let closed = false;
  let clientPaddingSize = 0; // 0 = not yet detected

  vlog(`${label} New WS connection`);

  // Connect to backend proxy (Xray/SingBox)
  const backendWs = new WebSocket(BACKEND, { maxPayload: MAX_FRAME_SIZE });

  // Idle-connection safety: any direction must send within IDLE_TIMEOUT_MS.
  let idleTimer = null;
  const resetIdle = () => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = setTimeout(() => {
      if (closed) return;
      vlog(`${label} idle timeout, closing`);
      closeBoth();
    }, IDLE_TIMEOUT_MS);
  };
  resetIdle();

  const closeBoth = () => {
    if (closed) return;
    closed = true;
    if (idleTimer) { clearTimeout(idleTimer); idleTimer = null; }
    try { clientWs.close(); } catch (_) { /* ignore */ }
    try { backendWs.close(); } catch (_) { /* ignore */ }
  };

  backendWs.on('open', () => {
    vlog(`${label} Backend connected`);
  });

  backendWs.on('error', (err) => {
    console.error(`${label} Backend error: ${err.message}`);
    closeBoth();
  });

  backendWs.on('close', () => {
    vlog(`${label} Backend closed`);
    closeBoth();
  });

  // BACKEND → CLIENT
  backendWs.on('message', (data) => {
    if (closed) return;
    resetIdle();
    try {
      if (isBaleMode) {
        if (data.length > MAX_PAYLOAD_SIZE) {
          console.error(`${label} backend payload > MAX_PAYLOAD_SIZE, closing`);
          closeBoth();
          return;
        }
        const padded = addPadding(data, clientPaddingSize);
        let wrapped;
        if (lastIndex > 0 && Math.random() > 0.3) {
          wrapped = encodeResponseEnvelope(padded, lastIndex);
        } else {
          wrapped = encodeUpdateEnvelope(padded);
        }
        if (!safeSend(clientWs, wrapped, `${label} backend→client`)) closeBoth();
      } else {
        if (!safeSend(clientWs, data, `${label} backend→client (legacy)`)) closeBoth();
      }
    } catch (err) {
      console.error(`${label} Backend→client error: ${err.message}`);
      closeBoth();
    }
  });

  // CLIENT → BACKEND
  clientWs.on('message', (data) => {
    if (closed) return;
    resetIdle();
    try {
      if (firstMessage) {
        firstMessage = false;
        try {
          const env = decodeClientEnvelope(data);
          if (env.type === 'handshake') {
            isBaleMode = true;
            console.log(`${label} Bale mode detected`);
            safeSend(clientWs, encodeHandshakeResponse(), `${label} handshake`);
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
            const reqMsg = decodeRequest(env.request);
            lastIndex = reqMsg.index;
            if (reqMsg.payload) {
              if (clientPaddingSize === 0) {
                clientPaddingSize = detectPaddingSize(reqMsg.payload);
                console.log(`${label} Detected padding: ${clientPaddingSize}-byte`);
              }
              const clean = stripPadding(reqMsg.payload, clientPaddingSize);
              if (clean.length > MAX_PAYLOAD_SIZE) {
                console.error(`${label} payload > MAX_PAYLOAD_SIZE after strip, closing`);
                closeBoth();
                return;
              }
              if (!safeSend(backendWs, clean, `${label} client→backend`)) closeBoth();
            }
            break;
          }
          case 'ping': {
            const ping = decodePing(env.ping);
            safeSend(clientWs, encodePong(ping.id), `${label} pong`);
            break;
          }
          case 'handshake': {
            safeSend(clientWs, encodeHandshakeResponse(), `${label} late-handshake`);
            handshakeDone = true;
            break;
          }
          default:
            // Unknown envelope type — ignore quietly, matches real Bale servers.
            break;
        }
      } else {
        // Legacy: pass through raw
        if (!safeSend(backendWs, data, `${label} client→backend (legacy)`)) closeBoth();
      }
    } catch (err) {
      console.error(`${label} Client→backend error: ${err.message}`);
      closeBoth();
    }
  });

  clientWs.on('close', () => {
    vlog(`${label} Client closed`);
    closeBoth();
  });

  clientWs.on('error', (err) => {
    console.error(`${label} Client error: ${err.message}`);
    closeBoth();
  });
});

server.listen(PORT, () => {
  console.log('');
  console.log('═══════════════════════════════════════════════════');
  console.log('  Mithra · bale-unwrapper');
  console.log('  Server-side Bale protobuf decoder');
  console.log('═══════════════════════════════════════════════════');
  console.log(`  Listen:        0.0.0.0:${PORT}`);
  console.log(`  Backend:       ${BACKEND}`);
  console.log(`  Path:          ${BACKEND_PATH}`);
  console.log(`  Idle timeout:  ${IDLE_TIMEOUT_MS} ms`);
  console.log(`  Max payload:   ${MAX_PAYLOAD_SIZE} bytes`);
  console.log('  Mode:          Auto-detect (Bale / Legacy)');
  console.log('═══════════════════════════════════════════════════');
  console.log('');
});

// Graceful shutdown for container orchestrators (Docker, k8s).
function shutdown(signal) {
  console.log(`Received ${signal}, shutting down`);
  wss.close();
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 5000).unref();
}
process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT', () => shutdown('SIGINT'));

function vlog(...args) {
  if (VERBOSE) console.log(...args);
}
