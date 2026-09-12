#!/usr/bin/env bash
# نصب کامل Gateway Core روی AlmaLinux
# اجرا از ریشه ریپو:
#   sudo bash deploy/install-almalinux.sh
#
# متغیرهای اختیاری:
#   ADMIN_PASSWORD   رمز پنل (اگر خالی باشد تصادفی ساخته می‌شود؛ فایل موجود را overwrite نمی‌کند)
#   ADMIN_HOST       پیش‌فرض gateway-admin.sabzevar.ir
#   SKIP_BUILD=1     اگر باینری از قبل در /usr/local/bin/gateway-core است
#   SKIP_NGINX=1
#   SKIP_FIREWALL=1

set -euo pipefail

if [[ ${EUID:-0} -ne 0 ]]; then
  echo "این اسکریپت باید با root اجرا شود: sudo bash deploy/install-almalinux.sh" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ADMIN_HOST="${ADMIN_HOST:-gateway-admin.sabzevar.ir}"
BIN_PATH="/usr/local/bin/gateway-core"
DATA_DIR="/var/lib/gateway-core"
ENV_FILE="/etc/gateway-core.env"
UNIT_FILE="/etc/systemd/system/gateway-core.service"
NGINX_MANAGED="/etc/nginx/conf.d/gateway-managed.conf"
CERT_DIR="/etc/pki/nginx"
CERT_FILE="${CERT_DIR}/fullchain.pem"
KEY_FILE="${CERT_DIR}/privkey.pem"

