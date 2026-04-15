# Open IP Lane Scanning Methodology

## Project Mithra — v3.0

### Overview

The Open IP Lane is a censorship circumvention routing technique that exploits infrastructure sharing between Iranian domestic services and international CDN/cloud providers. Iran's firewall cannot block these shared IP addresses without disrupting its own domestic economy.

This document describes the scanning methodology, tools, results, and five independent evasion vectors discovered through systematic analysis of Iran's network architecture.

### Iran's Censorship Architecture

Iran's censorship operates at distinct layers, each requiring a different evasion technique:

| Layer | Mechanism | When Active | Evasion |
|-------|-----------|-------------|---------|
| L3 (IP) | IP whitelist — blocks ALL international IPs | Total blackout | Use whitelisted IPs (ArvanCloud, Vercel, shared Cloudflare) |
| DPI (SNI) | Inspects TLS ClientHello SNI field | Always | SNI spoofing, domain fronting |
| HTTP | Inspects Host headers, WebSocket paths | Always | Camouflage paths, protocol mimicry |
| Content | Inspects binary payload contents | Selective | Encryption (VLESS, xray) |

**Critical finding (March 2026):** During MCI LTE blackout, Iran enforces a complete L3 IP whitelist. Only domestic Iranian IPs are reachable. This includes ArvanCloud CDN (185.143.x.x), Bale messaging (2.189.x.x), Rubika (5.106.x.x), and select international CDN IPs shared with domestic services. Cloudflare IPs (104.x.x, 188.114.x.x) are L3-blocked on MCI during blackout. Google IPs (216.239.x.x) are also blocked. Vercel IPs (198.169.x.x, 76.76.x.x) appear whitelisted on most operators.

### Three-Tier Reachability Test

Before deploying any evasion technique, determine which layer is blocking traffic:

```bash
# Run three-tier test on a target IP
bash mithra-scan.sh --tier-test=188.114.98.0

# Results:
# Tier 1 (TCP):  OPEN/BLOCKED — Is the IP L3-reachable?
# Tier 2 (Real SNI): PASS/BLOCKED — Does DPI block the real SNI?
# Tier 3 (Fake SNI): PASS/BLOCKED — Does a whitelisted SNI pass?
```

- **Tier 1 BLOCKED:** Need a different IP entirely (ArvanCloud, Vercel, DNSTT)
- **Tier 1 OPEN, Tier 2 BLOCKED:** SNI spoofing or domain fronting will work
- **All PASS:** Direct connection works, no evasion needed

### Scanning Methodology

#### Phase 1: Cloudflare IP Discovery

Identifies Cloudflare IPs shared with Iranian domestic services.

1. **Domain collection:** 464+ Iranian domains across financial, e-commerce, government, media, education, telecom, and other sectors.

2. **DNS resolution:** Resolve each domain against Cloudflare DNS (1.1.1.1) and Google DNS (8.8.8.8). Cross-check catches geographically optimized IP variations.

3. **Cloudflare identification:** Filter IPs within Cloudflare's published ranges:
   - `104.16.0.0/12` (104.16.x.x – 104.31.x.x)
   - `172.64.0.0/13` (172.64.x.x – 172.71.x.x)
   - `188.114.96.0/20` (188.114.96.x – 188.114.111.x)
   - `103.21.0.0/16`, `103.22.0.0/16`
   - `141.101.0.0/16`, `162.158.0.0/15`, `162.159.0.0/16`
   - `173.245.0.0/16`, `190.93.0.0/16`
   - `197.234.0.0/16`, `198.41.0.0/16`

4. **Subnet analysis:** Group by /24 to identify high-concentration ranges.

5. **Worker connectivity test:** Deploy a Cloudflare Worker that proxies WebSocket to a backend xray server. Verify each discovered IP routes to the Worker on all 6 TLS ports (443, 8443, 2053, 2083, 2087, 2096).

