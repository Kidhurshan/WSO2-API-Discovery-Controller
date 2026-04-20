#!/usr/bin/env bash
# install.sh — install WSO2 API Discovery Controller as a systemd service.
#
# Two modes:
#
#   1. Bundled (default)   — installs PostgreSQL locally and provisions an
#                            adc database with a randomly generated password.
#                            Suitable for single-VM deployments and POCs.
#
#   2. External            — skips PostgreSQL install, prompts for the
#                            connection details of an existing database, and
#                            verifies connectivity before continuing.
#
# Run from the repository root:
#   sudo ./deploy/systemd/install.sh                 # bundled, interactive
#   sudo ./deploy/systemd/install.sh --yes           # bundled, non-interactive
#   sudo ./deploy/systemd/install.sh --external-db   # external, interactive
#
# Or from a release tarball: cd into the extracted directory and run the
# script the same way.
#
# Idempotency: re-running the script is safe. Existing files are detected
# and reused; the systemd unit is reinstalled and the service restarted.

set -euo pipefail

# ── defaults ────────────────────────────────────────────────────────────────

MODE=bundled                # bundled | external
ASSUME_YES=false
DB_NAME="adc"
DB_USER="adc_user"
DB_PASSWORD=""
DB_HOST="localhost"
DB_PORT="5432"
DB_SSL_MODE="disable"
INSTALL_BIN="/usr/local/bin/adc"
INSTALL_CONFIG_DIR="/etc/adc"
INSTALL_LOG_DIR="/var/log/adc"
SYSTEMD_UNIT_DIR="/etc/systemd/system"
ADC_USER="adc"
ADC_GROUP="adc"

# ── argument parsing ────────────────────────────────────────────────────────

usage() {
    cat <<'EOF'
install.sh — install WSO2 API Discovery Controller (ADC) as a systemd service.

Usage:
  sudo ./deploy/systemd/install.sh [OPTIONS]

Options:
  --bundled              Install bundled PostgreSQL locally (default).
  --external-db          Skip PostgreSQL install; prompt for external DB.
  --yes, -y              Non-interactive mode (skip all prompts).
                         In bundled mode, generates a random DB password.
                         In external mode, requires --db-host/--db-password.
  --db-host HOST         External DB host (external mode + --yes).
  --db-port PORT         External DB port (default: 5432).
  --db-name NAME         Database name (default: adc).
  --db-user USER         Database user (default: adc_user).
  --db-password PASS     Database password.
  --db-ssl-mode MODE     PostgreSQL SSL mode for external DB.
                         Options: disable, require, verify-ca, verify-full
                         Default: require for external, disable for bundled.
  -h, --help             Show this help and exit.

Examples:
  # Interactive bundled install (recommended for first-time use)
  sudo ./deploy/systemd/install.sh

  # CI / Ansible / cloud-init bundled install
  sudo ./deploy/systemd/install.sh --yes

  # External RDS / CloudSQL install
  sudo ./deploy/systemd/install.sh --external-db \
      --yes \
      --db-host adc-prod.cluster-abc.us-east-1.rds.amazonaws.com \
      --db-name adc \
      --db-user adc_user \
      --db-password "$ADC_DB_PASSWORD" \
      --db-ssl-mode require
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --bundled)        MODE=bundled; shift ;;
        --external-db)    MODE=external; DB_SSL_MODE=require; shift ;;
        --yes|-y)         ASSUME_YES=true; shift ;;
        --db-host)        DB_HOST="$2"; shift 2 ;;
        --db-port)        DB_PORT="$2"; shift 2 ;;
        --db-name)        DB_NAME="$2"; shift 2 ;;
        --db-user)        DB_USER="$2"; shift 2 ;;
        --db-password)    DB_PASSWORD="$2"; shift 2 ;;
        --db-ssl-mode)    DB_SSL_MODE="$2"; shift 2 ;;
        -h|--help)        usage; exit 0 ;;
        *)                echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

# ── preflight ───────────────────────────────────────────────────────────────

if [[ "$EUID" -ne 0 ]]; then
    echo "ERROR: must run as root (use sudo)" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
    echo "ERROR: systemctl not found — install.sh only supports systemd hosts." >&2
    echo "       For non-systemd hosts, run the adc binary directly or use Docker." >&2
    exit 1
fi

# Resolve repo paths from this script's location.
SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

