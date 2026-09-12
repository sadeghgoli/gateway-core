#!/usr/bin/env bash
# تأیید روتینگ Host روی گیت‌وی (مدل 443 مشترک).
#   bash deploy/verify-shared-443.sh
# روی سرور گیت‌وی اجرا شود.
set -euo pipefail

BACKEND="${BACKEND:-127.0.0.1:8002}"
FAIL=0

check() {
  local host="$1" path="${2:-/}" expect_not="${3:-000}"
  local code
  code=$(curl -sS --noproxy '*' --max-time 8 -o /dev/null -w "%{http_code}" -H "Host: ${host}" "http://${BACKEND}${path}" || echo "000")
  if [[ "$code" == "$expect_not" ]]; then
    echo "FAIL  Host=${host} path=${path} code=${code}"
    FAIL=1
  else
    echo "OK    Host=${host} path=${path} code=${code}"
  fi
}

echo "==> Go :8002 (Host-based)"
check "map-gateway.sabzevar.ir" "/api/v1/health"
check "apisrv-gatewaylogin.sabzevar.ir" "/"
check "apisrv-gateway137.sabzevar.ir" "/"
check "gateway-admin.sabzevar.ir" "/"

if command -v nginx >/dev/null 2>&1 && ss -lnt 2>/dev/null | grep -q ':443'; then
  echo "==> Nginx :443"
  for host in map-gateway.sabzevar.ir apisrv-gatewaylogin.sabzevar.ir gateway-admin.sabzevar.ir; do
    code=$(curl -skS --noproxy '*' --max-time 8 -o /dev/null -w "%{http_code}" -H "Host: ${host}" "https://127.0.0.1/" || echo "000")
    echo "  https://${host}/ → ${code}"
  done
else
  echo "==> Nginx :443 در دسترس نیست (رد شدن تست HTTPS محلی)"
fi

if [[ "$FAIL" -ne 0 ]]; then
  echo "بعضی چک‌ها شکست خوردند." >&2
  exit 1
fi
echo "همه چک‌های Host روی Go OK."
