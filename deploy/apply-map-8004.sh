#!/usr/bin/env bash
# دامنه map-gateway.sabzevar.ir را روی پورت 8004 به gateway-core وصل می‌کند.
# پیش‌فرض HTTPS است تا سایت https://map.sabzevar.ir مسدود mixed-content نشود.
#
#   sudo bash deploy/apply-map-8004.sh
#   sudo MAP_SSL=0 bash deploy/apply-map-8004.sh          # فقط HTTP
#   sudo MAP_UPSTREAM='http://192.168.1.19:7003' bash deploy/apply-map-8004.sh
#
# آدرس: https://map-gateway.sabzevar.ir:8004/   (یا http اگر MAP_SSL=0)
set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "با root اجرا کنید: sudo bash deploy/apply-map-8004.sh" >&2
  exit 1
fi

ENV_FILE="/etc/gateway-core.env"
BACKEND="${BACKEND:-127.0.0.1:8002}"
NGINX_MAP="/etc/nginx/conf.d/map-gateway-8004.conf"
MAP_HOST="${MAP_HOST:-map-gateway.sabzevar.ir}"
MAP_PORT="${MAP_PORT:-8004}"
MAP_SSL="${MAP_SSL:-1}"
MAP_UPSTREAM="${MAP_UPSTREAM:-http://192.168.1.19:7003}"

if [[ -z "${CERT_FILE:-}" ]]; then
  for d in /etc/pki/nginx /etc/nginx/ssl; do
    if [[ -f "$d/fullchain.pem" && -f "$d/privkey.pem" ]]; then
      CERT_FILE="$d/fullchain.pem"
      KEY_FILE="$d/privkey.pem"
      break
    fi
  done
fi
CERT_FILE="${CERT_FILE:-/etc/pki/nginx/fullchain.pem}"
KEY_FILE="${KEY_FILE:-/etc/pki/nginx/privkey.pem}"

if [[ "${MAP_SSL}" == "1" && ( ! -f "${CERT_FILE}" || ! -f "${KEY_FILE}" ) ]]; then
  echo "گواهی پیدا نشد (${CERT_FILE}) — به HTTP روی ${MAP_PORT} سوییچ می‌شود" >&2
  MAP_SSL=0
fi

systemctl enable gateway-core >/dev/null 2>&1 || true
systemctl restart gateway-core || true
sleep 1

