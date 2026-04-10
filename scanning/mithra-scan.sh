#!/bin/bash
# ============================================================
# MITHRA OPEN IP LANE SCANNER v2.0
# Project Mithra
#
# Discovers Cloudflare and Google Cloud IP addresses shared
# with Iranian domestic services that can be used for
# censorship circumvention routing.
#
# Usage:
#   bash mithra-scan.sh                    # Full scan
#   bash mithra-scan.sh --cloudflare-only  # Cloudflare only
#   bash mithra-scan.sh --google-only      # Google only
#   bash mithra-scan.sh --help             # Show help
#
# Output:
#   /tmp/mithra-found-ips.txt   — All discovered IPs
#   /tmp/mithra-working-ips.txt — Verified working endpoints
#   /tmp/mithra-google-ips.txt  — Google domain fronting IPs
#
# Requirements: dig, curl, bash 4+
# License: MIT
# ============================================================

set -o pipefail

VERSION="2.0"
SCAN_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Output files
FOUND_IPS="/tmp/mithra-found-ips.txt"
WORKING_IPS="/tmp/mithra-working-ips.txt"
GOOGLE_IPS="/tmp/mithra-google-ips.txt"
> "$FOUND_IPS"
> "$WORKING_IPS"
> "$GOOGLE_IPS"

# Parse arguments
SCAN_CF=true
SCAN_GOOGLE=true
WORKER_HOST=""
WORKER_UUID=""

for arg in "$@"; do
  case $arg in
    --cloudflare-only) SCAN_GOOGLE=false ;;
    --google-only) SCAN_CF=false ;;
    --worker-host=*) WORKER_HOST="${arg#*=}" ;;
    --worker-uuid=*) WORKER_UUID="${arg#*=}" ;;
    --help)
      echo "Mithra Open IP Lane Scanner v$VERSION"
      echo ""
      echo "Usage: bash mithra-scan.sh [options]"
      echo ""
      echo "Options:"
      echo "  --cloudflare-only     Scan Cloudflare IPs only"
      echo "  --google-only         Scan Google domain fronting only"
      echo "  --worker-host=HOST    Set Cloudflare Worker hostname for testing"
      echo "  --worker-uuid=UUID    Set VLESS UUID for config generation"
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
# CLOUDFLARE IP DETECTION
# ============================================================
is_cloudflare() {
  local ip="$1"
  [[ "$ip" =~ ^104\.(1[6-9]|2[0-9]|3[01])\. ]] || \
  [[ "$ip" =~ ^172\.6[4-9]\. ]] || \
  [[ "$ip" =~ ^172\.7[0-1]\. ]] || \
  [[ "$ip" =~ ^188\.114\. ]] || \
  [[ "$ip" =~ ^103\.2[12]\. ]]
}

# ============================================================
# PHASE 1: CLOUDFLARE SCAN
# ============================================================
if $SCAN_CF; then

echo "=== PHASE 1: Cloudflare IP Discovery ==="
echo ""
echo "Scanning Iranian domains for Cloudflare hosting..."
echo ""

# Master domain list — 464 Iranian domains across all sectors
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
  # INSURANCE (expanded)
  bimeh.com bimito.com azki.com iraninsurance.ir
  bimehasia.ir pasargadinsurance.ir saman-insurance.ir
  # HEALTH (expanded)
  paziresh24.com ninibanban.com hidoctor.ir salamat.ir
  doctorhub.ir doctoreto.com snappdr.com snappdr.ir drdr.ir
  avinmed.ir daroogist.com pharmed-ci.com drsaina.com darooyab.ir
  abidi.ir roozdaroo.com
  # HOSTING / CDN (expanded)
  nic.ir irnic.ir host.ir mizbanfa.com hostiran.net parspack.com
  cloudzy.com www.cloudzy.com panel.cloudzy.com api.cloudzy.com
  parsdata.com serveriran.com limoo.host dotin.ir
  cdn.ir arvancloud.ir arvancloud.com cdn.arvancloud.com
  hamravesh.com liara.ir darkube.ir pushe.co
  pars-host.com iranhost.com webafrooz.com hostgator.ir
  parsonline.com abriment.com nividweb.com wpkit.ir
  fandogh.cloud chabokan.com metrix.ir adro.ir mediaad.org sabaidea.com shecan.ir
  # STOCK / FINANCE (expanded)
  emofid.com agah.com tadbirrlc.ir sahamyab.com rahavard365.com khodrobank.com
  # SPORTS / GAMING (expanded)
  footballi.net 90tv.ir soccerway.ir persianfootball.com tarafdari.com
  bazinama.com gamefa.com zula.ir playsho.ir respawnfirst.ir
  gameshot.ir gamepoint.ir pes.ir hazfi.ir takgoal.com footba11.ir goal90.ir livescore.ir
  # GOVERNMENT (expanded)
  nahad.ir oghaf.ir
  # TELECOM (expanded)
  fanaptelecom.ir hiweb.ir charge.ir irancell.shop
  # ACADEMIC (expanded)
  ganj.irandoc.ac.ir ricest.ac.ir isic.ac.ir
  # MISC IRANIAN
  api.ir assets.ir img.ir inbox.ir mail.ir chmail.ir parsmail.com
  # BALE
  web.bale.ai bale.ai next-ws.bale.ai
  # OPEN IP LANE CANDIDATES
  webiran.ir api.webiran.ir www.webiran.ir
  raz.ir api.raz.ir www.raz.ir
  pwa.ir api.pwa.ir www.pwa.ir
  # INTERNATIONAL ON CF (diversity)
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
  GOOGLE_IPS=$(dig +short "$d" @8.8.8.8 2>/dev/null | grep -E '^[0-9]+\.' | head -3)
  for ip in $GOOGLE_IPS; do
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
IP_COUNT=$(echo "$UNIQUE_IPS" | wc -l)
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
      if echo "$RESULT" | grep -q '"ok":true\|HTTP/'; then
        echo "  WORKING $ip:$port ($DOMS)"
        echo "CLOUDFLARE $ip $port $DOMS" >> "$WORKING_IPS"
      fi
    done
  done