6. **SNI spoofing assessment:** IPs in 188.114.x.x range are often L3-blocked during blackout but may be reachable with SNI spoofing (injecting a fake TLS ClientHello with wrong TCP sequence number).

**SNI Spoofing technique (patterniha method):**
- After TCP 3-way handshake, inject a FAKE TLS ClientHello with a whitelisted SNI (e.g., `vercel.com`) using an incorrect TCP sequence number
- DPI reads the fake SNI and whitelists the connection
- Server drops the fake packet (out-of-window sequence number)
- Real ClientHello passes through DPI unblocked
- Tools: `patterniha/SNI-Spoofing` (Python/Windows), `therealaleph/sni-spoofing-rust` (Rust, cross-platform)
- Limitation: Requires raw socket access — works on Windows/Linux/macOS, NOT on iOS/Android

#### Phase 2: Vercel Domain Fronting

Vercel routes requests by HTTP Host header, enabling true domain fronting.

1. **IP discovery:** Scan known Vercel IP ranges:
   - `76.76.21.0/24` (primary Anycast)
   - `198.169.0.0/16` (additional Anycast)
   - `64.29.18.0/24`

2. **SNI verification:** Test which SNI values are accepted:
   - `vercel.com` — whitelisted on most Iranian operators
   - `nextjs.org` — whitelisted on MCI
   - `vercel.app` — whitelisted on most operators

3. **Domain fronting test:** Connect with a whitelisted SNI (e.g., `vercel.com`) and a Host header pointing to a custom Vercel deployment. If the deployment responds, domain fronting is confirmed.

4. **Transport:** Vercel Edge Middleware can proxy to a backend xray server. Supported transports: xhttp `packet-up` mode. WebSocket and httpupgrade are NOT supported by Vercel's edge runtime.

**Example configuration (generalized):**
```
SNI: vercel.com or nextjs.org (whitelisted)
Host: your-app.vercel.app (custom deployment)
IP: Any Vercel Anycast IP (e.g., 76.76.21.x, 198.169.x.x)
Transport: xhttp packet-up
Backend: xray server via Edge Middleware proxy
```

#### Phase 3: ArvanCloud Cross-Routing

ArvanCloud is Iran's domestic CDN — its IPs are always whitelisted, even during total blackout.

1. **IP enumeration:** Scan ArvanCloud CDN ranges:
   - `185.143.232.0/22` (primary CDN)
   - `185.215.232.0/22` (secondary)
   - `188.121.104.0/21` (IaaS/compute)

2. **SNI cross-routing test:** ArvanCloud routes by SNI across all CDN IPs. Test if known Iranian domains (snapp.ir, bmi.ir, divar.ir) serve valid certificates from any ArvanCloud IP.

3. **Registration analysis:** ArvanCloud account creation requires Iranian IP — the registration API (`dejban.arvancloud.ir`) is geo-fenced to Iranian source IPs only. The CDN CNAME target is `r185.arvancdn.ir` (→ 185.143.233.235, 185.143.234.235).

4. **Operational model:** An operator with an ArvanCloud account registers a domain, points origin to their xray server, enables cloud proxy + HTTPS. Users connect to the ArvanCloud CDN IP with the registered domain's SNI. DPI sees domestic Iranian traffic. The CDN forwards to the xray origin.

**Key findings:**
- ArvanCloud cross-routes by SNI across all CDN IPs (any IP serves any registered domain)
- Unknown/unregistered SNIs are rejected (connection dropped)
- Registration geo-fencing prevents account creation from outside Iran
- Working transport: `VLESS+TCP+TLS` to ArvanCloud IP with Iranian domain SNI
- No xhttp, WebSocket, or other complex transports needed — plain TCP works

#### Phase 4: Google Domain Fronting

Google's 216.239.x.x frontend routes by Host header without enforcing SNI matching.

1. **Frontend IP enumeration:** Test 216.239.x.x range for TLS-accepting frontends.

2. **SNI enumeration:** Test googleapis.com subdomains (fcm, firestore, android, logging, monitoring, pubsub, storage, play, etc.).

