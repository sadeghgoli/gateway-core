#!/usr/bin/env bash
# پنل ادمین را دوباره روی HTTPS پورت 8003 می‌آورد (همان حالت قبلی).
#   sudo bash deploy/apply-admin-8003.sh
#
# آدرس: https://gateway-admin.sabzevar.ir:8003/
set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-admin-8003.sh" >&2
  exit 1
fi

ENV_FILE="/etc/gateway-core.env"
BACKEND="${BACKEND:-127.0.0.1:8002}"
NGINX_ADMIN="/etc/nginx/conf.d/gateway-admin-8003.conf"
NGINX_ADMIN_443="/etc/nginx/conf.d/gateway-admin.conf"

if [[ -z "${CERT_FILE:-}" ]]; then
  for d in /etc/pki/nginx /etc/pki/nginx /etc/nginx/ssl; do
    if [[ -f "$d/fullchain.pem" && -f "$d/privkey.pem" ]]; then
      CERT_FILE="$d/fullchain.pem"
      KEY_FILE="$d/privkey.pem"
      break
    fi
  done
fi
CERT_FILE="${CERT_FILE:-/etc/pki/nginx/fullchain.pem}"
KEY_FILE="${KEY_FILE:-/etc/pki/nginx/privkey.pem}"

if [[ -z "${ADMIN_HOST:-}" && -f "${ENV_FILE}" ]]; then
  ADMIN_HOST="$(awk -F= '/^GATEWAY_ADMIN_HOST=/{print $2; exit}' "${ENV_FILE}" | tr -d '\r')"
fi
ADMIN_HOST="${ADMIN_HOST:-gateway-admin.sabzevar.ir}"

if [[ ! -f "${CERT_FILE}" || ! -f "${KEY_FILE}" ]]; then
  echo "گواهی پیدا نشد: ${CERT_FILE}" >&2
  exit 1
fi

echo "==> دامنه پنل: ${ADMIN_HOST}:8003"

systemctl enable gateway-core >/dev/null 2>&1 || true
systemctl restart gateway-core || true
sleep 1

rm -f "${NGINX_ADMIN_443}"

echo "==> برداشتن ${ADMIN_HOST} از serverهای 443 دیگر"
python3 - "${ADMIN_HOST}" <<'PY'
import glob, re, sys
host = sys.argv[1]
pat = re.compile(r'(server_name\s+)([^;]+);')
for path in glob.glob("/etc/nginx/conf.d/*.conf"):
    if path.endswith("gateway-admin-8003.conf"):
        continue
    text = open(path, encoding="utf-8", errors="replace").read()
    def repl(m):
        names = [n for n in m.group(2).split() if n != host]
        if names == m.group(2).split():
            return m.group(0)
        if not names:
            return "server_name _;"
        return m.group(1) + " ".join(names) + ";"
    new = pat.sub(repl, text)
    if new != text:
        open(path, "w", encoding="utf-8").write(new)
        print("    پچ شد:", path)
PY

# فقط ریدایرکت HTTP ادمین را به :8003 برگردان (نه همه دامنه‌ها)
python3 - "${ADMIN_HOST}" <<'PY'
import glob, re, sys
host = sys.argv[1]
for path in glob.glob("/etc/nginx/conf.d/*.conf"):
    if path.endswith("gateway-admin-8003.conf"):
        continue
    text = open(path, encoding="utf-8", errors="replace").read()
    orig = text
    # server بلاک‌هایی که فقط همین admin host را دارند و listen 80 هستند
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
        if re.search(r"listen\s+.+\b80\b", block) and re.search(r"server_name\s+" + re.escape(host) + r"\s*;", block):
            block = re.sub(r"return\s+301\s+https://\$host\$request_uri;", "return 301 https://$host:8003$request_uri;", block)
        out.append(block)
        i = k
    new = "".join(out)
    if new != orig:
        open(path, "w", encoding="utf-8").write(new)
PY

cat > "${NGINX_ADMIN}" <<EOF
# پنل ادمین HTTPS روی 8003
server {
    listen 80;
    listen [::]:80;
    server_name ${ADMIN_HOST};
    return 301 https://\$host:8003\$request_uri;
}
server {
    listen 8003 ssl;
    listen [::]:8003 ssl;
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
        proxy_set_header Connection "";
        proxy_connect_timeout 5s;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
}
EOF
chmod 644 "${NGINX_ADMIN}"
restorecon -v "${NGINX_ADMIN}" >/dev/null 2>&1 || true

echo "==> فایروال / SELinux پورت 8003"
firewall-cmd --permanent --add-port=8002/tcp >/dev/null 2>&1 || true
firewall-cmd --permanent --add-port=8003/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true
semanage port -a -t http_port_t -p tcp 8003 2>/dev/null || semanage port -m -t http_port_t -p tcp 8003 2>/dev/null || true
semanage port -a -t http_port_t -p tcp 8002 2>/dev/null || semanage port -m -t http_port_t -p tcp 8002 2>/dev/null || true

echo "==> nginx -t && reload"
nginx -t
systemctl reload nginx

echo
curl -sS --noproxy '*' --max-time 5 -o /dev/null -w "Go 8002: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "http://${BACKEND}/" || true
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "Nginx 8003: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "https://127.0.0.1:8003/" || true
echo
echo "پنل: https://${ADMIN_HOST}:8003/"
ss -lntp | grep -E ':8003|:8002|:443' || true
