#!/bin/bash
# ============================================================
# MITHRA OPEN IP LANE SCANNER v3.0
# Project Mithra — Funded by the Open Technology Fund
#
# Multi-vector scanning tool for censorship circumvention
# infrastructure discovery. Identifies IP addresses shared
# between international CDN providers and Iranian domestic
# services, discovers domain fronting opportunities, and
# tests reachability at multiple network layers.
#
# Vectors scanned:
#   1. Cloudflare Open IP Lane (shared CDN IPs)
#   2. Vercel Domain Fronting (Host header routing)
#   3. ArvanCloud Cross-Routing (domestic CDN SNI routing)
#   4. Google Domain Fronting (216.239.x.x Host routing)
#   5. DNSTT DNS Tunnel Resolver Discovery
#
# Usage:
#   bash mithra-scan.sh                    # Full scan
#   bash mithra-scan.sh --cloudflare-only  # Cloudflare only
#   bash mithra-scan.sh --vercel-only      # Vercel only
#   bash mithra-scan.sh --arvancloud-only  # ArvanCloud only
#   bash mithra-scan.sh --google-only      # Google only
#   bash mithra-scan.sh --dnstt-only       # DNSTT resolvers only
#   bash mithra-scan.sh --tier-test=IP     # Three-tier test on IP
#   bash mithra-scan.sh --help             # Show help
#
# Output:
#   /tmp/mithra-found-ips.txt     — Cloudflare IPs
#   /tmp/mithra-working-ips.txt   — Verified working endpoints
#   /tmp/mithra-google-ips.txt    — Google domain fronting IPs
#   /tmp/mithra-vercel-ips.txt    — Vercel IPs
#   /tmp/mithra-arvancloud-ips.txt — ArvanCloud IPs
#   /tmp/mithra-dnstt-resolvers.txt — Working DNSTT resolvers
#
# Requirements: dig, curl, openssl, bash 4+
# License: MIT
# ============================================================

set -o pipefail

VERSION="3.0"
SCAN_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Output files
FOUND_IPS="/tmp/mithra-found-ips.txt"
WORKING_IPS="/tmp/mithra-working-ips.txt"
GOOGLE_IPS="/tmp/mithra-google-ips.txt"
VERCEL_IPS="/tmp/mithra-vercel-ips.txt"
ARVANCLOUD_IPS="/tmp/mithra-arvancloud-ips.txt"
DNSTT_RESOLVERS="/tmp/mithra-dnstt-resolvers.txt"
TIER_RESULTS="/tmp/mithra-tier-results.txt"
> "$FOUND_IPS"
> "$WORKING_IPS"
> "$GOOGLE_IPS"
> "$VERCEL_IPS"
> "$ARVANCLOUD_IPS"
> "$DNSTT_RESOLVERS"
> "$TIER_RESULTS"

# Parse arguments
SCAN_CF=true
SCAN_GOOGLE=true
SCAN_VERCEL=true
SCAN_ARVANCLOUD=true
SCAN_DNSTT=true
TIER_TEST_IP=""
WORKER_HOST=""
WORKER_UUID=""
VERCEL_HOST=""
DNSTT_DOMAIN=""

for arg in "$@"; do
  case $arg in
    --cloudflare-only) SCAN_GOOGLE=false; SCAN_VERCEL=false; SCAN_ARVANCLOUD=false; SCAN_DNSTT=false ;;
    --google-only) SCAN_CF=false; SCAN_VERCEL=false; SCAN_ARVANCLOUD=false; SCAN_DNSTT=false ;;
    --vercel-only) SCAN_CF=false; SCAN_GOOGLE=false; SCAN_ARVANCLOUD=false; SCAN_DNSTT=false ;;
    --arvancloud-only) SCAN_CF=false; SCAN_GOOGLE=false; SCAN_VERCEL=false; SCAN_DNSTT=false ;;
    --dnstt-only) SCAN_CF=false; SCAN_GOOGLE=false; SCAN_VERCEL=false; SCAN_ARVANCLOUD=false ;;
    --tier-test=*) TIER_TEST_IP="${arg#*=}" ;;
    --worker-host=*) WORKER_HOST="${arg#*=}" ;;
    --worker-uuid=*) WORKER_UUID="${arg#*=}" ;;
    --vercel-host=*) VERCEL_HOST="${arg#*=}" ;;
    --dnstt-domain=*) DNSTT_DOMAIN="${arg#*=}" ;;
    --help)
      echo "Mithra Open IP Lane Scanner v$VERSION"
      echo ""
      echo "Usage: bash mithra-scan.sh [options]"
      echo ""
      echo "Scan options:"
      echo "  --cloudflare-only     Scan Cloudflare IPs only"
      echo "  --vercel-only         Scan Vercel domain fronting only"
      echo "  --arvancloud-only     Scan ArvanCloud IPs only"
      echo "  --google-only         Scan Google domain fronting only"
      echo "  --dnstt-only          Scan DNSTT DNS resolvers only"
      echo ""
      echo "Testing options:"
      echo "  --tier-test=IP        Run three-tier reachability test on IP"
      echo "  --worker-host=HOST    Cloudflare Worker hostname for testing"
      echo "  --worker-uuid=UUID    VLESS UUID for config generation"
      echo "  --vercel-host=HOST    Vercel deployment hostname"
      echo "  --dnstt-domain=DOM    DNSTT tunnel domain (e.g., t.example.com)"
      echo ""
      echo "  --help                Show this help"
      exit 0
      ;;
  esac