3. **Host header routing:** Set Host to a Cloud Run hostname to route through Google's frontend to a custom xray server.

**Limitation:** During total L3 blackout, 216.239.x.x is blocked on MCI LTE. Effective during normal DPI-based censorship only.

#### Phase 5: DNSTT Resolver Discovery

DNS tunneling is the most reliable fallback — DNS resolvers are never blocked because DNS is essential infrastructure.

1. **Resolver enumeration:** Test known Iranian ISP DNS resolvers:
   - MCI: `2.179.60.25`, `2.179.64.30`, `2.179.67.90`, `2.179.64.79`
   - TCI: `217.218.155.155`, `217.218.127.127`
   - Irancell: `2.144.4.154`
   - Internal: `10.202.10.10`, `10.202.10.11`
   - Pars Online: `78.39.224.100`, `78.39.224.200`

2. **EDNS support test:** Verify resolvers support EDNS0 (required for larger DNS responses, critical for DNSTT throughput).

3. **NXDOMAIN hijacking test:** Some resolvers hijack NXDOMAIN responses with captive portals, breaking DNSTT.

4. **End-to-end DNSTT test:** If a DNSTT server domain is provided, verify the resolver forwards queries to the authoritative nameserver and the tunnel establishes.

5. **Comprehensive scanning:** Use `findns` tool (github.com/SamNet-dev/findns) with 7,800+ bundled Iranian resolvers for exhaustive discovery.

**DNSTT server architecture:**
- Protocol: NoizDNS (DPI-resistant fork of DNSTT by anonvector) or standard DNSTT
- Server: UDP:53 on a VM with NS delegation (e.g., `t.example.com` → server IP)
- Backend: Forwards to SSH (port 22) or xray SOCKS5 proxy
- Throughput: 30–50 KB/s typical (sufficient for messaging, basic browsing)

**Client compatibility:**

| Platform | App | DNSTT Mode | Notes |
|----------|-----|------------|-------|
| Android | SlipNet | DNSTT/NoizDNS/VayDNS | Built-in DNS scanner, recommended |
| Android | MahsaNG | DNSTT | Built-in DNS scanner, free configs |
| Android | HTTP Injector | SSH-DNSTT | Requires SSH backend |
| iOS | Npv Tunnel | SSH-DNSTT | Requires SSH backend on server |
| iOS | V2Box | DNSTT | Buggy — converts to hy2socks internally |
| Desktop | dnstt.xyz app | DNSTT/Slipstream | Windows/macOS/Linux |
| Desktop | dnstt-client CLI | DNSTT | Original reference client |

### Results (April 2026 Scan)

#### Cloudflare Open IP Lane

| Metric | Value |
|--------|-------|
| Domains scanned | 464+ |
| Domains on Cloudflare | 33+ |
| Unique Cloudflare IPs | 50+ |
| Unique /24 subnets | 46+ |
| Working endpoint combinations | 600+ |
| IP ranges | 104.16-21.x.x, 172.66-67.x.x, 188.114.x.x |
| L3 status during blackout | BLOCKED on MCI (188.114.x.x confirmed) |
| SNI spoofing viability | Depends on L3 reachability per operator |

#### Vercel Domain Fronting

| Metric | Value |
|--------|-------|
| IPs tested | 12+ |
| Working domain fronting IPs | 25+ across 8 /24 ranges |
| Whitelisted SNIs | vercel.com, nextjs.org, vercel.app |
| L3 status during blackout | Whitelisted on most operators |
| Transport | xhttp packet-up (xray 26.2.6+) |
| Domain fronting confirmed | YES — SNI≠Host routing verified |

#### ArvanCloud Cross-Routing

| Metric | Value |
|--------|-------|
| CDN IPs tested | 8 |
| Cross-routing confirmed | YES — any IP serves any registered domain |
| L3 status during blackout | Always whitelisted (domestic) |
| Registration geo-fence | Iranian IP required |
| Working transport | VLESS+TCP+TLS (plain, no CDN proxy needed for IaaS) |

