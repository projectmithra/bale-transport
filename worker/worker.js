// ============================================================
// Mithra Cloudflare Worker — Bale Protocol Bridge
//
// This Worker sits between the client and the origin server.
// Two modes:
//   1. Bale mode (X-Bale-Proto: 1, path /w):
//      Unwraps Bale protobuf from client, forwards raw VLESS to backend.
//      Wraps backend responses in Bale protobuf for client.
//   2. Legacy mode (any other request):
//      Passes WebSocket frames through unmodified.
//
// Non-WebSocket requests get Bale-like JSON responses for active
// probing resistance.
// ============================================================

// >>> CONFIGURATION — change this to your origin server <<<
const TARGET_DOMAIN = '103.241.67.19.traefik.me';
const BACKEND_PATH = '/api/v4/sync/data-stream';

export default {
  async fetch(request, env) {
    const upgradeHeader = request.headers.get('Upgrade');
    const url = new URL(request.url);

    // ─── CAMOUFLAGE: Bale-like responses for non-WS requests ───
    if (!upgradeHeader || upgradeHeader !== 'websocket') {
      return handleHTTP(url);
    }

    // ─── SECURITY: Validate path ───
    const allowedPaths = ['/api/v4/sync/data-stream', '/w'];
    if (!allowedPaths.includes(url.pathname)) {
      return jsonResponse(404, { ok: false, error: { code: 404, message: 'Not Found' } });
    }

    // ─── DETECT MODE ───
    const baleProto = request.headers.get('X-Bale-Proto');
    if (baleProto === '1' && url.pathname === '/w') {
      return handleBaleProxy(request, url);
    } else {
      return handleLegacyProxy(request, url);
    }
  }
};

// ============================================================
// HTTP CAMOUFLAGE — Active probing resistance
// ============================================================

function handleHTTP(url) {
  const fakeRequestId = () => {
    const hex = () => Math.floor(Math.random() * 0xFFFFFFFF).toString(16).padStart(8, '0');
    return `${hex()}${hex()}${hex()}${hex()}`;
  };

  const baleHeaders = (extra = {}) => ({
    'Server': 'nginx/1.25.3',
    'X-Request-Id': fakeRequestId(),
    'X-Content-Type-Options': 'nosniff',
    'X-Frame-Options': 'SAMEORIGIN',
    'Strict-Transport-Security': 'max-age=31536000; includeSubDomains',
    'Cache-Control': 'no-cache, no-store, must-revalidate',
    'Pragma': 'no-cache',
    ...extra,
  });

  if (url.pathname === '/' || url.pathname === '') {
    return new Response(JSON.stringify({
      ok: true,
      result: { version: '5.4.2', apiVersion: 1, mkprotoVersion: 1, serverTime: Date.now() },
    }), {
      status: 200,
      headers: baleHeaders({ 'Content-Type': 'application/json; charset=utf-8' }),
    });
  }

  if (['/api/v4/sync/data-stream', '/w'].includes(url.pathname)) {
    return new Response(JSON.stringify({
      ok: false,
      error: { code: 426, message: 'Upgrade Required', description: 'WebSocket connection expected' },
    }), {
      status: 426,
      headers: baleHeaders({ 'Content-Type': 'application/json; charset=utf-8', 'Upgrade': 'websocket', 'Connection': 'Upgrade' }),
    });
  }

  if (url.pathname === '/favicon.ico') {
    return new Response(null, { status: 204, headers: baleHeaders() });
  }
  if (url.pathname === '/robots.txt') {
    return new Response('User-agent: *\nDisallow: /\n', {
      status: 200, headers: baleHeaders({ 'Content-Type': 'text/plain' }),
    });
  }

  return jsonResponse(404, { ok: false, error: { code: 404, message: 'Not Found' } });
}

function jsonResponse(status, body) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8', 'Server': 'nginx/1.25.3' },
  });
}

// ============================================================
// LEGACY PASSTHROUGH (V2RayNG direct / bale-unwrapper on origin)
// ============================================================