done

echo "================================================================"
echo "  MITHRA OPEN IP LANE SCANNER v$VERSION"
echo "  $SCAN_DATE"
echo "================================================================"
echo ""

# ============================================================
# THREE-TIER REACHABILITY TEST
# ============================================================
# Iran's censorship operates at multiple layers. Testing each
# layer independently reveals which evasion technique is needed.
#
# Tier 1 (L3/IP): TCP SYN reachability — is the IP blocked?
# Tier 2 (DPI/SNI): TLS with real SNI — does DPI block it?
# Tier 3 (Whitelist): TLS with whitelisted SNI — does spoofing work?
# ============================================================
three_tier_test() {
  local ip="$1"
  local real_sni="${2:-test.example.com}"
  local fake_sni="${3:-vercel.com}"
  local port="${4:-443}"

  echo "  Three-tier test: $ip:$port"

  # Tier 1: TCP SYN reachability
  if timeout 5 bash -c "echo > /dev/tcp/$ip/$port" 2>/dev/null; then
    echo "    Tier 1 (TCP):  OPEN"
    echo "$ip TIER1_OPEN" >> "$TIER_RESULTS"
  else
    echo "    Tier 1 (TCP):  BLOCKED (L3 IP block)"
    echo "$ip TIER1_BLOCKED" >> "$TIER_RESULTS"
    return
  fi

  # Tier 2: TLS with real SNI
  local cert=$(echo | timeout 5 openssl s_client -connect "$ip:$port" -servername "$real_sni" 2>/dev/null | grep 'subject=' | head -1)
  if [ -n "$cert" ]; then
    echo "    Tier 2 (Real SNI=$real_sni): PASS — $cert"
    echo "$ip TIER2_PASS $real_sni" >> "$TIER_RESULTS"
  else
    echo "    Tier 2 (Real SNI=$real_sni): BLOCKED (DPI/SNI filter)"
    echo "$ip TIER2_BLOCKED $real_sni" >> "$TIER_RESULTS"
  fi

  # Tier 3: TLS with whitelisted SNI
  cert=$(echo | timeout 5 openssl s_client -connect "$ip:$port" -servername "$fake_sni" 2>/dev/null | grep 'subject=' | head -1)
  if [ -n "$cert" ]; then
    echo "    Tier 3 (Fake SNI=$fake_sni): PASS — $cert"
    echo "$ip TIER3_PASS $fake_sni" >> "$TIER_RESULTS"
  else
    echo "    Tier 3 (Fake SNI=$fake_sni): BLOCKED (whitelist enforcement)"
    echo "$ip TIER3_BLOCKED $fake_sni" >> "$TIER_RESULTS"
  fi
}

if [ -n "$TIER_TEST_IP" ]; then
  echo "=== Three-Tier Reachability Test ==="
  echo ""
  three_tier_test "$TIER_TEST_IP"
  echo ""
fi

# ============================================================
# CDN IDENTIFICATION FUNCTIONS
# ============================================================
is_cloudflare() {
  local ip="$1"
  [[ "$ip" =~ ^104\.(1[6-9]|2[0-9]|3[01])\. ]] || \
  [[ "$ip" =~ ^172\.6[4-9]\. ]] || \
  [[ "$ip" =~ ^172\.7[0-1]\. ]] || \
  [[ "$ip" =~ ^188\.114\.(9[6-9]|1[0-1][0-9])\. ]] || \
  [[ "$ip" =~ ^103\.2[12]\. ]] || \
  [[ "$ip" =~ ^103\.22\. ]] || \
  [[ "$ip" =~ ^141\.101\. ]] || \
  [[ "$ip" =~ ^162\.15[89]\. ]] || \
  [[ "$ip" =~ ^173\.245\. ]] || \
  [[ "$ip" =~ ^190\.93\. ]] || \
  [[ "$ip" =~ ^197\.234\. ]] || \
  [[ "$ip" =~ ^198\.41\. ]]
}

is_vercel() {
  local ip="$1"
  [[ "$ip" =~ ^76\.76\.(2[1-2]|3[0-1])\. ]] || \
  [[ "$ip" =~ ^198\.169\. ]] || \
  [[ "$ip" =~ ^64\.29\.18\. ]] || \
  [[ "$ip" =~ ^76\.223\. ]]
}