BINARY_SRC="$REPO_ROOT/bin/adc-linux-amd64"
CONFIG_SRC="$REPO_ROOT/config/config.toml"
SERVICE_SRC="$SCRIPT_DIR/adc.service"
PG_BOOTSTRAP="$SCRIPT_DIR/postgres-bootstrap.sh"

if [[ ! -f "$CONFIG_SRC" ]]; then
    echo "ERROR: config template not found at $CONFIG_SRC" >&2
    exit 1
fi
if [[ ! -f "$SERVICE_SRC" ]]; then
    echo "ERROR: systemd unit not found at $SERVICE_SRC" >&2
    exit 1
fi

# Build the binary if it isn't pre-built.
if [[ ! -f "$BINARY_SRC" ]]; then
    if command -v go >/dev/null 2>&1; then
        echo "[install] binary not found, building $BINARY_SRC..."
        (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            go build -ldflags "-s -w" -o "$BINARY_SRC" ./cmd/adc/)
    else
        echo "ERROR: $BINARY_SRC not found and 'go' is not installed." >&2
        echo "       Build it on a machine with Go and copy bin/adc-linux-amd64 here," >&2
        echo "       or install Go and re-run this script." >&2
        exit 1
    fi
fi

# ── interactive prompts (skipped with --yes) ────────────────────────────────

prompt() {
    local prompt_msg="$1" default_val="${2:-}" var_name="$3"
    if $ASSUME_YES; then
        printf -v "$var_name" '%s' "$default_val"
        return
    fi
    local input
    if [[ -n "$default_val" ]]; then
        read -r -p "$prompt_msg [$default_val]: " input
    else
        read -r -p "$prompt_msg: " input
    fi
    printf -v "$var_name" '%s' "${input:-$default_val}"
}

prompt_secret() {
    local prompt_msg="$1" var_name="$2"
    if $ASSUME_YES; then
        return
    fi
    local input
    read -r -s -p "$prompt_msg: " input
    echo
    printf -v "$var_name" '%s' "$input"
}

echo
echo "════════════════════════════════════════════════════════════════════"
echo " WSO2 API Discovery Controller — installer"
echo "════════════════════════════════════════════════════════════════════"
echo " Mode:               $MODE"
echo " Binary:             $BINARY_SRC → $INSTALL_BIN"
echo " Config:             $INSTALL_CONFIG_DIR/config.toml"
echo " Logs:               $INSTALL_LOG_DIR/"
echo " Systemd unit:       $SYSTEMD_UNIT_DIR/adc.service"
echo " Service user:       $ADC_USER:$ADC_GROUP"
echo "════════════════════════════════════════════════════════════════════"
echo

if [[ "$MODE" == "external" ]]; then
    if ! $ASSUME_YES; then
        echo "External database mode — please provide your PostgreSQL coordinates."
        prompt "Host"     "$DB_HOST" DB_HOST
        prompt "Port"     "$DB_PORT" DB_PORT
        prompt "Database" "$DB_NAME" DB_NAME
        prompt "User"     "$DB_USER" DB_USER
        prompt_secret "Password" DB_PASSWORD
        prompt "SSL mode (disable/require/verify-ca/verify-full)" "$DB_SSL_MODE" DB_SSL_MODE
    fi
    if [[ -z "$DB_PASSWORD" ]]; then
        echo "ERROR: --db-password is required in external mode" >&2
        exit 2
    fi
fi

if [[ "$MODE" == "bundled" && -z "$DB_PASSWORD" ]]; then
    if command -v openssl >/dev/null 2>&1; then
        DB_PASSWORD="$(openssl rand -base64 32 | tr -d '+/=' | head -c 32)"
    else
        DB_PASSWORD="$(head -c 256 /dev/urandom | tr -dc 'A-Za-z0-9' | head -c 32)"
    fi
    echo "[install] generated random database password (stored in /etc/adc/adc.env)"
fi

if ! $ASSUME_YES; then
    echo
    read -r -p "Proceed with installation? [Y/n]: " confirm
    case "${confirm:-Y}" in
        [Yy]|[Yy][Ee][Ss]) ;;
        *) echo "Aborted."; exit 0 ;;
    esac
fi

# ── system user + directories ───────────────────────────────────────────────

if ! getent group "$ADC_GROUP" >/dev/null; then
    echo "[install] creating group $ADC_GROUP"
    groupadd --system "$ADC_GROUP"
fi
if ! id -u "$ADC_USER" >/dev/null 2>&1; then
    echo "[install] creating user $ADC_USER"
    useradd --system --gid "$ADC_GROUP" --home-dir /nonexistent \
        --shell /usr/sbin/nologin "$ADC_USER"
