# گواهی TLS

## پیش‌فرض (همه دامنه‌ها)

فایل `certificate.pfx` را اینجا بگذارید. رمز پیش‌فرض اسکریپت نصب `12345` است (با `PFX_PASSWORD` قابل تغییر).

اسکریپت آن را به `/etc/pki/nginx/fullchain.pem` و `privkey.pem` تبدیل می‌کند و برای **همه دامنه‌هایی که در پنل گواهی جدا نگذاشته‌اند** استفاده می‌شود.

```bash
sudo bash deploy/apply-ssl.sh
```

در پنل → کنترل Nginx → «گواهی پیش‌فرض» / «کلید پیش‌فرض».

## گواهی جدا per-domain (مثل sbzl.ir)

SSL روی **همان سرور گیت‌وی** نصب می‌شود (Nginx با SNI روی پورت ۴۴۳ انتخاب می‌کند). بک‌اند نیازی به گواهی جدا ندارد.

1. فایل‌ها را روی سرور کپی کنید (مثلاً `sbzl.ir-cert.pfx` یا PEM + KEY).
2. نصب:

```bash
# از PFX
sudo DOMAIN=sbzl.ir PFX=/root/sbzl.ir-cert.pfx PFX_PASSWORD='رمز-pfx' bash deploy/apply-domain-ssl.sh

# یا از PEM (اگر کلید جدا دارید)
sudo DOMAIN=sbzl.ir CERT_PEM=/root/sbzl.ir-cert.pem KEY_PEM=/root/sbzl.ir-key.pem bash deploy/apply-domain-ssl.sh
```

خروجی:

- `/etc/pki/nginx/sbzl.ir/fullchain.pem`
- `/etc/pki/nginx/sbzl.ir/privkey.pem`

3. در پنل، دامنه `sbzl.ir` را ویرایش کنید:
   - گواهی: `/etc/pki/nginx/sbzl.ir/fullchain.pem`
   - کلید: `/etc/pki/nginx/sbzl.ir/privkey.pem`
   - بقیه دامنه‌ها را **خالی** بگذارید تا همان گواهی پیش‌فرض بماند.
4. Nginx → «اعمال روی سرور».

توجه: فایل `sbzl.ir-cert.pem` معمولاً فقط زنجیره گواهی است؛ برای کلید خصوصی از `.pfx` با `PFX_PASSWORD` استفاده کنید مگر فروشنده کلید جدا داده باشد.
