# راه‌اندازی کلاینت Bale Transport

دو روش برای اتصال وجود دارد:

- **باینری مستقل**: بدون نیاز به تغییر در SingBox. با هر کلاینت پروکسی کار می‌کند.
- **ترنسپورت بومی SingBox**: نیاز به بیلد سفارشی SingBox دارد. عملکرد بهتر.

## روش ۱: باینری مستقل

### ساخت باینری

روی یک سیستم با Go نصب‌شده:

```bash
git clone https://github.com/projectmithra/bale-transport.git
cd bale-transport
go build -o bale-transport ./cmd/bale-transport/
```

برای ویندوز:

```bash
GOOS=windows GOARCH=amd64 go build -o bale-transport.exe ./cmd/bale-transport/
```

برای اندروید:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bale-transport-arm64 ./cmd/bale-transport/
```

### اجرا

```bash
./bale-transport -worker wss://your-worker.example.com/w -v
```

پارامترهای خط فرمان:

| پارامتر | پیش‌فرض | توضیح |
|---------|---------|-------|
| `-worker` | (الزامی) | آدرس WebSocket ورکر Cloudflare |
| `-listen` | `127.0.0.1:1984` | آدرس و پورت محلی |
| `-host` | (از URL) | هدر Host سفارشی |
| `-origin` | `https://web.bale.ai` | هدر Origin |
| `-ping` | `25000` | فاصله keepalive (میلی‌ثانیه) |
| `-jitter` | `3000` | نوسان زمانی keepalive (میلی‌ثانیه) |
| `-v` | غیرفعال | لاگ‌های جزئی |

### تنظیم کلاینت پروکسی

بعد از اجرای باینری، کلاینت پروکسی خود را به `127.0.0.1:1984` متصل کنید.

نمونه تنظیم SingBox (فایل `config.json`):

```json
{
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "0.0.0.0",
      "listen_port": 2080
    }
  ],
  "outbounds": [
    {
      "type": "vless",
      "tag": "proxy",
      "server": "127.0.0.1",
      "server_port": 1984,
      "uuid": "UUID-سرور-شما",
      "tls": { "enabled": false }
    },
    { "type": "direct", "tag": "direct" }
  ],
  "route": {
    "rules": [
      { "geosite": ["ir"], "outbound": "direct" },
      { "geoip": ["ir", "private"], "outbound": "direct" }
    ],
    "final": "proxy"
  }
}
```

سپس پروکسی گوشی یا مرورگر را به آدرس `IP-کامپیوتر:2080` تنظیم کنید.

## روش ۲: ترنسپورت بومی SingBox

این روش نیاز به بیلد سفارشی SingBox با پچ‌های Bale دارد. عملکرد بهتری نسبت به باینری مستقل دارد چون یک اتصال کمتر وجود دارد.

### بیلد SingBox با پشتیبانی Bale

```bash
# دانلود hiddify-sing-box
git clone --depth 1 --branch extended https://github.com/hiddify/hiddify-sing-box.git
cd hiddify-sing-box

# اعمال پچ‌ها
# فایل‌های زیر را از مخزن bale-transport کپی کنید:

# ۱. ثابت ترنسپورت
cp ~/bale-transport/patches/patched-v2ray-constant.go constant/v2ray.go

# ۲. تعریف آپشن‌ها
cp ~/bale-transport/patches/v2ray_bale.go option/v2ray_bale.go

# ۳. ثبت ترنسپورت
cp ~/bale-transport/patches/patched-transport.go transport/v2ray/transport.go

# ۴. پکیج ترنسپورت
cp -r ~/bale-transport/transport/v2raybale/ transport/v2raybale/

# بیلد
go build -ldflags="-s -w" -tags "with_gvisor,with_quic,with_utls,with_ech" -o sing-box-bale ./cmd/sing-box
```

### تنظیم بومی

با بیلد سفارشی، ترنسپورت `bale` مستقیما در تنظیمات SingBox قابل استفاده است:

```json
{
  "outbounds": [
    {
      "type": "vless",
      "tag": "proxy",
      "server": "آدرس-سرور-شما",
      "server_port": 443,
      "uuid": "UUID-سرور-شما",
      "tls": {
        "enabled": true,
        "server_name": "آدرس-سرور-شما",
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      },
      "transport": {
        "type": "bale",
        "path": "/w",
        "origin": "https://web.bale.ai"
      }
    }
  ]
}
```

فیلدهای ترنسپورت:

| فیلد | نوع | پیش‌فرض | توضیح |
|------|-----|---------|-------|
| `worker_url` | string | (از سرور) | آدرس کامل WSS ورکر |
| `worker_host` | string | (از URL) | هدر Host سفارشی |
| `origin` | string | `https://web.bale.ai` | هدر Origin |
| `path` | string | `/w` | مسیر WebSocket |
| `accept_language` | string | `fa-IR,fa;q=0.9,...` | هدر Accept-Language |

## اتصال گوشی

بعد از اجرای کلاینت (به هر روشی):

**مرورگر:** پروکسی WiFi گوشی را به آدرس `IP-کامپیوتر:2080` تنظیم کنید.

**همه اپلیکیشن‌ها:** از V2Box (iOS) یا Hiddify (اندروید) استفاده کنید. تنظیمات SOCKS5 را به `IP-کامپیوتر:2080` وصل کنید.

## عیب‌یابی

**خطای `ws dial: dial tcp: connection refused`:**
ورکر Cloudflare در دسترس نیست. آدرس URL ورکر را بررسی کنید.

**Handshake timeout:**
اتصال به CDN برقرار می‌شود ولی سرور پاسخ نمی‌دهد. بررسی کنید که سرور bale-unwrapper اجرا باشد.

**داده عبور نمی‌کند:**
UUID کلاینت باید دقیقا با UUID تنظیم‌شده در `xray-config.json` سرور یکی باشد.