fi

install -d -m 0755 "$INSTALL_CONFIG_DIR"
install -d -m 0755 -o "$ADC_USER" -g "$ADC_GROUP" "$INSTALL_LOG_DIR"

# ── postgres bootstrap (bundled mode only) ──────────────────────────────────

if [[ "$MODE" == "bundled" ]]; then
    if [[ ! -x "$PG_BOOTSTRAP" ]]; then
        chmod +x "$PG_BOOTSTRAP"
    fi
    "$PG_BOOTSTRAP" --db "$DB_NAME" --user "$DB_USER" --password "$DB_PASSWORD"
else
    echo "[install] external DB mode — skipping bundled postgres install"
    echo "[install] verifying connectivity to $DB_HOST:$DB_PORT/$DB_NAME ..."
    if command -v psql >/dev/null 2>&1; then
        if PGPASSWORD="$DB_PASSWORD" psql \
                -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
                -c '\q' >/dev/null 2>&1; then
            echo "[install] connectivity OK"
        else
            echo "WARNING: could not connect to $DB_HOST:$DB_PORT/$DB_NAME as $DB_USER." >&2
            echo "         Continuing anyway — ADC will retry on startup. See" >&2
            echo "         docs/external-postgres.md for troubleshooting." >&2
        fi
    else
        echo "[install] psql not installed — skipping connectivity check"
    fi
fi

# ── binary install ──────────────────────────────────────────────────────────

echo "[install] installing binary → $INSTALL_BIN"
install -m 0755 "$BINARY_SRC" "$INSTALL_BIN"

# ── config + env file ───────────────────────────────────────────────────────

# Write the env file FIRST (mode 0640, owned by root:adc — readable by the
# service user but not world-readable).
ENV_FILE="$INSTALL_CONFIG_DIR/adc.env"
echo "[install] writing $ENV_FILE (mode 0640)"
umask 0027
cat > "$ENV_FILE" <<EOF
# Database credentials for the WSO2 API Discovery Controller.
# This file is loaded by systemd via EnvironmentFile= and consumed by
# adc.service. ADC's config loader expands \${POSTGRES_*} references in
# /etc/adc/config.toml against this environment.
#
# To rotate: edit this file, then 'sudo systemctl restart adc'.
POSTGRES_DB=$DB_NAME
POSTGRES_USER=$DB_USER
POSTGRES_PASSWORD=$DB_PASSWORD
EOF
umask 0022
chown "root:$ADC_GROUP" "$ENV_FILE"
chmod 0640 "$ENV_FILE"

# Copy config.toml, then patch the [catalog.datastore] section so credentials
# are sourced from the env file. host/port/ssl_mode are set inline because
# they vary per install (bundled vs external) and don't need to be secret.
CONFIG_DEST="$INSTALL_CONFIG_DIR/config.toml"
if [[ -f "$CONFIG_DEST" ]]; then
    BACKUP="$CONFIG_DEST.bak.$(date +%Y%m%d-%H%M%S)"
    echo "[install] existing config detected — backing up to $BACKUP"
    cp -a "$CONFIG_DEST" "$BACKUP"
fi

echo "[install] writing $CONFIG_DEST"
cp "$CONFIG_SRC" "$CONFIG_DEST"

# Patch the datastore section. We use a python heredoc for safe TOML editing
# rather than fragile sed regexes — every supported install host has python3.
if ! command -v python3 >/dev/null 2>&1; then
    echo "ERROR: python3 is required to patch the datastore section." >&2
    echo "       Install python3 (apt install python3 / dnf install python3) and re-run." >&2
    exit 1
fi

python3 - "$CONFIG_DEST" "$DB_HOST" "$DB_PORT" "$DB_SSL_MODE" <<'PY'
import re
import sys

path, host, port, ssl_mode = sys.argv[1:]
with open(path, "r", encoding="utf-8") as f:
    content = f.read()

# Locate the [catalog.datastore] section: from its header up to the next
# top-level [section] header (or end of file). We must scope all field
# substitutions to this slice, because the same field names (host, port,
# user, password, database) appear in [discovery.source.clickhouse] earlier
# in the file. Patching globally would silently rewrite the wrong section.
section_re = re.compile(
    r'(?ms)^\[catalog\.datastore\]\s*\n(.*?)(?=^\[|\Z)'
)
m = section_re.search(content)
if not m:
    sys.stderr.write(f"ERROR: [catalog.datastore] section not found in {path}\n")
    sys.exit(1)