is_arvancloud() {
  local ip="$1"
  [[ "$ip" =~ ^185\.143\.(23[2-5]|233|234)\. ]] || \
  [[ "$ip" =~ ^185\.215\.23[2-5]\. ]] || \
  [[ "$ip" =~ ^188\.121\.(10[4-9]|1[1-9][0-9])\. ]] || \
  [[ "$ip" =~ ^5\.106\. ]] || \
  [[ "$ip" =~ ^95\.156\. ]]
}

# ============================================================
# PHASE 1: CLOUDFLARE SCAN
# ============================================================
if $SCAN_CF; then

echo "=== PHASE 1: Cloudflare IP Discovery ==="
echo ""
echo "Scanning Iranian domains for Cloudflare hosting..."
echo ""

# Master domain list — 464+ Iranian domains across all sectors
DOMAINS=(
  # FINANCIAL / PAYMENT / BANKING
  tsetmc.ir codal.ir ifb.ir ime.co.ir seo.ir cbi.ir shaparak.ir
  samanepay.ir sep.ir bpm.ir asan.ir zarinpal.ir zarinpal.com
  api.zarinpal.com www.zarinpal.com idpay.ir api.idpay.ir www.idpay.ir
  nextpay.ir api.nextpay.ir panel.nextpay.ir www.nextpay.ir
  payir.ir pay.ir www.pay.ir pardakhtpay.ir jibit.ir vandar.ir
  zibal.ir poolban.ir payping.ir paystar.ir sizpay.ir
  behpardakht.com sadad.ir pecco.ir psp.co.ir pna.co.ir
  bmi.ir www.bmi.ir ib.bmi.ir sb24.com www.sb24.com
  bankmellat.ir pec.ir fanap.ir fanvap.com farabixo.com
  # CRYPTO / FINTECH
  wallex.ir ramzinex.ir excoino.ir bit24.cash aban-tether.com
  nobitex.market arzdigital.com arzinja.com coiniran.com
  tetherland.com sarafi.io exchanger.ir api.exchanger.ir
  www.exchanger.ir panel.exchanger.ir sarmayex.com exir.io
  bitpin.ir okcoin.ir toman.ir
  # E-COMMERCE / SHOPPING
  torob.com torob.ir emalls.com emalls.ir basalam.com
  pintapin.com sheypoor.com divar.ir esam.ir akhondak.com
  mobile.ir meghdadit.com priceto.ir zoomtech.ir
  techno-life.com shopfa.com roshd.ir digikala.com
  digiexpress.ir digistyle.com zanbil.ir bamilo.com
  modiseh.com netbarg.com takhfifan.com okala.com
  sibche.com sibapp.com sibdigi.com charkhoneh.com
  sibmo.ir anardoni.ir anardoni.com
  # CLASSIFIEDS / JOBS
  jobinja.ir jobvision.ir e-estekhdam.com iranestekhdam.ir
  karnameh.com niazpardaz.com niazmandi.com iran-tejarat.com
  # REAL ESTATE / AUTO / TRAVEL
  kilid.ir homisha.com shab.ir bama.ir ikido.ir car.ir
  carmada.ir bazar.ir hamrah-mechanic.com irantib.com
  karnaval.ir alibaba.ir flightio.com safarmarket.com
  eligasht.com snapptrip.com otaghak.com jabama.com homsa.ir
  pin.ir hotelyar.com eghamat24.com mrbilit.com ghasedak24.com
  iranair.com mahan.aero flypersia.ir kish.aero raja.ir
  safar724.com irantrainticket.com trip.ir respina24.ir
  # RIDE / FOOD
  snapp.ir snapp.cab maxim.ir tapsi.cab tapsi.ir
  alopeyk.com miare.ir peydaa.ir
  snappfood.ir chilivery.com delino.com reyhoon.com
  zoodfood.com changal.com maman-pz.ir
  # TECH / STARTUPS
  virgool.io cafebazaar.ir myket.ir tapsel.ir tapsell.ir
  yektanet.com yektanet.ir sabavision.com sotoon.ir
  abrarvan.com hamrahvas.com evand.com
  # MAPS / EDUCATION
  neshan.org balad.ir cedarmaps.com waze.ir
  7learn.com faradars.org maktabkhooneh.org toplearn.com
  daneshjooyar.com classino.com roocket.ir sokanacademy.com quera.org
  # UNIVERSITIES
  ut.ac.ir sharif.edu sharif.ir aut.ac.ir iust.ac.ir
  modares.ac.ir sbu.ac.ir guilan.ac.ir um.ac.ir
  tabrizu.ac.ir shirazu.ac.ir kntu.ac.ir pnu.ac.ir
  iau.ir azad.ac.ir sid.ir noormags.ir magiran.com
  civilica.com ensani.ir irandoc.ac.ir elmnet.ir
  # MEDIA / ENTERTAINMENT
  filimo.com namava.ir telewebion.com aparat.com aparatbaby.ir
  navaak.com tiwall.com cinematicket.org gapfilm.ir lenz.ir
  tamasha.com taaghche.com fidibo.com ketabrah.ir navaar.ir
  # MUSIC
  radiojavan.com media.radiojavan.com api.radiojavan.com
  host1.radiojavan.com mybia2music.com melobit.com
  ahangimo.com nex1music.ir api.nex1music.ir www.nex1music.ir
  dl.nex1music.ir beeptunes.com
  # SOCIAL / MESSAGING
  rubika.ir igap.net soroush-app.ir gap.im bisphone.com
  cloob.com www.cloob.com api.cloob.com facenama.com
  wisgoon.com lenzor.com balatarin.com
  # DOWNLOADS
  sarzamindownload.com p30download.ir soft98.ir yasdl.com
  uploadboy.com farsroid.com downloadsoftware.ir p30world.com mihandownload.com
  # NEWS
  irna.ir tasnimnews.com mehrnews.com isna.ir farsnews.ir
  yjc.ir yjc.news khabaronline.ir tabnak.ir entekhab.ir
  mashreghnews.ir iribnews.ir donya-e-eqtesad.com
  eghtesadnews.com tejaratnews.com boursenews.ir sena.ir
  tahlilbazaar.com varzesh3.com namnak.com ninisite.com
  rokna.ir rokna.net alef.ir jamejamonline.ir
  bartarinha.ir beytoote.com asriran.com hamshahri.ir
  aftabnews.ir mizan.news
  # TECH NEWS / SPORTS
  zoomit.ir digiato.com gsm.ir sakhtafzarmag.com techfars.com gadgetnews.net
  footballi.net 90tv.ir soccerway.ir persianfootball.com tarafdari.com
  # GOVERNMENT
  dolat.ir president.ir parliran.ir ict.gov.ir
  epolice.ir adliran.ir ssaa.ir sabteahval.ir naja.ir
  ghavanin.ir sanjesh.org azmoon.org medu.ir msrt.ir
  sis.ir post.ir 10000.ir farhang.gov.ir leader.ir khamenei.ir
  # RELIGIOUS
  hawzah.net islamquest.net aqr.ir tebyan.net ido.ir
  quran.ir iqna.ir rasekhoon.net aviny.com basij.ir
  # TELECOM / ISP
  mci.ir irancell.ir my.irancell.ir rightel.ir
  tci.ir shatel.ir asiatech.ir mokhaberat.ir tic.ir
  pishgaman.net afranet.com respina.net samantel.ir
  # BLOGS / TOOLS
  blogfa.com blogsky.com mihanblog.com persianblog.ir blog.ir
  vajehyab.com abadis.ir persiantools.com
  loxblog.com rozblog.com modirnameh.ir ketab.ir sakha.ir dictinary.ir
  # INSURANCE
  bimeh.com bimito.com azki.com iraninsurance.ir
  bimehasia.ir pasargadinsurance.ir saman-insurance.ir
  # HEALTH
  paziresh24.com ninibanban.com hidoctor.ir salamat.ir
  doctorhub.ir doctoreto.com snappdr.com snappdr.ir drdr.ir
  avinmed.ir daroogist.com pharmed-ci.com drsaina.com darooyab.ir
  abidi.ir roozdaroo.com
  # HOSTING / CDN
  nic.ir irnic.ir host.ir mizbanfa.com hostiran.net parspack.com
  cloudzy.com www.cloudzy.com panel.cloudzy.com api.cloudzy.com
  parsdata.com serveriran.com limoo.host dotin.ir
  cdn.ir arvancloud.ir arvancloud.com cdn.arvancloud.com
  hamravesh.com liara.ir darkube.ir pushe.co
  pars-host.com iranhost.com webafrooz.com hostgator.ir
  parsonline.com abriment.com nividweb.com wpkit.ir
  fandogh.cloud chabokan.com metrix.ir adro.ir mediaad.org sabaidea.com shecan.ir
  # STOCK / FINANCE
  emofid.com agah.com tadbirrlc.ir sahamyab.com rahavard365.com khodrobank.com
  # GAMING
  bazinama.com gamefa.com zula.ir playsho.ir respawnfirst.ir
  gameshot.ir gamepoint.ir pes.ir hazfi.ir takgoal.com footba11.ir goal90.ir livescore.ir
  # BALE / RUBIKA (domestic messaging)
  web.bale.ai bale.ai next-ws.bale.ai rubika.ir
  # MISC
  api.ir assets.ir img.ir inbox.ir mail.ir chmail.ir parsmail.com
  webiran.ir api.webiran.ir www.webiran.ir
  raz.ir api.raz.ir www.raz.ir pwa.ir api.pwa.ir www.pwa.ir
  nahad.ir oghaf.ir fanaptelecom.ir hiweb.ir charge.ir irancell.shop
  ganj.irandoc.ac.ir ricest.ac.ir isic.ac.ir
  # INTERNATIONAL ON CF (for IP diversity)
  discord.com canva.com medium.com notion.so figma.com
  zoom.us slack.com trello.com shopify.com patreon.com
  udemy.com coursera.org npmjs.com jsdelivr.net
)