async function handleLegacyProxy(request, url) {
  const backendURL = `http://${TARGET_DOMAIN}${BACKEND_PATH}${url.search}`;
  const newRequest = new Request(backendURL, {
    method: request.method,
    headers: request.headers,
    redirect: 'follow'
  });
  newRequest.headers.set('Host', TARGET_DOMAIN);

  try {
    return await fetch(newRequest);
  } catch (e) {
    return jsonResponse(502, { ok: false, error: { code: 502, message: 'Bad Gateway' } });
  }
}

// ============================================================
// BALE PROTOCOL BRIDGE
// ============================================================

async function handleBaleProxy(request, url) {
  const [clientWs, serverWs] = Object.values(new WebSocketPair());
  serverWs.accept();

  // Connect to backend
  const backendURL = `http://${TARGET_DOMAIN}${BACKEND_PATH}${url.search}`;
  const backendRequest = new Request(backendURL, {
    headers: { 'Upgrade': 'websocket', 'Connection': 'Upgrade', 'Host': TARGET_DOMAIN },
  });

  let backendResponse;
  try {
    backendResponse = await fetch(backendRequest);
  } catch (e) {
    serverWs.close(1011, 'Backend unreachable');
    return new Response(null, { status: 101, webSocket: clientWs });
  }

  const backendWs = backendResponse.webSocket;
  if (!backendWs) {
    serverWs.close(1011, 'Backend did not upgrade');
    return new Response(null, { status: 101, webSocket: clientWs });
  }
  backendWs.accept();

  let handshakeDone = false;
  let lastIndex = 0;

  // CLIENT → WORKER: Bale protobuf → unwrap → raw to backend
  serverWs.addEventListener('message', (event) => {
    try {
      const data = toUint8Array(event.data);
      const envelope = decodeClientEnvelope(data);

      switch (envelope.type) {
        case 'handshake': {
          serverWs.send(encodeHandshakeResponse());
          handshakeDone = true;
          break;
        }
        case 'request': {
          if (!handshakeDone) break;
          const req = decodeRequest(envelope.request);
          lastIndex = req.index;
          if (req.payload) {
            const clean = stripPadding(req.payload);
            backendWs.send(clean);
          }
          break;
        }
        case 'ping': {
          const ping = decodePing(envelope.ping);
          serverWs.send(encodePong(ping.id));
          break;
        }
      }
    } catch (err) {
      console.error('Client frame error:', err.message);
    }
  });

  // BACKEND → WORKER: raw → wrap in Bale protobuf → client
  backendWs.addEventListener('message', (event) => {
    try {
      const data = toUint8Array(event.data);
      const padded = addPadding(data);
      let wrapped;
      if (lastIndex > 0 && Math.random() > 0.3) {
        wrapped = encodeResponseEnvelope(padded, lastIndex);
      } else {
        wrapped = encodeUpdateEnvelope(padded);
      }
      serverWs.send(wrapped);
    } catch (err) {
      console.error('Backend frame error:', err.message);
    }
  });

  serverWs.addEventListener('close', () => { try { backendWs.close(); } catch (_) {} });
  backendWs.addEventListener('close', () => { try { serverWs.close(1000, 'Backend closed'); } catch (_) {} });
  serverWs.addEventListener('error', () => { try { backendWs.close(); } catch (_) {} });
  backendWs.addEventListener('error', () => { try { serverWs.close(1011, 'Backend error'); } catch (_) {} });

  return new Response(null, { status: 101, webSocket: clientWs });
}

function toUint8Array(data) {
  if (data instanceof ArrayBuffer) return new Uint8Array(data);
  if (data instanceof Uint8Array) return data;
  return new TextEncoder().encode(data);
}

// ============================================================
// INLINE PROTOBUF CODEC
// ============================================================

