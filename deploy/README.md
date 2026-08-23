# استقرار روی AlmaLinux

از ریشهٔ ریپو روی سرور:

```bash
sudo bash deploy/install-almalinux.sh
```

اگر پنل ادمین باید روی HTTP پورت ۸۰۰۳ باشد (بدون SSL):

```bash
sudo bash deploy/apply-admin-http-8003.sh
```

بعد: `http://gateway-admin.sabzevar.ir:8003/`

اسکریپت بسته‌ها، کاربر `gateway`، بیلد باینری، systemd، Nginx، گواهی (اگر نبود خودامضا)، فایروال، SELinux و sudoers ریلود Nginx را یکجا انجام می‌دهد.

اختیاری:

```bash
sudo ADMIN_PASSWORD='رمز-قوی' ADMIN_HOST=gateway-admin.sabzevar.ir bash deploy/install-almalinux.sh
sudo SKIP_BUILD=1 bash deploy/install-almalinux.sh   # اگر باینری از قبل نصب است
```

اگر `go build` خطای `403 Forbidden` از `proxy.golang.org` داد، اسکریپت از آینه `goproxy.cn` استفاده می‌کند. دستی:

```bash
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=off
export GOTOOLCHAIN=local
```

---

نصب دستی (اگر اسکریپت نمی‌خواهید):

1. کاربر و مسیر:

```bash
sudo useradd -r -s /sbin/nologin gateway
sudo mkdir -p /var/lib/gateway-core
sudo chown gateway:gateway /var/lib/gateway-core
```

2. باینری:

```bash
go build -o gateway-core ./cmd/gateway
sudo install -m 0755 gateway-core /usr/local/bin/gateway-core
```

3. systemd:

```bash
sudo cp deploy/systemd/gateway-core.service /etc/systemd/system/
sudo cp deploy/systemd/gateway-core.env.example /etc/gateway-core.env
sudo chmod 600 /etc/gateway-core.env
# رمز ادمین را در /etc/gateway-core.env عوض کنید
sudo systemctl daemon-reload
sudo systemctl enable --now gateway-core
```

4. Nginx:

```bash
sudo cp deploy/nginx/gateway.conf /etc/nginx/conf.d/gateway.conf
# مسیر گواهی را درست کنید؛ ترجیحاً wildcard *.sabzevar.ir
sudo nginx -t && sudo systemctl reload nginx
```

5. فایروال و SELinux:

```bash
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --permanent --add-service=https
sudo firewall-cmd --reload
sudo setsebool -P httpd_can_network_connect 1
```

## پنل گراف و Nginx

از پنل می‌توانید:

- دامنه عمومی را به **پورت محلی** (`127.0.0.1:PORT`) یا دامنه/URL دیگر وصل کنید
- پورت listen، گواهی، timeout و WebSocket را برای Nginx تنظیم و با «اعمال روی سرور» reload کنید
- روی بوم (شبیه n8n) ببینید کدام دامنه به کدام سرویس وصل است؛ گره قرمز یعنی سرویس قطع است

برای مدیریت Nginx توسط کاربر `gateway`:

```
gateway ALL=(root) NOPASSWD: /usr/sbin/nginx -t, /usr/sbin/nginx -s reload
```

و در پنل دستور reload را `sudo nginx -s reload` بگذارید. اگر هنوز conf دستی می‌خواهید، دستور تست/reload را `none` بگذارید؛ فقط فایل نوشته می‌شود.