DOMAINS=($(printf '%s\n' "${DOMAINS[@]}" | sort -u))
echo "Scanning ${#DOMAINS[@]} unique domains..."
echo ""

CF_COUNT=0
for d in "${DOMAINS[@]}"; do
  IPS=$(dig +short "$d" @1.1.1.1 2>/dev/null | grep -E '^[0-9]+\.' | head -3)
  for ip in $IPS; do
    if is_cloudflare "$ip"; then
      echo "  CF  $d -> $ip"
      echo "$ip $d" >> "$FOUND_IPS"
      CF_COUNT=$((CF_COUNT + 1))
    fi
  done
done

echo ""
echo "Found $CF_COUNT Cloudflare domain/IP pairs"

# Cross-check with Google DNS
echo ""
echo "Cross-checking with Google DNS (8.8.8.8)..."
UNIQUE_DOMAINS=$(awk '{print $2}' "$FOUND_IPS" | sort -u)
GOOGLE_NEW=0
for d in $UNIQUE_DOMAINS; do
  GOOGLE_IPS_LIST=$(dig +short "$d" @8.8.8.8 2>/dev/null | grep -E '^[0-9]+\.' | head -3)
  for ip in $GOOGLE_IPS_LIST; do
    if is_cloudflare "$ip"; then
      if ! grep -q "^$ip " "$FOUND_IPS"; then
        echo "  NEW (Google DNS) $d -> $ip"
        echo "$ip $d (google-dns)" >> "$FOUND_IPS"
        GOOGLE_NEW=$((GOOGLE_NEW + 1))
      fi
    fi
  done
