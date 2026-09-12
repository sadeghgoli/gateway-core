#!/usr/bin/env bash
# همه دامنه‌ها روی 443 مشترک (مدل کارفرما: فایروال → گیت‌وی → سرور مقصد).
#   sudo bash deploy/apply-shared-443.sh
#
# DNS هر دامنه باید به IP همین سرور باشد. روتینگ Host در gateway-core است.
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-shared-443.sh" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND="${BACKEND:-127.0.0.1:8002}"
NGINX_SHARED="/etc/nginx/conf.d/gateway.conf"
NGINX_MANAGED="${GATEWAY_NGINX_CONF:-/etc/nginx/conf.d/gateway-managed.conf}"
SSL_CERT="${SSL_CERT:-/etc/pki/nginx/fullchain.pem}"
SSL_KEY="${SSL_KEY:-/etc/pki/nginx/privkey.pem}"

echo "==> نصب conf مشترک 443"
install -m 0644 "${ROOT}/deploy/nginx/gateway.conf" "${NGINX_SHARED}"

# هر conf دیگری که upstream gateway_core دارد با gateway.conf تداخل می‌کند
if [[ -f "${NGINX_MANAGED}" ]]; then
  echo "==> آرشیو conf مدیریت‌شده: ${NGINX_MANAGED}.shared443.bak"
  mv -f "${NGINX_MANAGED}" "${NGINX_MANAGED}.shared443.bak"
fi

# confهای اختصاصی قدیمی / تکراری
for f in /etc/nginx/conf.d/map-gateway-8004.conf /etc/nginx/conf.d/gateway-admin-8003.conf; do
  if [[ -f "$f" ]]; then
    echo "==> آرشیو $f"
    mv -f "$f" "${f}.bak"
  fi
done

# اگر فایل دیگری در conf.d هنوز upstream gateway_core دارد، هشدار بده
dup=$(grep -l 'upstream gateway_core' /etc/nginx/conf.d/*.conf 2>/dev/null | grep -v "${NGINX_SHARED}" || true)
if [[ -n "${dup}" ]]; then
  echo "هشدار: upstream تکراری در:" >&2
  echo "${dup}" >&2
  echo "این فایل‌ها را دستی آرشیو کنید و دوباره اجرا کنید." >&2
  exit 1
fi

if [[ -f "${SSL_CERT}" && -f "${SSL_KEY}" ]]; then
  # اطمینان از فعال بودن خطوط گواهی در conf کپی‌شده
  sed -i \
    -e 's|^[[:space:]]*#[[:space:]]*ssl_certificate[[:space:]]\+/etc/pki/nginx/fullchain.pem|    ssl_certificate     /etc/pki/nginx/fullchain.pem|' \
    -e 's|^[[:space:]]*#[[:space:]]*ssl_certificate_key[[:space:]]\+/etc/pki/nginx/privkey.pem|    ssl_certificate_key /etc/pki/nginx/privkey.pem|' \
    "${NGINX_SHARED}" 2>/dev/null || true
else
  echo "هشدار: گواهی در ${SSL_CERT} نیست؛ از ssl/README.md یا apply-ssl.sh استفاده کنید." >&2
fi

echo "==> فایروال فقط http/https (بدون پورت دامنه جدا)"
firewall-cmd --permanent --add-service=http >/dev/null 2>&1 || true
firewall-cmd --permanent --add-service=https >/dev/null 2>&1 || true
firewall-cmd --permanent --remove-port=8003/tcp >/dev/null 2>&1 || true
firewall-cmd --permanent --remove-port=8004/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true

setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true

echo "==> nginx -t && reload"
nginx -t
systemctl reload nginx

echo "==> بالا آوردن gateway-core"
systemctl enable --now gateway-core >/dev/null 2>&1 || true
systemctl restart gateway-core >/dev/null 2>&1 || true

echo "==> انتظار برای listen روی ${BACKEND}"
ready=0
for i in $(seq 1 30); do
  if curl -sS --noproxy '*' --max-time 1 -o /dev/null "http://${BACKEND}/" -H "Host: gateway-admin.sabzevar.ir" 2>/dev/null; then
    ready=1
    break
  fi
  # connect refused / هنوز بالا نیامده
  if ss -lnt 2>/dev/null | grep -q ':8002'; then
    # پورت باز است ولی هنوز پاسخ نمی‌دهد
    :
  fi
  sleep 0.5
done
if [[ "$ready" -ne 1 ]]; then
  echo "هشدار: gateway-core روی ${BACKEND} آماده نشد. وضعیت:" >&2
  systemctl --no-pager --full status gateway-core || true
  journalctl -u gateway-core -n 30 --no-pager || true
fi

echo "==> تست Host روی ${BACKEND}"
for host in map-gateway.sabzevar.ir apisrv-gatewaylogin.sabzevar.ir apisrv-gateway137.sabzevar.ir gateway-admin.sabzevar.ir; do
  code=$(curl -sS --noproxy '*' --max-time 5 -o /dev/null -w "%{http_code}" -H "Host: ${host}" "http://${BACKEND}/" || echo "000")
  echo "  ${host} → HTTP ${code}"
done

echo
echo "آماده. آدرس عمومی (بدون پورت اضافه):"
echo "  https://map-gateway.sabzevar.ir/"
echo "  https://apisrv-gatewaylogin.sabzevar.ir/"
echo "  https://apisrv-gateway137.sabzevar.ir/"
echo "  https://gateway-admin.sabzevar.ir/"
echo "تأیید: bash deploy/verify-shared-443.sh"