if [[ -f /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
fi
if [[ "${ID:-}" != "almalinux" && "${ID:-}" != "rhel" && "${ID:-}" != "centos" && "${ID_LIKE:-}" != *"rhel"* ]]; then
  echo "هشدار: این اسکریپت برای AlmaLinux/RHEL نوشته شده (شناسه فعلی: ${ID:-unknown}). ادامه می‌دهیم." >&2
fi

echo "==> نصب بسته‌ها"
dnf install -y nginx firewalld openssl policycoreutils-python-utils tar gzip curl >/dev/null
systemctl enable --now firewalld >/dev/null 2>&1 || true

go_arch() {
  case "$(uname -m)" in
    x86_64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo amd64 ;;
  esac
}

# go1.27.0 در فهرست JSON هست ولی روی CDN هنوز 404 است. اول نسخه‌های قطعی را می‌گیریم.
go_versions() {
  printf '%s\n' go1.24.6 go1.23.8 go1.22.10 go1.26.7
}

download_go_tarball() {
  local dest="$1" arch file url ver
  arch="$(go_arch)"
  local -a mirrors=(
    "https://mirrors.aliyun.com/golang"
    "https://cdn.npmmirror.com/binaries/go"
    "https://mirrors.cloud.tencent.com/go"
    "https://mirrors.ustc.edu.cn/golang"
    "https://dl.google.com/go"
  )
  local -a versions
  mapfile -t versions < <(go_versions "${arch}")
  for ver in "${versions[@]}"; do
    [[ "${ver}" == go1.* ]] || continue
    file="${ver}.linux-${arch}.tar.gz"
    for base in "${mirrors[@]}"; do
      url="${base}/${file}"
      echo "    امتحان ${url}"
      if curl -fL --connect-timeout 20 --max-time 180 --retry 2 --retry-delay 1 -o "${dest}" "${url}"; then
        if tar -tzf "${dest}" >/dev/null 2>&1; then
          echo "    موفق: ${file}"
          return 0
        fi
      fi
      rm -f "${dest}"
    done
    url="https://go.dev/dl/${file}?download=true"
    echo "    امتحان ${url}"
    if curl -fL --connect-timeout 20 --max-time 180 -o "${dest}" "${url}"; then
      if tar -tzf "${dest}" >/dev/null 2>&1; then
        echo "    موفق: ${file}"
        return 0
      fi
    fi
    rm -f "${dest}"
  done
  return 1
}

go_ok() {
  command -v go >/dev/null 2>&1 || return 1
  local ver
  ver="$(go version | awk '{print $3}' | sed 's/go//')"
  [[ "$(printf '%s\n' "1.22" "$ver" | sort -V | head -n1)" == "1.22" ]]
}

ensure_go() {
  if go_ok; then
    echo "==> Go موجود است: $(go version)"
    return 0
  fi
  echo "==> تلاش برای Go از مخزن AlmaLinux"
  dnf install -y golang >/dev/null 2>&1 || true
  hash -r || true
  if go_ok; then
    echo "==> Go از dnf: $(go version)"
    return 0
  fi
  echo "==> نصب Go از آینه (حداقل 1.22)"
  local tmp
  tmp="$(mktemp -d)"
  if ! download_go_tarball "${tmp}/go.tgz"; then
    echo "دانلود Go شکست خورد. CDN گوگل/go.dev از این سرور در دسترس نیست یا نسخه روی CDN نیست." >&2
    echo "راه‌حل: روی ماشین دیگر بیلد کنید و با SKIP_BUILD=1 اسکریپت را بزنید،" >&2
    echo "یا tar رسمی را دستی در /usr/local/go باز کنید." >&2
    rm -rf "${tmp}"
    exit 1
  fi
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tmp}/go.tgz"
  ln -sfn /usr/local/go/bin/go /usr/local/bin/go
  hash -r || true
  go version
  rm -rf "${tmp}"
}

# proxy.golang.org به storage.googleapis.com می‌رود و از ایران اغلب 403 می‌شود.
setup_go_modules() {
  export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
  export GOPROXY="${GOPROXY:-https://goproxy.cn,https://goproxy.io,https://mirrors.aliyun.com/goproxy,direct}"
  export GOSUMDB="${GOSUMDB:-off}"
  echo "==> GOPROXY=${GOPROXY}"
}

if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  ensure_go
  setup_go_modules
  echo "==> ساخت باینری"
  (cd "${REPO_ROOT}" && go build -o /tmp/gateway-core ./cmd/gateway)
  install -m 0755 /tmp/gateway-core "${BIN_PATH}"
  restorecon -v "${BIN_PATH}" >/dev/null 2>&1 || chcon -t bin_t "${BIN_PATH}" >/dev/null 2>&1 || true
  rm -f /tmp/gateway-core
elif [[ ! -x "${BIN_PATH}" ]]; then
  echo "SKIP_BUILD=1 است ولی ${BIN_PATH} وجود ندارد." >&2
  exit 1
fi

echo "==> کاربر و مسیر داده"
if ! id -u gateway >/dev/null 2>&1; then
  useradd -r -s /sbin/nologin -d "${DATA_DIR}" gateway
fi
if getent group nginx >/dev/null 2>&1; then
  usermod -a -G nginx gateway || true
fi
mkdir -p "${DATA_DIR}"
chown gateway:gateway "${DATA_DIR}"
chmod 750 "${DATA_DIR}"

if [[ ! -f "${ENV_FILE}" ]]; then
  PASS="${ADMIN_PASSWORD:-$(openssl rand -base64 18 | tr -d '/+=' | head -c 20)}"
  umask 077
  cat > "${ENV_FILE}" <<EOF
GATEWAY_ADMIN_USER=admin
GATEWAY_ADMIN_PASSWORD=${PASS}
GATEWAY_ADMIN_HOST=${ADMIN_HOST}
GATEWAY_LISTEN=0.0.0.0:8002
GATEWAY_DB=${DATA_DIR}/gateway.db
GATEWAY_NGINX_CONF=${NGINX_MANAGED}
GATEWAY_NGINX_TEST=sudo /usr/sbin/nginx -t
GATEWAY_NGINX_RELOAD=sudo /usr/sbin/nginx -s reload
GATEWAY_NGINX_UPSTREAM=127.0.0.1:8002
EOF
  chmod 600 "${ENV_FILE}"
  echo "==> رمز پنل در ${ENV_FILE} نوشته شد"
else
  echo "==> ${ENV_FILE} از قبل هست؛ دست نخورده ماند"
fi

echo "==> systemd"
cat > "${UNIT_FILE}" <<'EOF'
[Unit]
Description=Sabzevar Gateway Core
After=network.target nginx.service

[Service]
Type=simple
User=gateway
Group=gateway
SupplementaryGroups=nginx
WorkingDirectory=/var/lib/gateway-core
Environment=GATEWAY_LISTEN=0.0.0.0:8002
Environment=GATEWAY_DB=/var/lib/gateway-core/gateway.db
Environment=GATEWAY_ADMIN_HOST=gateway-admin.sabzevar.ir
Environment=GATEWAY_ADMIN_USER=admin
EnvironmentFile=-/etc/gateway-core.env
ExecStart=/usr/local/bin/gateway-core
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

echo "==> sudoers برای reload Nginx از پنل"
cat > /etc/sudoers.d/gateway-core <<'EOF'
Defaults:gateway !requiretty
gateway ALL=(root) NOPASSWD: /usr/sbin/nginx -t, /usr/sbin/nginx -s reload
EOF
chmod 440 /etc/sudoers.d/gateway-core
if ! visudo -cf /etc/sudoers.d/gateway-core >/dev/null; then
  echo "sudoers نامعتبر است" >&2
  exit 1
fi

if [[ "${SKIP_NGINX:-0}" != "1" ]]; then
  echo "==> گواهی TLS"
  mkdir -p "${CERT_DIR}"
  PFX_CANDIDATE="${REPO_ROOT}/ssl/certificate.pfx"
  if [[ -f "${PFX_CANDIDATE}" ]]; then
    echo "استخراج ${PFX_CANDIDATE}"
    PFX_PASSWORD="${PFX_PASSWORD:-12345}"
    tmpd="$(mktemp -d)"
    openssl pkcs12 -in "${PFX_CANDIDATE}" -passin "pass:${PFX_PASSWORD}" -nocerts -nodes -out "${tmpd}/key.pem" \
      || openssl pkcs12 -in "${PFX_CANDIDATE}" -passin "pass:${PFX_PASSWORD}" -legacy -nocerts -nodes -out "${tmpd}/key.pem"
    openssl pkcs12 -in "${PFX_CANDIDATE}" -passin "pass:${PFX_PASSWORD}" -nokeys -clcerts -out "${tmpd}/cert.pem" \
      || openssl pkcs12 -in "${PFX_CANDIDATE}" -passin "pass:${PFX_PASSWORD}" -legacy -nokeys -clcerts -out "${tmpd}/cert.pem"
    openssl pkcs12 -in "${PFX_CANDIDATE}" -passin "pass:${PFX_PASSWORD}" -nokeys -cacerts -out "${tmpd}/ca.pem" 2>/dev/null || true
    if [[ -s "${tmpd}/ca.pem" ]]; then cat "${tmpd}/cert.pem" "${tmpd}/ca.pem" > "${CERT_FILE}"; else cp "${tmpd}/cert.pem" "${CERT_FILE}"; fi
    cp "${tmpd}/key.pem" "${KEY_FILE}"
    rm -rf "${tmpd}"
  elif [[ ! -f "${CERT_FILE}" || ! -f "${KEY_FILE}" ]]; then
    openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
      -keyout "${KEY_FILE}" -out "${CERT_FILE}" \
      -subj "/CN=*.sabzevar.ir" >/dev/null 2>&1
    echo "گواهی خودامضا ساخته شد (${CERT_DIR}). بعداً با Let's Encrypt عوض کنید."
  else
    echo "گواهی موجود استفاده شد: ${CERT_FILE}"
  fi
  chmod 640 "${KEY_FILE}" "${CERT_FILE}"
  chown root:nginx "${KEY_FILE}" "${CERT_FILE}" || chown root:root "${KEY_FILE}" "${CERT_FILE}"

  rm -f /etc/nginx/conf.d/gateway.conf
  if [[ ! -f "${NGINX_MANAGED}" ]]; then
    cat > "${NGINX_MANAGED}" <<EOF
# bootstrap — پنل می‌تواند این فایل را بازنویسی کند
map \$http_upgrade \$connection_upgrade {
    default upgrade;
    ''      close;
}
upstream gateway_core {
    server 127.0.0.1:8002;
    keepalive 32;
}
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    client_max_body_size 2m;
    location / {
        proxy_pass http://gateway_core;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
server {
    listen 80;
    listen [::]:80;
    server_name map-gateway.sabzevar.ir apisrv-gatewaylogin.sabzevar.ir apisrv-gateway137.sabzevar.ir;
    return 301 https://\$host\$request_uri;
}
server {
    listen 80;
    listen [::]:80;
    server_name ${ADMIN_HOST};
    return 301 https://\$host:8003\$request_uri;
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name map-gateway.sabzevar.ir apisrv-gatewaylogin.sabzevar.ir apisrv-gateway137.sabzevar.ir;
    ssl_certificate     ${CERT_FILE};
    ssl_certificate_key ${KEY_FILE};
    client_max_body_size 20m;
    location / {
        proxy_pass http://gateway_core;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
        proxy_buffering off;
    }
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
        proxy_pass http://gateway_core;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
  fi
  chown gateway:nginx "${NGINX_MANAGED}" 2>/dev/null || chown gateway:gateway "${NGINX_MANAGED}"
  chmod 640 "${NGINX_MANAGED}"
  restorecon -v "${NGINX_MANAGED}" >/dev/null 2>&1 || true
  nginx -t
  systemctl enable --now nginx
  systemctl reload nginx
fi

if [[ "${SKIP_FIREWALL:-0}" != "1" ]]; then
  echo "==> فایروال و SELinux (http/https برای مدل 443 مشترک)"
  firewall-cmd --permanent --add-service=http >/dev/null
  firewall-cmd --permanent --add-service=https >/dev/null
  # 8002 فقط لوکال/مدیریت؛ از بیرون لازم نیست ولی برای سازگاری باز می‌ماند
  firewall-cmd --permanent --add-port=8002/tcp >/dev/null
  firewall-cmd --reload >/dev/null
  setsebool -P httpd_can_network_connect 1 || true
fi

echo "==> سرویس gateway-core"
systemctl daemon-reload
systemctl enable --now gateway-core
sleep 1
systemctl --no-pager --full status gateway-core || true

echo
echo "نصب تمام شد."
echo "  مدل پیشنهادی (۴۴۳ مشترک): sudo bash deploy/apply-shared-443.sh"
echo "  پنل:  https://${ADMIN_HOST}/   یا   http://<IP-سرور>:8002/_admin/"
echo "  کاربر: admin"
echo "  رمز:   داخل ${ENV_FILE}  (GATEWAY_ADMIN_PASSWORD)"
echo "  تأیید: bash deploy/verify-shared-443.sh"
echo
echo "اگر گواهی خودامضا است، مرورگر هشدار می‌دهد؛ با certbot عوض کنید."