done
echo "  $GOOGLE_NEW additional IPs from Google DNS"

# Summary
echo ""
echo "--- Cloudflare Results ---"
UNIQUE_IPS=$(awk '{print $1}' "$FOUND_IPS" | sort -u)
IP_COUNT=$(echo "$UNIQUE_IPS" | grep -c .)
echo "  Unique IPs: $IP_COUNT"
echo "  Grouped by /24 subnet:"
echo "$UNIQUE_IPS" | awk -F. '{print $1"."$2"."$3".0/24"}' | sort -u -t. -k1,1n -k2,2n -k3,3n | while read subnet; do
  PREFIX=$(echo "$subnet" | sed 's|.0/24||')
  COUNT=$(echo "$UNIQUE_IPS" | grep "^${PREFIX}\." | wc -l)
  DOMS=$(grep "^${PREFIX}\." "$FOUND_IPS" | awk '{print $2}' | sort -u | head -3 | tr '\n' ',' | sed 's/,$//')
  echo "    $subnet ($COUNT IPs) — $DOMS"
done

# Worker connectivity test
if [ -n "$WORKER_HOST" ]; then
  echo ""
  echo "--- Testing Worker Connectivity ---"
  PORTS="443 8443 2053 2083 2087 2096"
  for ip in $UNIQUE_IPS; do
    DOMS=$(grep "^$ip " "$FOUND_IPS" | awk '{print $2}' | head -3 | tr '\n' ',' | sed 's/,$//')
    for port in $PORTS; do
      RESULT=$(curl -sk --max-time 8 --resolve "${WORKER_HOST}:${port}:${ip}" "https://${WORKER_HOST}:${port}/" 2>&1)
      if echo "$RESULT" | grep -q '"ok":true\|"status":"ok"\|HTTP/'; then
        echo "  WORKING $ip:$port ($DOMS)"
        echo "CLOUDFLARE $ip $port $DOMS" >> "$WORKING_IPS"
      fi
    done
  done
fi

# SNI spoofing test (for IPs that are L3-reachable but DPI-blocked)
echo ""
echo "--- SNI Spoofing Candidates ---"
echo "  IPs in 188.114.96.0/20 range (Cloudflare, often L3-blocked during blackout):"
echo "$UNIQUE_IPS" | grep "^188\.114\." | while read ip; do
  echo "    $ip — requires SNI spoofing if L3-reachable"
done
echo "  Use --tier-test=IP to determine if SNI spoofing is viable"

fi # end SCAN_CF

# ============================================================
# PHASE 2: VERCEL DOMAIN FRONTING SCAN
# ============================================================
if $SCAN_VERCEL; then

echo ""
echo "================================================================"
echo "=== PHASE 2: Vercel Domain Fronting Discovery ==="
echo "================================================================"
echo ""
echo "Vercel routes by Host header, enabling domain fronting."
echo "SNI can be set to whitelisted domains (vercel.com, nextjs.org)"
echo "while Host header routes to a custom Vercel deployment."
echo ""