fi

fi # end SCAN_CF

# ============================================================
# PHASE 2: GOOGLE CLOUD DOMAIN FRONTING SCAN
# ============================================================
if $SCAN_GOOGLE; then

echo ""
echo "================================================================"
echo "=== PHASE 2: Google Cloud Domain Fronting Discovery ==="
echo "================================================================"
echo ""
echo "Testing 216.239.x.x IPs for Host header routing..."
echo ""

# Google's 216.239.x.x frontend routes by Host header without SNI enforcement
# SNI can be set to fcm.googleapis.com while Host header routes to Cloud Run
GOOGLE_FRONTEND_IPS=(
  216.239.32.223 216.239.34.223 216.239.36.54 216.239.36.55
  216.239.36.56 216.239.36.57 216.239.36.58 216.239.36.59
  216.239.38.55 216.239.38.57
)

# SNI values that work with Google's 216.239.x.x infrastructure
GOOGLE_SNIS=(
  fcm.googleapis.com
  firestore.googleapis.com
  android.googleapis.com
  logging.googleapis.com
  monitoring.googleapis.com
  pubsub.googleapis.com
  storage.googleapis.com
  play.googleapis.com
  cloudresourcemanager.googleapis.com
  servicemanagement.googleapis.com
  oauth2.googleapis.com
)

echo "Testing ${#GOOGLE_FRONTEND_IPS[@]} IPs x ${#GOOGLE_SNIS[@]} SNIs = $((${#GOOGLE_FRONTEND_IPS[@]} * ${#GOOGLE_SNIS[@]})) combinations..."
echo ""

GOOGLE_WORKING=0
for ip in "${GOOGLE_FRONTEND_IPS[@]}"; do
  for sni in "${GOOGLE_SNIS[@]}"; do
    # Test if the IP+SNI combination accepts TLS connection
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
echo "  Unique SNIs: $(awk '{print $2}' "$GOOGLE_IPS" 2>/dev/null | sort -u | wc -l)"
echo ""
echo "  NOTE: During total L3 blackout, 216.239.x.x is blocked on MCI LTE."
echo "  Google domain fronting works during normal DPI filtering only."

fi # end SCAN_GOOGLE

# ============================================================
# PHASE 3: FINAL SUMMARY
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
  echo "Cloudflare:"
  echo "  $CF_UNIQUE unique IPs found"
  echo "  $(awk '{print $1}' "$FOUND_IPS" | sort -u | awk -F. '{print $1"."$2"."$3".0/24"}' | sort -u | wc -l) unique /24 subnets"
  if [ -s "$WORKING_IPS" ]; then
    echo "  $(wc -l < "$WORKING_IPS") verified working endpoints"
  fi
fi

if [ -s "$GOOGLE_IPS" ]; then
  echo ""
  echo "Google Domain Fronting:"
  echo "  $(awk '{print $1}' "$GOOGLE_IPS" | sort -u | wc -l) unique 216.239.x.x IPs"
  echo "  $(awk '{print $2}' "$GOOGLE_IPS" | sort -u | wc -l) unique SNI values"
  echo "  $(wc -l < "$GOOGLE_IPS") working IP+SNI combinations"
fi

echo ""
echo "Output files:"
echo "  $FOUND_IPS"
echo "  $WORKING_IPS"
echo "  $GOOGLE_IPS"
echo ""
echo "================================================================"
