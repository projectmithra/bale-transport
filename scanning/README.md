# Open IP Lane Scanning Methodology

## Project Mithra

### Overview

The Open IP Lane is a censorship circumvention routing technique that exploits IP address sharing between Iranian domestic services and international CDN infrastructure. Iran's firewall cannot block these shared IP addresses without disrupting its own domestic economy.

This document describes the scanning methodology, tools, and results.

### The Problem

Iran's censorship infrastructure operates at multiple layers:

- **Layer 1 (IP):** Blocks known VPN and proxy server IP addresses
- **Layer 2 (TLS):** Inspects SNI fields and TLS fingerprints
- **Layer 3 (HTTP):** Inspects Host headers and WebSocket paths
- **Layer 4 (Content):** Inspects binary WebSocket frame contents

During normal censorship, Iran blocks international IPs associated with VPN services while keeping CDN IPs accessible for domestic services. During total blackout events, Iran blocks ALL international IPs at the network layer.

### The Solution

**Cloudflare Open IP Lane:** Many Iranian financial services, payment gateways, and e-commerce platforms use Cloudflare's CDN. These services share IP addresses with Cloudflare's global infrastructure. By routing tunnel traffic through these shared IPs, the tunnel appears to be connecting to an Iranian financial service at the IP layer.

**Google Domain Fronting:** Google's 216.239.x.x frontend infrastructure routes requests by Host header without enforcing SNI matching. This allows setting the SNI to a trusted Google service (e.g., fcm.googleapis.com) while the Host header routes to a different Google Cloud destination.

### Scanning Methodology

#### Phase 1: Cloudflare IP Discovery

1. **Domain collection:** Compile a list of 400+ Iranian domains across all sectors (financial, e-commerce, government, media, education, telecom, etc.)

2. **DNS resolution:** Resolve each domain against both Cloudflare DNS (1.1.1.1) and Google DNS (8.8.8.8). Iranian ISP resolvers may return geographically optimized IPs that differ from global resolvers.

3. **Cloudflare identification:** Filter results to identify IPs within Cloudflare's known ranges:
   - 104.16.0.0/12
   - 172.64.0.0/13
   - 188.114.96.0/20
   - 103.21.0.0/16

4. **Deduplication and subnet analysis:** Group unique IPs by /24 subnet to identify ranges with high concentration of Iranian services.

5. **Connectivity verification:** Test each IP against the Cloudflare Worker relay on all 6 supported TLS ports (443, 8443, 2053, 2083, 2087, 2096) using curl with --resolve to force routing through the specific IP.

#### Phase 2: Google Domain Fronting Discovery

1. **Frontend IP enumeration:** Test Google's 216.239.x.x IP range for TLS-accepting frontends.

2. **SNI enumeration:** Test multiple googleapis.com subdomains as SNI values:
   - fcm.googleapis.com (Firebase Cloud Messaging)
   - firestore.googleapis.com
   - android.googleapis.com
   - logging.googleapis.com
   - monitoring.googleapis.com
   - pubsub.googleapis.com
   - storage.googleapis.com
   - play.googleapis.com
   - And others

3. **Combination testing:** Test each IP × SNI combination for successful TLS connection. The Host header can be set to a Cloud Run hostname to route the request to the tunnel server.

4. **Certificate verification:** The certificate served is from edgecert.googleapis.com with a wildcard *.googleapis.com, which covers all tested SNI values.

### Results (March 2026 Scan)

#### Cloudflare

| Metric | Value |
|--------|-------|
| Domains scanned | 464 |
| Domains on Cloudflare | 33 |
| Unique Cloudflare IPs | 50 |
| Unique /24 subnets | 46 |
| Working endpoint combinations | 600 |
| IP ranges | 104.16.x.x, 104.17.x.x, 104.20.x.x, 104.21.x.x, 172.66.x.x, 172.67.x.x, 188.114.x.x |

Notable Iranian services sharing Cloudflare IPs include payment processors (nextpay.ir, zarinpal.com), cryptocurrency exchanges (exchanger.ir, exir.io, sarafi.io), e-commerce platforms (bamilo.com, pintapin.com), media (radiojavan.com, nex1music.ir), blogs (blogfa.com), and social platforms (cloob.com).

All 50 IPs work on all 6 Cloudflare TLS ports, providing 600 total routing options.

#### Google Domain Fronting

| Metric | Value |
|--------|-------|
| Frontend IPs tested | 10 |
| SNI values tested | 11 |
| Working combinations | 110+ |
| Certificate | edgecert.googleapis.com (wildcard) |

**Important limitation:** During total L3 blackout on MCI LTE, 216.239.x.x IPs are blocked. Google domain fronting is effective during normal DPI-based censorship only.

### Usage

```bash
# Full scan (Cloudflare + Google)
bash mithra-scan.sh

# Cloudflare only with Worker testing
bash mithra-scan.sh --cloudflare-only --worker-host=your-worker.example.com

# Google domain fronting only
bash mithra-scan.sh --google-only

# Save results
bash mithra-scan.sh 2>&1 | tee scan-results.txt
```

### Operational Notes

- **IP rotation:** Cloudflare rotates IPs periodically. Re-scan weekly to maintain an up-to-date list.
- **ArvanCloud migration:** Iran is migrating domestic services from Cloudflare to ArvanCloud (state-aligned CDN). As this migration progresses, the number of Iranian domains on Cloudflare will decrease. Monitor this trend.
- **DNS resolver differences:** Iranian ISP resolvers may return different Cloudflare IPs than global resolvers. Cross-checking with Google DNS captures some of these differences, but testing from inside Iran provides the most accurate results.
- **Port diversity:** Using non-standard Cloudflare ports (8443, 2053, 2083, 2087, 2096) provides additional evasion options if port 443 is specifically targeted.

### File Structure

```
mithra-scan.sh          — Main scanning script
README.md               — This document
sample-results/
  mithra-found-ips.txt   — Sample raw IP discovery output
  mithra-working-ips.txt — Sample verified working endpoints
  scan-results.txt       — Sample full scan output
```

### License

MIT — Project Mithra