# Known Vercel IP ranges
VERCEL_TEST_IPS=(
  76.76.21.21 76.76.21.22 76.76.21.61 76.76.21.93 76.76.21.98
  76.76.21.123 76.76.21.142 76.76.21.164 76.76.21.195 76.76.21.241
  198.169.2.1 198.169.2.65
)

# Whitelisted SNIs for domain fronting (confirmed from Iranian community)
VERCEL_SNIS=(vercel.com nextjs.org)

echo "Testing ${#VERCEL_TEST_IPS[@]} IPs x ${#VERCEL_SNIS[@]} SNIs..."
echo ""

VERCEL_WORKING=0
for ip in "${VERCEL_TEST_IPS[@]}"; do
  for sni in "${VERCEL_SNIS[@]}"; do
    cert=$(echo | timeout 5 openssl s_client -connect "$ip:443" -servername "$sni" 2>/dev/null | grep 'subject=' | head -1)
    if [ -n "$cert" ]; then
      echo "  PASS $ip SNI=$sni — $cert"
      echo "$ip $sni PASS" >> "$VERCEL_IPS"
      VERCEL_WORKING=$((VERCEL_WORKING + 1))

      # Test domain fronting: SNI=whitelisted, Host=custom deployment
      if [ -n "$VERCEL_HOST" ]; then
        CODE=$(curl -sk --max-time 8 \
          --connect-to "::$ip:443" \
          --resolve "$sni:443:$ip" \
          -H "Host: $VERCEL_HOST" \
          "https://$sni/generate_204" \
          -o /dev/null -w "%{http_code}" 2>/dev/null)
        if [ "$CODE" = "204" ]; then
          echo "    DOMAIN FRONTING CONFIRMED: SNI=$sni Host=$VERCEL_HOST -> $CODE"
          echo "VERCEL_DF $ip $sni $VERCEL_HOST $CODE" >> "$WORKING_IPS"
        fi
      fi
    fi
  done
done

echo ""
echo "--- Vercel Results ---"
echo "  Working IP+SNI combinations: $VERCEL_WORKING"
echo "  Unique IPs: $(awk '{print $1}' "$VERCEL_IPS" 2>/dev/null | sort -u | wc -l)"
echo ""
echo "  Confirmed whitelisted from Iran (most operators):"
echo "    vercel.com — most operators"
echo "    vercel.app — most operators"
echo "    nextjs.org — confirmed on MCI"

fi # end SCAN_VERCEL

# ============================================================
# PHASE 3: ARVANCLOUD CROSS-ROUTING SCAN
# ============================================================
if $SCAN_ARVANCLOUD; then

echo ""
echo "================================================================"
echo "=== PHASE 3: ArvanCloud Cross-Routing Discovery ==="
echo "================================================================"
echo ""
echo "ArvanCloud is Iran's domestic CDN — always whitelisted."
echo "Any ArvanCloud IP serves any registered domain by SNI routing."
echo "Registration requires Iranian IP (dejban.arvancloud.ir geo-fenced)."
echo ""

# Known ArvanCloud CDN IP ranges
ARVAN_IPS=(
  185.143.232.200 185.143.233.200 185.143.234.200 185.143.235.200
  185.143.233.122 185.143.234.122 185.143.233.235 185.143.234.235
)

# Iranian domains known to be on ArvanCloud
ARVAN_SNIS=(snapp.ir bmi.ir divar.ir tamin.ir)

echo "Testing ${#ARVAN_IPS[@]} IPs x ${#ARVAN_SNIS[@]} SNIs..."
echo ""

ARVAN_WORKING=0
for ip in "${ARVAN_IPS[@]}"; do
  # Test TCP reachability first
  if timeout 3 bash -c "echo > /dev/tcp/$ip/443" 2>/dev/null; then
    for sni in "${ARVAN_SNIS[@]}"; do
      cert=$(echo | timeout 5 openssl s_client -connect "$ip:443" -servername "$sni" 2>/dev/null | grep 'subject=' | head -1)
      if [ -n "$cert" ]; then
        echo "  PASS $ip SNI=$sni — $cert"
        echo "$ip $sni PASS" >> "$ARVANCLOUD_IPS"
        ARVAN_WORKING=$((ARVAN_WORKING + 1))
      else
        echo "  REJECT $ip SNI=$sni"
      fi
    done
  else
    echo "  TCP CLOSED $ip"
  fi
done

echo ""
echo "--- ArvanCloud Results ---"
echo "  Working combinations: $ARVAN_WORKING"
echo "  CDN CNAME target: r185.arvancdn.ir -> 185.143.233.235, 185.143.234.235"
echo ""
echo "  Key finding: ArvanCloud cross-routes by SNI across all CDN IPs."
echo "  Unknown/unregistered SNIs are rejected (connection dropped)."
echo "  Registration API (dejban.arvancloud.ir) only accessible from Iranian IPs."

