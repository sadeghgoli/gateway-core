# کاتالوگ مسیریابی Gateway Core

الگوی دامنه/مسیر به سرویس مقصد. جزئیات عملیاتی در پنل مدیریت قابل تغییر است.

## جدول

| دامنه گیت‌وی | الگوی path | سرویس مقصد | پارامتر / هدر | نمونه درخواست | پاسخ انتظاری |
|---|---|---|---|---|---|
| `map-gateway.sabzevar.ir` | `/*` | `https://geo.sabzevar.ir` | `X-Forwarded-Gateway=map` — path و query بدون تغییر | پایین | همان پاسخ geo |
| `apisrv-gatewaylogin.sabzevar.ir` | `/*` | `https://apisrv.sabzevar.ir` (پنل) | Cookie و Authorization فوروارد؛ Location به دامنه گیت‌وی | پایین | پاسخ لاگین بدون افشای URL داخلی |
| `gateway-admin.sabzevar.ir` | `/` | خود gateway-core | نشست ادمین | مرورگر | پنل فارسی |
| `apisrv-gateway137.sabzevar.ir` | `/*` (آینده) | از پنل | — | — | — |

نسخه اکسل: [routing-catalog.xlsx](routing-catalog.xlsx)

## نمونه نقشه

```bash
curl -sI https://map-gateway.sabzevar.ir/
# معادل منطقی:
# GET https://geo.sabzevar.ir/
# Header: Host: geo.sabzevar.ir
# Header: X-Forwarded-Host: map-gateway.sabzevar.ir
# Header: X-Forwarded-Gateway: map-gateway.sabzevar.ir
```

نمونه کاشی (اگر geo چنین مسیری داشته باشد):

```bash
curl -sI "https://map-gateway.sabzevar.ir/tiles/1/2/3.png"
```

تست روی خود سرور بدون DNS:

```bash
curl -sI -H "Host: map-gateway.sabzevar.ir" http://127.0.0.1:8002/
```

## نمونه لاگین

```bash
curl -si https://apisrv-gatewaylogin.sabzevar.ir/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"demo","password":"***"}'
```

رمز در لاگ گیت‌وی نوشته نمی‌شود (`sensitive=true`). اگر بک‌اند `Location: https://apisrv.sabzevar.ir/...` بدهد، گیت‌وی آن را به `https://apisrv-gatewaylogin.sabzevar.ir/...` برمی‌گرداند.

آدرس واقعی سرویس لاگین را در پنل، بخش Upstream، عوض کنید.

## پورت محلی

در پنل، نوع مقصد را `پورت محلی` بگذارید؛ مثلاً دامنه `apisrv-gateway137.sabzevar.ir` به `http://127.0.0.1:13700`. Nginx از پنل پورت ۸۰/۴۴۳ و گواهی را کنترل می‌کند.


1. DNS دامنه (مثلاً `apisrv-gateway137.sabzevar.ir`) روی سرور اصلی.
2. اگر گواهی wildcard `*.sabzevar.ir` دارید، Nginx نیاز به تغییر ندارد.
3. در پنل: نام، Host، یک یا چند Upstream، مسیر `/` ، سقف همزمانی و صف.