echo "==> به‌روزرسانی آپ‌ستریم ${MAP_HOST} → ${MAP_UPSTREAM}"
python3 - "${MAP_HOST}" "${MAP_UPSTREAM}" <<'PY'
import os, sqlite3, sys, time, uuid
from urllib.parse import urlparse
host, upstream = sys.argv[1], sys.argv[2]
parsed = urlparse(upstream)
scheme = parsed.scheme or "http"
target_host = parsed.hostname or "127.0.0.1"
target_port = parsed.port or (443 if scheme == "https" else 80)
url = f"{scheme}://{target_host}:{target_port}"
candidates = []
env_path = "/etc/gateway-core.env"
if os.path.isfile(env_path):
    for line in open(env_path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if line.startswith("GATEWAY_DB="):
            candidates.append(line.split("=", 1)[1].strip().strip('"'))
candidates += [
    "/var/lib/gateway-core/gateway.db",
    os.path.join(os.getcwd(), "data/gateway.db"),
]
db_path = next((p for p in candidates if p and os.path.isfile(p)), None)
if not db_path:
    print("    WARNING: gateway.db پیدا نشد — آپ‌ستریم را از پنل عوض کنید", file=sys.stderr)
    sys.exit(0)
con = sqlite3.connect(db_path)
con.row_factory = sqlite3.Row
row = con.execute("SELECT id FROM gateways WHERE host = ?", (host,)).fetchone()
if not row:
    print(f"    WARNING: گیت‌وی {host} در DB نیست — از پنل بسازید", file=sys.stderr)
    sys.exit(0)
gid = row["id"]
now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
existing = con.execute("SELECT id FROM upstreams WHERE gateway_id = ? ORDER BY weight DESC", (gid,)).fetchall()
if existing:
    con.execute(
        """UPDATE upstreams SET url = ?, enabled = 1, kind = 'remote',
           target_host = ?, target_port = ?, scheme = ?, health_path = '/api/v1/health'
           WHERE id = ?""",
        (url, target_host, target_port, scheme, existing[0]["id"]),
    )
else:
    con.execute(
        """INSERT INTO upstreams(id, gateway_id, url, weight, enabled, kind, target_host, target_port, scheme, health_path)
           VALUES (?, ?, ?, 1, 1, 'remote', ?, ?, ?, '/api/v1/health')""",
        (str(uuid.uuid4()), gid, url, target_host, target_port, scheme),
    )
try:
    con.execute(
        "UPDATE gateways SET health_path = ?, cors_allow_origin = '*', allowed_origins = '[]', updated_at = ? WHERE id = ?",
        ("/api/v1/health", now, gid),
    )
except sqlite3.OperationalError:
    con.execute(
        "UPDATE gateways SET health_path = ?, cors_allow_origin = '*', updated_at = ? WHERE id = ?",
        ("/api/v1/health", now, gid),
    )
# map-api owns API keys; disable any leftover gateway-core tokens on this host.
try:
    con.execute("UPDATE access_tokens SET enabled = 0 WHERE gateway_id = ?", (gid,))
except sqlite3.OperationalError:
    pass
con.commit()
print(f"    DB {db_path}: {host} → {url}")
con.close()
PY

# Reload in-process registry if the binary exposes it via restart (already restarted above).
# A second restart picks up the SQLite change.
systemctl restart gateway-core || true
sleep 1

if [[ "${MAP_SSL}" == "1" ]]; then
  echo "==> Nginx HTTPS :${MAP_PORT} برای ${MAP_HOST}"
  cat > "${NGINX_MAP}" <<EOF
# map-gateway dedicated port — managed by deploy/apply-map-8004.sh
map \$http_upgrade \$connection_upgrade_map8004 {
    default upgrade;
    ''      close;
}
server {
    listen ${MAP_PORT} ssl;
    listen [::]:${MAP_PORT} ssl;
    http2 on;
    server_name ${MAP_HOST};
    ssl_certificate     ${CERT_FILE};
    ssl_certificate_key ${KEY_FILE};
    client_max_body_size 16m;
    location / {
        proxy_pass http://${BACKEND};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade_map8004;
        proxy_connect_timeout 5s;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
}
EOF
else
  echo "==> Nginx HTTP :${MAP_PORT} برای ${MAP_HOST}"
  cat > "${NGINX_MAP}" <<EOF
# map-gateway dedicated port — managed by deploy/apply-map-8004.sh
map \$http_upgrade \$connection_upgrade_map8004 {
    default upgrade;
    ''      close;
}
server {
    listen ${MAP_PORT};
    listen [::]:${MAP_PORT};
    server_name ${MAP_HOST};
    client_max_body_size 16m;
    location / {
        proxy_pass http://${BACKEND};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade_map8004;
        proxy_connect_timeout 5s;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
}
EOF
fi
chmod 644 "${NGINX_MAP}"
restorecon -v "${NGINX_MAP}" >/dev/null 2>&1 || true

echo "==> فایروال / SELinux پورت ${MAP_PORT}"
firewall-cmd --permanent --add-port=${MAP_PORT}/tcp >/dev/null 2>&1 || true
firewall-cmd --reload >/dev/null 2>&1 || true
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 || true
semanage port -a -t http_port_t -p tcp "${MAP_PORT}" 2>/dev/null || semanage port -m -t http_port_t -p tcp "${MAP_PORT}" 2>/dev/null || true

echo "==> nginx -t && reload"
nginx -t
systemctl reload nginx

SCHEME="http"
[[ "${MAP_SSL}" == "1" ]] && SCHEME="https"
echo
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "Go 8002 health: %{http_code}\n" \
  -H "Host: ${MAP_HOST}" "http://${BACKEND}/api/v1/health" || true
curl -k -sS --noproxy '*' --max-time 8 -o /dev/null -w "Nginx ${MAP_PORT}: %{http_code}\n" \
  -H "Host: ${MAP_HOST}" "${SCHEME}://127.0.0.1:${MAP_PORT}/api/v1/health" || true
echo
echo "Public: ${SCHEME}://${MAP_HOST}:${MAP_PORT}/styles/style.json?key=pk_..."
echo "Do not add gateway-core access tokens on this host — clients must send map-api ?key="
ss -lntp | grep -E ":${MAP_PORT}|:8002|:443" || true