fi # end SCAN_ARVANCLOUD

# ============================================================
# PHASE 4: GOOGLE DOMAIN FRONTING SCAN
# ============================================================
if $SCAN_GOOGLE; then

echo ""
echo "================================================================"
echo "=== PHASE 4: Google Cloud Domain Fronting Discovery ==="
echo "================================================================"
echo ""
echo "Testing 216.239.x.x IPs for Host header routing..."
echo ""

GOOGLE_FRONTEND_IPS=(
  216.239.32.223 216.239.34.223 216.239.36.54 216.239.36.55
  216.239.36.56 216.239.36.57 216.239.36.58 216.239.36.59
  216.239.38.55 216.239.38.57
)

GOOGLE_SNIS=(
  fcm.googleapis.com firestore.googleapis.com android.googleapis.com
  logging.googleapis.com monitoring.googleapis.com pubsub.googleapis.com
  storage.googleapis.com play.googleapis.com
  cloudresourcemanager.googleapis.com servicemanagement.googleapis.com
  oauth2.googleapis.com
)

echo "Testing ${#GOOGLE_FRONTEND_IPS[@]} IPs x ${#GOOGLE_SNIS[@]} SNIs..."
echo ""

GOOGLE_WORKING=0
for ip in "${GOOGLE_FRONTEND_IPS[@]}"; do
  for sni in "${GOOGLE_SNIS[@]}"; do
    RESULT=$(curl -sk --max-time 5 \
      --resolve "${sni}:443:${ip}" \
      "https://${sni}/" \
      -o /dev/null -w "%{http_code}" 2>/dev/null)
    if [ "$RESULT" != "000" ] && [ "$RESULT" != "" ]; then
      echo "  WORKING $ip SNI=$sni -> HTTP $RESULT"
      echo "$ip $sni $RESULT" >> "$GOOGLE_IPS"
      GOOGLE_WORKING=$((GOOGLE_WORKING + 1))
    fi
  done
done

echo ""
echo "--- Google Domain Fronting Results ---"
echo "  Working combinations: $GOOGLE_WORKING"
echo "  Unique IPs: $(awk '{print $1}' "$GOOGLE_IPS" 2>/dev/null | sort -u | wc -l)"
echo ""
echo "  LIMITATION: During total L3 blackout on MCI, 216.239.x.x is blocked."
echo "  Google domain fronting works during normal DPI filtering only."

fi # end SCAN_GOOGLE

# ============================================================
# PHASE 5: DNSTT RESOLVER DISCOVERY
# ============================================================
if $SCAN_DNSTT; then

echo ""
echo "================================================================"
echo "=== PHASE 5: DNSTT DNS Resolver Discovery ==="
echo "================================================================"
echo ""
echo "Testing Iranian ISP DNS resolvers for DNSTT compatibility."
echo "These resolvers are whitelisted during blackout (DNS is essential)."
echo ""

# Known working Iranian DNS resolvers (from community testing)
IRAN_DNS_RESOLVERS=(
  2.179.60.25 2.179.64.30 2.179.67.90 2.179.64.79
  2.144.4.154
  10.202.10.10 10.202.10.11
  78.39.224.100 78.39.224.200
  217.218.155.155 217.218.127.127
  4.2.2.4
)

echo "Testing ${#IRAN_DNS_RESOLVERS[@]} resolvers..."
echo ""

DNSTT_WORKING=0
for dns in "${IRAN_DNS_RESOLVERS[@]}"; do
  # Test 1: Does it resolve standard domains?
  RESULT=$(dig +short google.com @$dns +time=3 +tries=1 2>/dev/null | head -1)
  if [ -n "$RESULT" ] && [ "$RESULT" != ";;" ]; then
    echo "  ALIVE $dns -> google.com = $RESULT"

    # Test 2: Does it forward to our DNSTT domain?
    if [ -n "$DNSTT_DOMAIN" ]; then
      TUNNEL=$(dig +short "$DNSTT_DOMAIN" @$dns +time=5 +tries=1 2>/dev/null | head -1)
      if [ -n "$TUNNEL" ]; then
        echo "    DNSTT FORWARD $dns -> $DNSTT_DOMAIN = $TUNNEL"
        echo "$dns DNSTT_OK $DNSTT_DOMAIN" >> "$DNSTT_RESOLVERS"
        DNSTT_WORKING=$((DNSTT_WORKING + 1))
      else
        echo "    DNSTT TIMEOUT $dns -> $DNSTT_DOMAIN"
        echo "$dns DNSTT_TIMEOUT $DNSTT_DOMAIN" >> "$DNSTT_RESOLVERS"
      fi
    else
      echo "$dns ALIVE" >> "$DNSTT_RESOLVERS"
      DNSTT_WORKING=$((DNSTT_WORKING + 1))
    fi
  else
    echo "  DEAD $dns"
    echo "$dns DEAD" >> "$DNSTT_RESOLVERS"
  fi
