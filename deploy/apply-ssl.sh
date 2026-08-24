#!/usr/bin/env bash
# استخراج certificate.pfx و اعمال روی همه دامنه‌های Nginx + پنل ادمین HTTPS :8003
#   sudo bash deploy/apply-ssl.sh
#
#   PFX_PASSWORD   پیش‌فرض 12345
#   ADMIN_HOST     پیش‌فرض gateway-admin.sabzevar.ir
#   BACKEND        پیش‌فرض 127.0.0.1:8002

set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-ssl.sh" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PFX_PASSWORD="${PFX_PASSWORD:-12345}"
BACKEND="${BACKEND:-127.0.0.1:8002}"
ENV_FILE="/etc/gateway-core.env"
CERT_DIR="/etc/pki/nginx"
CERT_FILE="${CERT_DIR}/fullchain.pem"
KEY_FILE="${CERT_DIR}/privkey.pem"
NGINX_MANAGED="/etc/nginx/conf.d/gateway-managed.conf"
NGINX_ADMIN="/etc/nginx/conf.d/gateway-admin-8003.conf"
BIN_PATH="/usr/local/bin/gateway-core"

if [[ -z "${ADMIN_HOST:-}" && -f "${ENV_FILE}" ]]; then
  ADMIN_HOST="$(awk -F= '/^GATEWAY_ADMIN_HOST=/{print $2; exit}' "${ENV_FILE}" | tr -d '\r')"
fi
ADMIN_HOST="${ADMIN_HOST:-gateway-admin.sabzevar.ir}"

find_pfx() {
  local c
  for c in \
    "${REPO_ROOT}/ssl/certificate.pfx" \
    "${PWD}/ssl/certificate.pfx" \
    "/root/gateway-core/ssl/certificate.pfx" \
    "/opt/gateway-core/ssl/certificate.pfx" \
    "/etc/gateway-core/ssl/certificate.pfx"
  do
    if [[ -f "$c" ]]; then
      echo "$c"
      return 0
    fi
  done
  return 1
}

extract_pfx() {
  local pfx="$1" tmp
  tmp="$(mktemp -d)"
  chmod 700 "$tmp"
  local ok=0
  local extra=()
  for extra in "" "-legacy"; do
    if openssl pkcs12 -in "$pfx" -passin "pass:${PFX_PASSWORD}" ${extra} -nocerts -nodes -out "${tmp}/key.pem" 2>/dev/null \
      && openssl pkcs12 -in "$pfx" -passin "pass:${PFX_PASSWORD}" ${extra} -nokeys -clcerts -out "${tmp}/cert.pem" 2>/dev/null; then
      ok=1
      openssl pkcs12 -in "$pfx" -passin "pass:${PFX_PASSWORD}" ${extra} -nokeys -cacerts -out "${tmp}/ca.pem" 2>/dev/null || true
      break
    fi
  done
  if [[ "$ok" -ne 1 ]]; then
    rm -rf "$tmp"
    echo "استخراج PFX شکست خورد. رمز یا فایل را چک کنید." >&2
    exit 1
  fi
  mkdir -p "${CERT_DIR}"
  if [[ -s "${tmp}/ca.pem" ]] && grep -q "BEGIN CERTIFICATE" "${tmp}/ca.pem"; then
    cat "${tmp}/cert.pem" "${tmp}/ca.pem" > "${CERT_FILE}"
  else
    cp "${tmp}/cert.pem" "${CERT_FILE}"
  fi
  cp "${tmp}/key.pem" "${KEY_FILE}"
  chmod 640 "${CERT_FILE}" "${KEY_FILE}"
  chown root:nginx "${CERT_FILE}" "${KEY_FILE}" 2>/dev/null || chown root:root "${CERT_FILE}" "${KEY_FILE}"
  restorecon -v "${CERT_FILE}" "${KEY_FILE}" >/dev/null 2>&1 || true
  openssl x509 -in "${CERT_FILE}" -noout -subject -dates
  rm -rf "$tmp"
}

strip_listen_8003() {
  local path="$1"
  [[ -f "$path" ]] || return 0
  python3 - "$path" <<'PY'
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
    k = start + (m.end() - m.start())
    depth = 1
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
}

echo "==> پیدا کردن certificate.pfx"
PFX="$(find_pfx || true)"
if [[ -z "${PFX}" ]]; then
  echo "فایل ssl/certificate.pfx پیدا نشد. آن را در ${REPO_ROOT}/ssl/ بگذارید." >&2
  exit 1
fi
echo "    ${PFX}"

echo "==> استخراج PEM برای Nginx"
extract_pfx "${PFX}"

echo "==> سرویس Go"
if [[ -x "${BIN_PATH}" ]]; then
  restorecon -v "${BIN_PATH}" >/dev/null 2>&1 || chcon -t bin_t "${BIN_PATH}" >/dev/null 2>&1 || true
fi
systemctl enable gateway-core >/dev/null 2>&1 || true
systemctl restart gateway-core || true
sleep 1

echo "==> Nginx: همه دامنه‌ها + ادمین :8003 با همین گواهی"
strip_listen_8003 "${NGINX_MANAGED}"
rm -f /etc/nginx/conf.d/gateway-admin.conf
if [[ -f "${NGINX_MANAGED}" ]]; then
  sed -i "s#ssl_certificate     .*#ssl_certificate     ${CERT_FILE};#g" "${NGINX_MANAGED}" || true
  sed -i "s#ssl_certificate_key .*#ssl_certificate_key ${KEY_FILE};#g" "${NGINX_MANAGED}" || true
  sed -i 's/127\.0\.0\.1:8080/127.0.0.1:8002/g' "${NGINX_MANAGED}" || true
fi

cat > "${NGINX_ADMIN}" <<EOF
# پنل ادمین HTTPS روی 8003 — گواهی مشترک PFX
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
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
}
EOF
chmod 644 "${NGINX_ADMIN}"
restorecon -v "${NGINX_ADMIN}" >/dev/null 2>&1 || true

echo "==> فایروال / SELinux"
firewall-cmd --permanent --add-port=8002/tcp >/dev/null 2>&1 || true
firewall-cmd --permanent --add-service=http >/dev/null 2>&1 || true
firewall-cmd --permanent --add-service=https >/dev/null 2>&1 || true
firewall-cmd --permanent --add-port=8003/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true
semanage port -a -t http_port_t -p tcp 8003 2>/dev/null || semanage port -m -t http_port_t -p tcp 8003 2>/dev/null || true

echo "==> nginx -t && reload"
nginx -t
systemctl reload nginx

echo
echo "انجام شد. گواهی برای همه serverهای ssl در ${NGINX_MANAGED} و پنل ادمین اعمال شد."
echo "  پنل: https://${ADMIN_HOST}:8003/"
echo "  گواهی: ${CERT_FILE}"
ss -lntp | grep -E ':8002|:8003|:443' || true
echo
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "admin 8003: %{http_code}\n" -H "Host: ${ADMIN_HOST}" "https://127.0.0.1:8003/" || true