class PbWriter {
  constructor() { this.chunks = []; this.length = 0; }
  writeVarint(v) {
    const buf = []; v = v >>> 0;
    while (v > 0x7f) { buf.push((v & 0x7f) | 0x80); v >>>= 7; }
    buf.push(v & 0x7f);
    const bytes = new Uint8Array(buf);
    this.chunks.push(bytes); this.length += bytes.length;
    return this;
  }
  writeTag(fn, wt) { return this.writeVarint((fn << 3) | wt); }
  writeBytes(fn, data) {
    if (typeof data === 'string') data = new TextEncoder().encode(data);
    if (!(data instanceof Uint8Array)) return this;
    this.writeTag(fn, 2); this.writeVarint(data.length);
    this.chunks.push(data); this.length += data.length;
    return this;
  }
  writeInt32(fn, v) { if (v !== 0) { this.writeTag(fn, 0); this.writeVarint(v); } return this; }
  finish() {
    const result = new Uint8Array(this.length);
    let offset = 0;
    for (const c of this.chunks) { result.set(c, offset); offset += c.length; }
    return result;
  }
}

class PbReader {
  constructor(buf) { this.buf = buf instanceof ArrayBuffer ? new Uint8Array(buf) : buf; this.pos = 0; this.len = this.buf.length; }
  readVarint() {
    let r = 0, s = 0;
    while (this.pos < this.len) { const b = this.buf[this.pos++]; r |= (b & 0x7f) << s; if ((b & 0x80) === 0) return r >>> 0; s += 7; if (s > 35) throw new Error('Varint too long'); }
    throw new Error('Unexpected end');
  }
  readTag() { const t = this.readVarint(); return { fieldNumber: t >>> 3, wireType: t & 0x07 }; }
  readBytes() { const len = this.readVarint(); const bytes = this.buf.slice(this.pos, this.pos + len); this.pos += len; return bytes; }
  readString() { return new TextDecoder().decode(this.readBytes()); }
  skip(wt) {
    switch (wt) {
      case 0: this.readVarint(); break;
      case 1: this.pos += 8; break;
      case 2: { const l = this.readVarint(); this.pos += l; break; }
      case 5: this.pos += 4; break;
      default: throw new Error(`Unknown wire type: ${wt}`);
    }
  }
  hasMore() { return this.pos < this.len; }
}

// ============================================================
// PADDING — Length-prefix scheme (unified with Go client)
//
// Format: [uint16_be(real_length)] [real_data] [random_padding]
// ============================================================

function addPadding(data) {
  const dataLen = data.length;
  let targetSize;
  if (dataLen < 50) targetSize = 50 + Math.floor(Math.random() * 150);
  else if (dataLen < 500) targetSize = Math.max(dataLen, 200) + Math.floor(Math.random() * 300);
  else if (dataLen < 4096) targetSize = dataLen + Math.floor(Math.random() * 512);
  else targetSize = dataLen + Math.floor(Math.random() * 64);

  if (targetSize < dataLen) targetSize = dataLen;
  const paddingLen = targetSize - dataLen;

  const result = new Uint8Array(2 + dataLen + paddingLen);
  // Write uint16 big-endian length prefix
  result[0] = (dataLen >> 8) & 0xFF;
  result[1] = dataLen & 0xFF;
  result.set(data, 2);
  // Fill padding with random bytes
  for (let i = 2 + dataLen; i < result.length; i++) {
    result[i] = Math.floor(Math.random() * 256);
  }
  return result;
}

function stripPadding(data) {
  if (data.length < 2) return data;
  const realLen = (data[0] << 8) | data[1];
  if (realLen + 2 > data.length) return data; // not length-prefixed
  return data.slice(2, 2 + realLen);
}

// ============================================================
// PROTOBUF MESSAGE FUNCTIONS
// ============================================================

function decodeClientEnvelope(buffer) {
  const r = new PbReader(buffer);
  const result = {};
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
      case 4: r.skip(wireType); break; // metadata
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
  const inner = new PbWriter(); if (id !== 0) { inner.writeTag(1, 0); inner.writeVarint(id); }
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