section_start, section_end = m.start(1), m.end(1)
section = content[section_start:section_end]

def patch_in_section(field, new_value):
    global section
    pattern = re.compile(rf'(?m)^{field}\s*=\s*.*$')
    if not pattern.search(section):
        sys.stderr.write(f"ERROR: field '{field}' not found in [catalog.datastore]\n")
        sys.exit(1)
    section = pattern.sub(f'{field} = {new_value}', section, count=1)

patch_in_section("host",     f'"{host}"')
patch_in_section("port",     port)
patch_in_section("database", '"${POSTGRES_DB}"')
patch_in_section("user",     '"${POSTGRES_USER}"')
patch_in_section("password", '"${POSTGRES_PASSWORD}"')
patch_in_section("ssl_mode", f'"{ssl_mode}"')

content = content[:section_start] + section + content[section_end:]

with open(path, "w", encoding="utf-8") as f:
    f.write(content)
PY

chown "root:$ADC_GROUP" "$CONFIG_DEST"
chmod 0640 "$CONFIG_DEST"

# Patch log_output to a file under /var/log/adc so logs survive restarts and
# are visible without journalctl. Same python tool.
python3 - "$CONFIG_DEST" <<'PY'
import re, sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as f:
    content = f.read()
content = re.sub(r'(?m)^log_output\s*=\s*.*$', 'log_output = "/var/log/adc/adc.log"', content, count=1)
with open(path, "w", encoding="utf-8") as f:
    f.write(content)
PY

# Validate the config before installing the unit. ADC's --validate flag
# loads, expands env vars, parses, applies defaults, and runs Validate().
# ADC_MODE and POSTGRES_HOST match what adc.service supplies at runtime via
# Environment=, so --validate sees the same env the running service will.
echo "[install] validating config..."
if ! sudo -u "$ADC_USER" \
        env ADC_MODE=standalone \
            POSTGRES_HOST="$DB_HOST" \
            POSTGRES_DB="$DB_NAME" \
            POSTGRES_USER="$DB_USER" \
            POSTGRES_PASSWORD="$DB_PASSWORD" \
        "$INSTALL_BIN" --config "$CONFIG_DEST" --validate; then
    echo "ERROR: config validation failed — see error above." >&2
    exit 1
fi

# ── systemd unit ────────────────────────────────────────────────────────────

UNIT_DEST="$SYSTEMD_UNIT_DIR/adc.service"
echo "[install] installing systemd unit → $UNIT_DEST"
install -m 0644 "$SERVICE_SRC" "$UNIT_DEST"

systemctl daemon-reload
systemctl enable adc.service
systemctl restart adc.service

# ── post-install verification ───────────────────────────────────────────────

echo "[install] waiting for ADC to become healthy..."
HEALTH_PORT="$(sed -n -E 's/^health_port[[:space:]]*=[[:space:]]*([0-9]+).*/\1/p' "$CONFIG_DEST" | head -1)"
HEALTH_PORT="${HEALTH_PORT:-8090}"

for i in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:$HEALTH_PORT/readyz" >/dev/null 2>&1; then
        break
    fi
    if [[ $i -eq 30 ]]; then
        echo "ERROR: ADC did not become ready within 60s" >&2
        echo "Recent logs:" >&2
        journalctl -u adc.service --no-pager -n 30 >&2 || true
        exit 1
    fi
    sleep 2
done

echo
echo "════════════════════════════════════════════════════════════════════"
echo " ADC installed and running."
echo "════════════════════════════════════════════════════════════════════"
echo
echo " Status:         systemctl status adc"
echo " Logs (live):    journalctl -u adc -f"
echo " Logs (file):    tail -f /var/log/adc/adc.log"
echo " Health:         curl http://127.0.0.1:$HEALTH_PORT/readyz"
echo " Config:         $CONFIG_DEST"
echo " Credentials:    $ENV_FILE  (mode 0640)"
echo
if [[ "$MODE" == "bundled" ]]; then
    echo " Bundled PostgreSQL is running locally."
    echo " The DB password is stored in $ENV_FILE — back it up if you care about the data."
else
    echo " Connected to external PostgreSQL at $DB_HOST:$DB_PORT/$DB_NAME"
fi
echo
echo " All five pipeline phases are DISABLED by default. Edit $CONFIG_DEST"
echo " to enable Phase 1 (discovery), Phase 2 (managed sync), etc., then:"
echo "   sudo systemctl restart adc"
echo