done

echo ""
echo "--- DNSTT Resolver Results ---"
echo "  Working resolvers: $DNSTT_WORKING / ${#IRAN_DNS_RESOLVERS[@]}"
echo ""
echo "  NOTE: These resolvers are only reachable from inside Iran."
echo "  Use findns tool (github.com/SamNet-dev/findns) for comprehensive scanning"
echo "  with 7,800+ bundled Iranian resolvers."
echo ""
echo "  Recommended tools for DNSTT clients:"
echo "    Android: SlipNet, MahsaNG, HTTP Injector"
echo "    iOS:     Npv Tunnel (SSH-DNSTT), V2Box (DNSTT - buggy)"
echo "    Desktop: dnstt.xyz app, dnstt-client CLI"

fi # end SCAN_DNSTT

# ============================================================
# PHASE 6: FINAL SUMMARY
# ============================================================
echo ""
echo "================================================================"
echo "=== FINAL SUMMARY ==="
echo "================================================================"
echo ""
echo "Scan completed: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo ""

if [ -s "$FOUND_IPS" ]; then
  CF_UNIQUE=$(awk '{print $1}' "$FOUND_IPS" | sort -u | wc -l)
  echo "Cloudflare Open IP Lane:"
  echo "  $CF_UNIQUE unique IPs"
  echo "  $(awk '{print $1}' "$FOUND_IPS" | sort -u | awk -F. '{print $1"."$2"."$3".0/24"}' | sort -u | wc -l) unique /24 subnets"
  if [ -s "$WORKING_IPS" ] && grep -q "^CLOUDFLARE" "$WORKING_IPS"; then
    echo "  $(grep -c "^CLOUDFLARE" "$WORKING_IPS") verified Worker endpoints"
  fi
fi

if [ -s "$VERCEL_IPS" ]; then
  echo ""
  echo "Vercel Domain Fronting:"
  echo "  $(awk '{print $1}' "$VERCEL_IPS" | sort -u | wc -l) unique IPs"
  echo "  $(wc -l < "$VERCEL_IPS") working IP+SNI combinations"
  if grep -q "^VERCEL_DF" "$WORKING_IPS" 2>/dev/null; then
    echo "  $(grep -c "^VERCEL_DF" "$WORKING_IPS") confirmed domain fronting endpoints"
  fi
fi

if [ -s "$ARVANCLOUD_IPS" ]; then
  echo ""
  echo "ArvanCloud Cross-Routing:"
  echo "  $(awk '{print $1}' "$ARVANCLOUD_IPS" | sort -u | wc -l) unique IPs"
  echo "  $(wc -l < "$ARVANCLOUD_IPS") working IP+SNI combinations"
fi

if [ -s "$GOOGLE_IPS" ]; then
  echo ""
  echo "Google Domain Fronting:"
  echo "  $(awk '{print $1}' "$GOOGLE_IPS" | sort -u | wc -l) unique 216.239.x.x IPs"
  echo "  $(wc -l < "$GOOGLE_IPS") working IP+SNI combinations"
fi

if [ -s "$DNSTT_RESOLVERS" ]; then
  echo ""
  echo "DNSTT Resolvers:"
  echo "  $(grep -c "ALIVE\|DNSTT_OK" "$DNSTT_RESOLVERS") working resolvers"
fi

echo ""
echo "Evasion Techniques Summary:"
echo "  1. Cloudflare + SNI Spoofing: Inject fake ClientHello with whitelisted SNI"
echo "     Tool: patterniha/SNI-Spoofing (Windows), sni-spoofing-rust (cross-platform)"
echo "  2. Vercel Domain Fronting: SNI=vercel.com, Host=custom.vercel.app"
echo "     Transport: xhttp packet-up mode via Vercel Edge Middleware"
echo "  3. ArvanCloud: Domestic CDN, always whitelisted, requires Iranian account"
echo "     Transport: VLESS+TCP+TLS with Iranian domain SNI"
echo "  4. Google Domain Fronting: SNI=*.googleapis.com, Host=Cloud Run hostname"
echo "     Limited: blocked during total L3 blackout"
echo "  5. DNSTT: DNS tunnel through Iranian ISP resolvers (always available)"
echo "     Transport: NoizDNS/DNSTT over UDP:53 to authoritative NS"
echo ""
echo "Output files:"
echo "  $FOUND_IPS"
echo "  $WORKING_IPS"
echo "  $VERCEL_IPS"
echo "  $ARVANCLOUD_IPS"
echo "  $GOOGLE_IPS"
echo "  $DNSTT_RESOLVERS"
echo "  $TIER_RESULTS"
echo ""
echo "================================================================"
