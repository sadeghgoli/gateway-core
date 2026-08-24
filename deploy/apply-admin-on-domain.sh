#!/usr/bin/env bash
# پنل ادمین را از پورت 8003 برمی‌دارد و روی دامنهٔ استاندارد HTTPS می‌گذارد.
#   sudo bash deploy/apply-admin-on-domain.sh
#
# بعد از اجرا: https://gateway-admin.sabzevar.ir/
set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-admin-on-domain.sh" >&2
  exit 1
fi

ENV_FILE="/etc/gateway-core.env"
CERT_FILE="${CERT_FILE:-/etc/pki/nginx/fullchain.pem}"
KEY_FILE="${KEY_FILE:-/etc/pki/nginx/privkey.pem}"
BACKEND="${BACKEND:-127.0.0.1:8002}"
NGINX_MANAGED="${NGINX_MANAGED:-/etc/nginx/conf.d/gateway-managed.conf}"
NGINX_ADMIN_OLD="/etc/nginx/conf.d/gateway-admin-8003.conf"
NGINX_ADMIN="/etc/nginx/conf.d/gateway-admin.conf"
DB_PATH="${GATEWAY_DB:-/var/lib/gateway-core/gateway.db}"

if [[ -z "${ADMIN_HOST:-}" && -f "${ENV_FILE}" ]]; then
  ADMIN_HOST="$(awk -F= '/^GATEWAY_ADMIN_HOST=/{print $2; exit}' "${ENV_FILE}" | tr -d '\r')"
fi
ADMIN_HOST="${ADMIN_HOST:-gateway-admin.sabzevar.ir}"

if [[ ! -f "${CERT_FILE}" || ! -f "${KEY_FILE}" ]]; then
  echo "گواهی پیدا نشد: ${CERT_FILE} / ${KEY_FILE}" >&2
  echo "اول sudo bash deploy/apply-ssl.sh را بزنید." >&2
  exit 1
fi

echo "==> دامنه پنل: ${ADMIN_HOST}"
echo "==> حذف listen 8003"

rm -f "${NGINX_ADMIN_OLD}"
for f in /etc/nginx/conf.d/*.conf; do
  [[ -f "$f" ]] || continue
  python3 - "$f" <<'PY'
import re, sys
path = sys.argv[1]
text = open(path, encoding="utf-8", errors="replace").read()
orig = text
text = text.replace("https://$host:8003", "https://$host")
text = text.replace("https://\\$host:8003", "https://\\$host")
out, i, n = [], 0, len(text)
while i < n:
    m = re.search(r"server\s*\{", text[i:])
    if not m:
        out.append(text[i:])
        break
    start = i + m.start()
    out.append(text[i:start])
    k = start + (m.end() - m.start())
    depth = 1
    while k < n and depth:
        if text[k] == "{":
            depth += 1
        elif text[k] == "}":
            depth -= 1
        k += 1
    block = text[start:k]
    if re.search(r"listen\s+\[?::\]?:?8003\b", block) or re.search(r"listen\s+8003\b", block):
        i = k
        continue
    out.append(block)
    i = k
new = "".join(out)
if new != orig:
    open(path, "w", encoding="utf-8").write(new)
    print("    پچ شد:", path)
PY
done

echo "==> server ادمین روی 443"
cat > "${NGINX_ADMIN}" <<EOF
# پنل ادمین روی دامنه — بدون پورت
server {
    listen 80;
    listen [::]:80;
    server_name ${ADMIN_HOST};
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ${ADMIN_HOST};
    ssl_certificate     ${CERT_FILE};
    ssl_certificate_key ${KEY_FILE};
    client_max_body_size 2m;
    location / {
        proxy_pass http://${BACKEND};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
}
EOF
chmod 644 "${NGINX_ADMIN}"
restorecon -v "${NGINX_ADMIN}" >/dev/null 2>&1 || true

if command -v sqlite3 >/dev/null 2>&1 && [[ -f "${DB_PATH}" ]]; then
  echo "==> به‌روزرسانی تنظیم Nginx در دیتابیس (پورت پنل = 443)"
  sqlite3 "${DB_PATH}" "UPDATE settings SET value = json_set(value, '$.listen_admin_https', 443) WHERE key = 'nginx';" 2>/dev/null || true
fi

echo "==> فایروال: 8003 لازم نیست"
firewall-cmd --permanent --add-service=http >/dev/null 2>&1 || true
firewall-cmd --permanent --add-service=https >/dev/null 2>&1 || true
firewall-cmd --permanent --remove-port=8003/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true

echo "==> nginx -t && reload"
nginx -t
systemctl reload nginx

echo
echo "انجام شد. پنل روی دامنه است (بدون پورت):"
echo "  https://${ADMIN_HOST}/"
ss -lntp | grep -E ':443|:8002|:8003' || true
echo
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "admin 443: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "https://127.0.0.1/" || true
