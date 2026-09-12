# Gateway Core

سرویس گیت‌وی شهرداری سبزوار: دامنهٔ عمومی را می‌گیرد، درخواست را صف‌بندی و بازنویسی می‌کند، سپس به سرویس داخلی می‌فرستد. Nginx فقط TLS است؛ منطق در باینری Go است.

## مدل کارفرما (۴۴۳ مشترک)

همه دامنه‌ها DNS → IP سرور گیت‌وی؛ فایروال فقط ۸۰/۴۴۳؛ Nginx روی ۴۴۳؛ Go با `Host` به IP هر میکروسرویس فوروارد می‌کند.

فهرست دامنه/Upstream: [docs/domains-inventory.md](docs/domains-inventory.md).

## فاز ۱

| دامنه عمومی | مقصد |
|---|---|
| `map-gateway.sabzevar.ir` | `http://<maps-host>:7003` (map-api) — عمومی روی `:443` |
| `apisrv-gatewaylogin.sabzevar.ir` | `https://apisrv.sabzevar.ir` (در پنل قابل تغییر) |
| `apisrv-gateway137.sabzevar.ir` | `http://127.0.0.1:13700` (پورت محلی) |
| `gateway-admin.sabzevar.ir` | پنل مدیریت |

کاتالوگ کامل: [docs/routing-catalog.md](docs/routing-catalog.md) و [docs/routing-catalog.xlsx](docs/routing-catalog.xlsx).

## اجرا محلی

```bash
export GATEWAY_ADMIN_PASSWORD=changeme
go run ./cmd/gateway
```

- پنل: http://IP-سرور:8002/_admin/  (کاربر `admin`)
- برای تست Host: `curl -H "Host: map-gateway.sabzevar.ir" http://127.0.0.1:8002/api/v1/health`

## متغیرهای محیطی

- `GATEWAY_LISTEN` پیش‌فرض `0.0.0.0:8002`
- `GATEWAY_DB` پیش‌فرض `./data/gateway.db`
- `GATEWAY_ADMIN_HOST` پیش‌فرض `gateway-admin.sabzevar.ir`
- `GATEWAY_ADMIN_USER` / `GATEWAY_ADMIN_PASSWORD`
- `GATEWAY_NGINX_CONF` پیش‌فرض `/etc/nginx/conf.d/gateway-managed.conf`
- `GATEWAY_NGINX_TEST` / `GATEWAY_NGINX_RELOAD` (برای رد شدن: `none`)
- `GATEWAY_NGINX_UPSTREAM` پیش‌فرض `127.0.0.1:8002`

پنل گراف اتصال دامنه→سرویس و وضعیت قطع/وصل را نشان می‌دهد. تنظیمات Nginx (حالت پیش‌فرض: **۴۴۳ مشترک**) از همان پنل قابل پیش‌نمایش و اعمال است.

## استقرار AlmaLinux

روی سرور، از ریشه ریپو:

```bash
sudo bash deploy/install-almalinux.sh
sudo bash deploy/apply-shared-443.sh
bash deploy/verify-shared-443.sh
```

اگر `go build` روی سرور از `proxy.golang.org` خطای 403 گرفت، اسکریپت به‌صورت پیش‌فرض از آینه (`goproxy.cn`) استفاده می‌کند. دستی:

```bash
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=off
export GOTOOLCHAIN=local
```

جزئیات و نصب دستی: [deploy/README.md](deploy/README.md).

گیت‌وی نقشه (آپ‌ستریم map-api) — روی ۴۴۳ مشترک، آپ‌ستریم را در پنل تنظیم کنید. اسکریپت پورت‌جدا قدیمی: `deploy/apply-map-8004.sh`.
