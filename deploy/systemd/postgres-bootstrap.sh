#!/usr/bin/env bash
# postgres-bootstrap.sh — install PostgreSQL and create the ADC database/user.
#
# This script is normally called by install.sh in bundled mode, but it can also
# be run standalone if you want to provision the bundled database without
# installing the ADC service itself.
#
# Usage:
#   sudo ./postgres-bootstrap.sh \
#       --db adc \
#       --user adc_user \
#       --password "$(openssl rand -base64 32)"
#
# Supported distributions:
#   - Debian / Ubuntu (apt-get)
#   - RHEL / CentOS / Rocky / AlmaLinux (dnf or yum)
#
# Idempotency: safe to re-run. Existing installs are detected and reused;
# the user/database are created only if missing; the password is updated to
# match the value passed in.

set -euo pipefail

DB_NAME=""
DB_USER=""
DB_PASSWORD=""

usage() {
    cat <<'EOF'
postgres-bootstrap.sh — install PostgreSQL and provision the ADC database.

Usage:
  sudo postgres-bootstrap.sh --db NAME --user USER --password PASS

Required:
  --db NAME            Database name to create (e.g. "adc")
  --user USER          Role/user to create (e.g. "adc_user")
  --password PASS      Password for the role

Optional:
  -h, --help           Show this help and exit
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --db)        DB_NAME="$2"; shift 2 ;;
        --user)      DB_USER="$2"; shift 2 ;;
        --password)  DB_PASSWORD="$2"; shift 2 ;;
        -h|--help)   usage; exit 0 ;;
        *)           echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

if [[ -z "$DB_NAME" || -z "$DB_USER" || -z "$DB_PASSWORD" ]]; then
    echo "ERROR: --db, --user, and --password are all required" >&2
    usage >&2
    exit 2
fi

if [[ "$EUID" -ne 0 ]]; then
    echo "ERROR: must run as root (use sudo)" >&2
    exit 1
fi

# ── distro detection ────────────────────────────────────────────────────────

if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    DISTRO_ID="${ID:-unknown}"
    DISTRO_LIKE="${ID_LIKE:-}"
else
    echo "ERROR: cannot detect distribution (no /etc/os-release)" >&2
    exit 1
fi

case "$DISTRO_ID $DISTRO_LIKE" in
    *debian*|*ubuntu*) PKG_FAMILY=debian ;;
    *rhel*|*centos*|*rocky*|*almalinux*|*fedora*) PKG_FAMILY=rhel ;;
    *)
        echo "ERROR: unsupported distribution: $DISTRO_ID" >&2
        echo "Supported: Debian/Ubuntu, RHEL/CentOS/Rocky/AlmaLinux/Fedora." >&2
        echo "For other distros, install PostgreSQL manually and re-run install.sh --external-db." >&2
        exit 1
        ;;
esac

echo "[postgres-bootstrap] detected: $DISTRO_ID ($PKG_FAMILY family)"

# ── package install ─────────────────────────────────────────────────────────

install_postgres_debian() {
    if dpkg -s postgresql >/dev/null 2>&1; then
        echo "[postgres-bootstrap] postgresql already installed, skipping apt install"
    else
        echo "[postgres-bootstrap] installing postgresql via apt-get..."
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq
        apt-get install -y -qq postgresql postgresql-contrib
    fi
    PG_SERVICE=postgresql
}

install_postgres_rhel() {
    if rpm -q postgresql-server >/dev/null 2>&1; then
        echo "[postgres-bootstrap] postgresql-server already installed, skipping dnf install"
    else
        echo "[postgres-bootstrap] installing postgresql-server via dnf..."
        if command -v dnf >/dev/null 2>&1; then
            dnf install -y -q postgresql-server postgresql-contrib
        else
            yum install -y -q postgresql-server postgresql-contrib
        fi
    fi
    # On RHEL the data dir must be initdb'd before first start.
    if [[ ! -f /var/lib/pgsql/data/PG_VERSION ]]; then
        echo "[postgres-bootstrap] running postgresql-setup initdb..."
        if command -v postgresql-setup >/dev/null 2>&1; then
            postgresql-setup --initdb
        else
            /usr/bin/initdb -D /var/lib/pgsql/data
        fi
    fi
    PG_SERVICE=postgresql
}

case "$PKG_FAMILY" in
    debian) install_postgres_debian ;;
    rhel)   install_postgres_rhel ;;
esac

# ── service start + enable ──────────────────────────────────────────────────

echo "[postgres-bootstrap] starting + enabling $PG_SERVICE..."
systemctl enable --now "$PG_SERVICE"

# Wait for postgres to accept connections (up to ~30s).
for i in $(seq 1 30); do
    if sudo -u postgres psql -tAc 'SELECT 1' >/dev/null 2>&1; then
        break
    fi
    if [[ $i -eq 30 ]]; then
        echo "ERROR: postgresql did not become ready within 30s" >&2
        systemctl status "$PG_SERVICE" --no-pager || true
        exit 1
    fi
    sleep 1
done

echo "[postgres-bootstrap] postgresql is ready"

# ── role + database provisioning ────────────────────────────────────────────

# Use psql with -v to safely pass identifiers / values.
#
# Note: psql variable substitution (:'var', :"var") does NOT happen inside
# dollar-quoted strings ($$ ... $$), so we cannot put a DO block here. We
# instead use top-level statements with `\gexec`, which generates the SQL
# string with format() and then executes it.
sudo -u postgres psql -v ON_ERROR_STOP=1 \
    -v db_name="$DB_NAME" \
    -v db_user="$DB_USER" \
    -v db_password="$DB_PASSWORD" \
    <<'PSQL'
-- Create role if missing. format(%I) handles identifier quoting; format(%L)
-- handles literal quoting (escaping any single quotes in the password).
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'db_user', :'db_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'db_user')
\gexec

-- Always reset the password so re-runs converge on the value passed in.
-- This is the idempotent counterpart to CREATE ROLE above.
SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', :'db_user', :'db_password')
\gexec

-- Create database if missing, owned by the role.
SELECT format('CREATE DATABASE %I OWNER %I', :'db_name', :'db_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db_name')
\gexec

-- Grant privileges (idempotent, no-op if already granted).
GRANT ALL PRIVILEGES ON DATABASE :"db_name" TO :"db_user";
PSQL

# PostgreSQL 15+ revoked CREATE on the public schema from PUBLIC. Grant it
# explicitly to the ADC user inside the new database.
sudo -u postgres psql -d "$DB_NAME" -v ON_ERROR_STOP=1 \
    -v db_user="$DB_USER" \
    <<'PSQL'
GRANT ALL ON SCHEMA public TO :"db_user";
PSQL

echo "[postgres-bootstrap] database '$DB_NAME' ready, user '$DB_USER' provisioned"

# ── verify connectivity from the new user ───────────────────────────────────

if PGPASSWORD="$DB_PASSWORD" psql -h 127.0.0.1 -U "$DB_USER" -d "$DB_NAME" -c '\q' >/dev/null 2>&1; then
    echo "[postgres-bootstrap] connectivity test passed"
else
    echo "WARNING: could not connect as $DB_USER to $DB_NAME via TCP." >&2
    echo "         pg_hba.conf may require host-auth tweaks. ADC will still work" >&2
    echo "         if it connects via the unix socket." >&2
fi

echo "[postgres-bootstrap] done."
