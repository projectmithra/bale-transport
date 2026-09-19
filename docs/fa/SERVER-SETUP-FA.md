# راه‌اندازی سرور Bale Transport

## پیش‌نیازها

- یک سرور لینوکس (Ubuntu 22.04 یا Debian 12) با دسترسی root
- Docker و Docker Compose نصب شده
- یک دامنه متصل به Cloudflare (پلن رایگان کافی است)
- دامنه باید از طریق پروکسی Cloudflare (ابر نارنجی) عبور کند

## ۱. نصب Docker

اگر Docker نصب نیست:

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
```

از سیستم خارج شوید و دوباره وارد شوید تا تغییرات گروه اعمال شود.

تست نصب:

```bash
docker --version
docker compose version
```

## ۲. دانلود پروژه

```bash
git clone https://github.com/projectmithra/bale-transport.git
cd bale-transport/server/docker
```

## ۳. تنظیم UUID

یک UUID تصادفی بسازید:

```bash
cat /proc/sys/kernel/random/uuid
```

خروجی چیزی شبیه این خواهد بود:

```
a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

این UUID را در فایل `xray-config.json` جایگزین کنید:

```bash
nano xray-config.json
```

بخش `YOUR-UUID-HERE` را با UUID خودتان عوض کنید:

```json
{
  "inbounds": [
    {
      "port": 8443,
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
            "level": 0
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "ws",
        "wsSettings": {
          "path": "/api/v4/sync/data-stream"
        }
      }
    }
  ],
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    }
  ]
}
```

ذخیره کنید و خارج شوید (در nano: `Ctrl+O` سپس `Ctrl+X`).

## ۴. اجرای سرور

```bash
docker compose up -d
```

این دستور دو سرویس را اجرا می‌کند:

- **bale-unwrapper** روی پورت 80: فریم‌های پروتوباف بله را باز می‌کند
- **xray** روی پورت 8443 (فقط داخلی): سرور پروکسی VLESS

بررسی وضعیت:

```bash
docker compose ps
docker compose logs -f unwrapper
```

## ۵. تنظیم Cloudflare Worker

یک Cloudflare Worker بسازید که ترافیک WebSocket را به سرور شما هدایت کند. Worker به پورت 80 سرور شما متصل می‌شود.

برای کد Worker به مخزن [`cloudflare-worker`](https://github.com/projectmithra/cloudflare-worker) مراجعه کنید.

## ۶. تست اتصال

از روی سرور:

```bash
curl -s http://localhost:80/
```

خروجی باید شبیه پاسخ واقعی سرور بله باشد:

```json
{"ok":true,"result":{"version":"5.4.2","apiVersion":1,"mkprotoVersion":1,"serverTime":...}}
```

## ۷. تنظیمات اختیاری

متغیرهای محیطی در `docker-compose.yml` قابل تنظیم هستند:

| متغیر | پیش‌فرض | توضیح |
|--------|---------|-------|
| `BACKEND` | `ws://xray:8443` | آدرس سرور پروکسی داخلی |
| `BACKEND_PATH` | `/api/v4/sync/data-stream` | مسیر WebSocket |
| `VERBOSE` | `0` | لاگ‌های جزئی (`1` برای فعال) |
| `IDLE_TIMEOUT_MS` | `120000` | مهلت بی‌فعالیتی (میلی‌ثانیه) |
| `MAX_PAYLOAD_SIZE` | `4194304` | حداکثر اندازه بسته (بایت) |

## ۸. به‌روزرسانی

```bash
cd bale-transport/server/docker
docker compose pull
docker compose up -d --build
```

## ۹. مشاهده لاگ‌ها

```bash
# لاگ‌های unwrapper
docker compose logs -f unwrapper

# لاگ‌های xray
docker compose logs -f xray

# هر دو
docker compose logs -f
```

## ۱۰. توقف سرویس

```bash
docker compose down
```

## عیب‌یابی

**unwrapper بالا نمی‌آید:**
```bash
docker compose logs unwrapper
```
معمولا مشکل از دسترسی به پورت 80 است. مطمئن شوید nginx یا سرویس دیگری روی پورت 80 اجرا نیست.

**اتصال WebSocket برقرار نمی‌شود:**
بررسی کنید که Cloudflare Worker درست تنظیم شده و به IP سرور اشاره می‌کند.

**xray لاگ خطا می‌دهد:**
UUID در `xray-config.json` باید دقیقا با UUID تنظیم‌شده در کلاینت یکی باشد.
