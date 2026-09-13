#!/usr/bin/env bash
# نصب گواهی جدا برای یک دامنه روی سرور گیت‌وی (SNI روی 443).
#
#   sudo DOMAIN=sbzl.ir PFX=/path/to/sbzl.ir-cert.pfx PFX_PASSWORD='***' bash deploy/apply-domain-ssl.sh
#   sudo DOMAIN=sbzl.ir CERT_PEM=/path/to/sbzl.ir-cert.pem KEY_PEM=/path/to/privkey.pem bash deploy/apply-domain-ssl.sh
#
# خروجی:
#   /etc/pki/nginx/<domain>/fullchain.pem
#   /etc/pki/nginx/<domain>/privkey.pem
#
# سپس در پنل، برای همان دامنه:
#   گواهی: /etc/pki/nginx/sbzl.ir/fullchain.pem
#   کلید:  /etc/pki/nginx/sbzl.ir/privkey.pem
# و Nginx → اعمال روی سرور.
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  echo "با root اجرا کنید" >&2
  exit 1
fi

DOMAIN="${DOMAIN:-}"
if [[ -z "$DOMAIN" ]]; then
  echo "DOMAIN لازم است؛ مثال: DOMAIN=sbzl.ir" >&2
  exit 1
fi
DOMAIN="$(echo "$DOMAIN" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"

PFX_PASSWORD="${PFX_PASSWORD:-}"
PFX="${PFX:-}"
CERT_PEM="${CERT_PEM:-}"
KEY_PEM="${KEY_PEM:-}"
OUT_DIR="/etc/pki/nginx/${DOMAIN}"
CERT_FILE="${OUT_DIR}/fullchain.pem"
KEY_FILE="${OUT_DIR}/privkey.pem"

mkdir -p "${OUT_DIR}"
chmod 755 /etc/pki/nginx >/dev/null 2>&1 || true

extract_pfx() {
  local pfx="$1" tmp ok=0
  tmp="$(mktemp -d)"
  chmod 700 "$tmp"
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
    echo "استخراج PFX شکست خورد. رمز یا فایل را چک کنید (PFX_PASSWORD)." >&2
    exit 1
  fi
  if [[ -s "${tmp}/ca.pem" ]] && grep -q "BEGIN CERTIFICATE" "${tmp}/ca.pem"; then
    cat "${tmp}/cert.pem" "${tmp}/ca.pem" > "${CERT_FILE}"
  else
    cp "${tmp}/cert.pem" "${CERT_FILE}"
  fi
  cp "${tmp}/key.pem" "${KEY_FILE}"
  rm -rf "$tmp"
}

if [[ -n "$PFX" ]]; then
  if [[ ! -f "$PFX" ]]; then
    echo "فایل PFX پیدا نشد: $PFX" >&2
    exit 1
  fi
  if [[ -z "$PFX_PASSWORD" ]]; then
    echo "برای PFX باید PFX_PASSWORD بدهید." >&2
    exit 1
  fi
  echo "==> استخراج PFX برای ${DOMAIN}"
  extract_pfx "$PFX"
elif [[ -n "$CERT_PEM" && -n "$KEY_PEM" ]]; then
  if [[ ! -f "$CERT_PEM" || ! -f "$KEY_PEM" ]]; then
    echo "CERT_PEM یا KEY_PEM پیدا نشد" >&2
    exit 1
  fi
  echo "==> کپی PEM برای ${DOMAIN}"
  cp "$CERT_PEM" "${CERT_FILE}"
  cp "$KEY_PEM" "${KEY_FILE}"
else
  echo "یکی از این‌ها لازم است:" >&2
  echo "  PFX=... PFX_PASSWORD=..." >&2
  echo "  یا CERT_PEM=... KEY_PEM=..." >&2
  exit 1
fi

chmod 640 "${CERT_FILE}" "${KEY_FILE}"
chown root:nginx "${CERT_FILE}" "${KEY_FILE}" 2>/dev/null || chown root:root "${CERT_FILE}" "${KEY_FILE}"
restorecon -v "${CERT_FILE}" "${KEY_FILE}" >/dev/null 2>&1 || true

echo "==> گواهی نصب شد:"
openssl x509 -in "${CERT_FILE}" -noout -subject -dates || true
echo
echo "  ssl_cert = ${CERT_FILE}"
echo "  ssl_key  = ${KEY_FILE}"
echo
echo "در پنل گیت‌وی دامنه ${DOMAIN} را ویرایش کنید و همین دو مسیر را بگذارید،"
echo "سپس Nginx → اعمال روی سرور (shared_443)."
echo
echo "تست:"
echo "  curl -skI --resolve ${DOMAIN}:443:127.0.0.1 https://${DOMAIN}/"
