#!/usr/bin/env bash
# Run on the gateway-core host after apply-map-8004.sh
#   KEY=pk_... bash deploy/verify-map-8004.sh
set -euo pipefail

HOST="${MAP_HOST:-map-gateway.sabzevar.ir}"
PORT="${MAP_PORT:-8004}"
KEY="${KEY:-}"
SCHEME="${SCHEME:-https}"

echo "==> Go :8002"
curl -sS --noproxy '*' --max-time 8 -o /dev/null -w "health %{http_code}\n" \
  -H "Host: ${HOST}" "http://127.0.0.1:8002/api/v1/health"

echo "==> Nginx :${PORT}"
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "health %{http_code}\n" \
  -H "Host: ${HOST}" "${SCHEME}://127.0.0.1:${PORT}/api/v1/health"

if [[ -z "${KEY}" ]]; then
  echo "Set KEY=pk_... to test style.json"
  exit 0
fi

code="$(curl -k -sS --noproxy '*' --max-time 20 -o /dev/null -w "%{http_code}" \
  -H "Host: ${HOST}" "${SCHEME}://127.0.0.1:${PORT}/styles/style.json?key=${KEY}")"
echo "==> style.json HTTP ${code}"
[[ "${code}" == "200" ]]