#### Google Domain Fronting

| Metric | Value |
|--------|-------|
| Frontend IPs tested | 10 |
| Working combinations | 110+ |
| L3 status during blackout | BLOCKED on MCI |
| Use case | Normal DPI censorship only |

#### DNSTT Resolvers

| Metric | Value |
|--------|-------|
| Known working resolvers | 5+ (from community testing) |
| Available for scanning | 7,800+ (via findns tool) |
| L3 status during blackout | Always available (DNS is essential) |
| Throughput | 30–50 KB/s |

### Usage

```bash
# Full scan (all 5 phases)
bash mithra-scan.sh

# Cloudflare only with Worker testing
bash mithra-scan.sh --cloudflare-only --worker-host=your-worker.workers.dev

# Vercel domain fronting with deployment testing
bash mithra-scan.sh --vercel-only --vercel-host=your-app.vercel.app

# ArvanCloud cross-routing
bash mithra-scan.sh --arvancloud-only

# Google domain fronting
bash mithra-scan.sh --google-only

# DNSTT resolver discovery with tunnel verification
bash mithra-scan.sh --dnstt-only --dnstt-domain=t.example.com

# Three-tier test on specific IP
bash mithra-scan.sh --tier-test=188.114.98.0

# Save results
bash mithra-scan.sh 2>&1 | tee scan-results-$(date +%Y%m%d).txt
```

### Operational Notes

- **IP rotation:** Cloudflare and Vercel rotate IPs periodically. Re-scan weekly.
- **ArvanCloud migration:** Iran is migrating domestic services from Cloudflare to ArvanCloud. Monitor this trend — it reduces Cloudflare shared IPs but increases ArvanCloud routing options.
- **DNS resolver differences:** Iranian ISP resolvers return different CDN IPs than global resolvers. Testing from inside Iran provides the most accurate results.
- **Port diversity:** Cloudflare supports non-standard TLS ports (8443, 2053, 2083, 2087, 2096) providing additional evasion if port 443 is targeted.
- **Operator variation:** Different Iranian mobile operators (MCI, Irancell, Rightel) have different whitelist policies. Test per-operator.
- **V2Box iOS limitations:** V2Box DNSTT on iOS is buggy (converts configs to hy2socks). Use Npv Tunnel for SSH-DNSTT or Vercel domain fronting for xhttp.
- **SNI spoofing requires admin/root:** The patterniha technique needs raw socket access. Works on Windows (WinDivert), Linux (AF_PACKET), macOS (BPF). Not possible on stock iOS/Android.

### File Structure

```
mithra-scan.sh              — Main scanning script (5 phases)
README.md                   — This methodology document
sample-results/
  mithra-found-ips.txt       — Cloudflare IP discovery
  mithra-vercel-ips.txt      — Vercel domain fronting IPs
  mithra-arvancloud-ips.txt  — ArvanCloud cross-routing IPs
  mithra-google-ips.txt      — Google domain fronting IPs
  mithra-dnstt-resolvers.txt — DNSTT resolver results
  mithra-working-ips.txt     — All verified working endpoints
  mithra-tier-results.txt    — Three-tier test results
```

### References

- IRBlock paper (USENIX Security '25) — Confirms MCI <1% normal GFI filtering during blackout; separate L3 whitelist
- patterniha/SNI-Spoofing — TCP-level fake ClientHello injection for DPI bypass
- therealaleph/sni-spoofing-rust — Cross-platform Rust implementation of SNI spoofing
- SamNet-dev/findns — DNSTT resolver scanner with 7,800+ bundled Iranian resolvers
- anonvector/SlipNet — Android DNSTT/NoizDNS client with built-in DNS scanner
- net2share/dnstm-setup — Automated DNSTT server deployment with multiple protocols
- GFW-knocker/MahsaNG — Android V2Ray client with DNSTT and Fragment support

### License
MIT — Project Mithra
MIT — Project Mithra
