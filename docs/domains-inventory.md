# فهرست دامنه‌ها → سرور مقصد (گیت‌وی)

همه دامنه‌های عمومی باید DNS به **IP سرور گیت‌وی** داشته باشند.
فقط گیت‌وی روی **443** گوش می‌دهد؛ روتینگ بر اساس `Host` به IP داخلی هر سرویس می‌رود.

| دامنه عمومی | Upstream پیشنهادی | یادداشت |
|---|---|---|
| `map-gateway.sabzevar.ir` | `http://192.168.1.19:7003` | map-api روی سرور نقشه |
| `apisrv-gatewaylogin.sabzevar.ir` | `https://apisrv.sabzevar.ir` | لاگین؛ در پنل قابل تغییر |
| `apisrv-gateway137.sabzevar.ir` | `http://127.0.0.1:13700` | پورت محلی / سرویس ۱۳۷ |
| `gateway-admin.sabzevar.ir` | خود gateway-core | پنل ادمین (پروکسی نمی‌شود) |
| `sbzl.ir` (نمونه SSL جدا) | از پنل | گواهی جدا روی سرور گیت‌وی؛ فیلد SSL در ویرایش دامنه |

## SSL

- پیش‌فرض همه دامنه‌ها: `/etc/pki/nginx/fullchain.pem` + `privkey.pem`
- دامنه با گواهی جدا: `sudo DOMAIN=... PFX=... bash deploy/apply-domain-ssl.sh` سپس در پنل مسیرها را پر کنید (جزئیات: [ssl/README.md](../ssl/README.md))

## قالب برای دامنه جدید (از کارفرما بگیر)

برای هر سرویس جدید این سه مورد لازم است:

1. **دامنه عمومی** (مثلاً `service-x.sabzevar.ir`)
2. **IP:PORT سرور مقصد** (یا URL کامل مثل `http://192.168.x.x:PORT`)
3. آیا مسیر خاص / WebSocket / حساس بودن لاگین لازم است؟

سپس:

1. DNS A/AAAA → IP گیت‌وی
2. در پنل: Gateway + Upstream + Route `/`
3. Nginx روی 443 مشترک (حالت `shared_443`) — معمولاً با wildcard `*.sabzevar.ir` کافی است
4. فایروال فقط `http` / `https`

جزئیات مسیر و نمونه curl: [routing-catalog.md](routing-catalog.md).
