#!/usr/bin/env bash
# پنل ادمین را روی HTTP پورت 8003 بدون SSL بالا می‌آورد.
#   sudo bash deploy/apply-admin-http-8003.sh
#
# متغیرها:
#   ADMIN_HOST   پیش‌فرض از /etc/gateway-core.env یا gateway-admin.sabzevar.ir
#   BACKEND      پیش‌فرض 127.0.0.1:8002

set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-admin-http-8003.sh" >&2
  exit 1
fi

ENV_FILE="/etc/gateway-core.env"
NGINX_MANAGED="/etc/nginx/conf.d/gateway-managed.conf"
NGINX_ADMIN="/etc/nginx/conf.d/gateway-admin-8003.conf"
BIN_PATH="/usr/local/bin/gateway-core"
BACKEND="${BACKEND:-127.0.0.1:8002}"

if [[ -z "${ADMIN_HOST:-}" && -f "${ENV_FILE}" ]]; then
  ADMIN_HOST="$(awk -F= '/^GATEWAY_ADMIN_HOST=/{print $2; exit}' "${ENV_FILE}" | tr -d '\r')"
fi
ADMIN_HOST="${ADMIN_HOST:-gateway-admin.sabzevar.ir}"

echo "==> باینری و سرویس"
if [[ -x "${BIN_PATH}" ]]; then
  restorecon -v "${BIN_PATH}" >/dev/null 2>&1 || chcon -t bin_t "${BIN_PATH}" >/dev/null 2>&1 || true
fi
if [[ -f "${ENV_FILE}" ]]; then
  sed -i 's/GATEWAY_LISTEN=127.0.0.1:8080/GATEWAY_LISTEN=0.0.0.0:8002/' "${ENV_FILE}" || true
  sed -i 's/GATEWAY_LISTEN=127.0.0.1:8002/GATEWAY_LISTEN=0.0.0.0:8002/' "${ENV_FILE}" || true
  sed -i 's/127.0.0.1:8080/127.0.0.1:8002/g' "${ENV_FILE}" || true
fi
systemctl daemon-reload
systemctl enable gateway-core >/dev/null 2>&1 || true
systemctl restart gateway-core
sleep 1
if ! ss -lnt | grep -q ':8002'; then
  echo "هشدار: روی 8002 چیزی listen نیست. لاگ:" >&2
  journalctl -u gateway-core -n 20 --no-pager >&2 || true
fi

echo "==> پاک کردن listen 8003 قبلی از conf مدیریت‌شده"
if [[ -f "${NGINX_MANAGED}" ]]; then
  python3 - "${NGINX_MANAGED}" <<'PY'
import re, sys
path = sys.argv[1]
text = open(path, encoding="utf-8", errors="replace").read()
out, i, n = [], 0, len(text)
while i < n:
    m = re.search(r"server\s*\{", text[i:])
    if not m:
        out.append(text[i:])
        break
    start = i + m.start()
    out.append(text[i:start])
    j = start + m.end() - i
    depth = 1
    k = start + (m.end() - m.start())
    while k < n and depth:
        if text[k] == "{":
            depth += 1
        elif text[k] == "}":
            depth -= 1
        k += 1
    block = text[start:k]
    if not re.search(r"listen\s+\[?::\]?:?8003\b", block) and not re.search(r"listen\s+8003\b", block):
        out.append(block)
    i = k
open(path, "w", encoding="utf-8").write("".join(out))
PY
  sed -i 's/127\.0\.0\.1:8080/127.0.0.1:8002/g' "${NGINX_MANAGED}" || true
fi

echo "==> نوشتن ${NGINX_ADMIN}"
cat > "${NGINX_ADMIN}" <<EOF
# HTTP پنل ادمین — بدون TLS
server {
    listen 8003;
    listen [::]:8003;
    server_name ${ADMIN_HOST};
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

echo "==> فایروال و SELinux"
firewall-cmd --permanent --add-port=8002/tcp >/dev/null 2>&1 || true
firewall-cmd --permanent --add-port=8003/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true
semanage port -a -t http_port_t -p tcp 8003 2>/dev/null || semanage port -m -t http_port_t -p tcp 8003 2>/dev/null || true

echo "==> تست و reload Nginx"
nginx -t
systemctl reload nginx

echo
echo "انجام شد."
echo "  پنل:  http://${ADMIN_HOST}:8003/"
echo "  بک‌اند: http://${BACKEND}"
ss -lntp | grep -E ':8002|:8003' || true
echo
curl -sS --noproxy '*' --max-time 5 -o /dev/null -w "لوکال 8002: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "http://${BACKEND}/" || true
curl -sS --noproxy '*' --max-time 5 -o /dev/null -w "nginx 8003: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "http://127.0.0.1:8003/" || true
