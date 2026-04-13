#!/usr/bin/env bash
# uninstall.sh — remove the WSO2 API Discovery Controller systemd install.
#
# By default this is a *conservative* uninstall:
#   • stops + disables adc.service
#   • removes /etc/systemd/system/adc.service
#   • removes /usr/local/bin/adc
#   • leaves /etc/adc/, /var/log/adc/, the adc user, and the database alone
#     (so a re-install can pick up where this one left off, and so historical
#     logs survive an accidental uninstall)
#
# Add --purge to also delete config, logs, and the adc system user.
# Add --drop-db to also DROP the bundled PostgreSQL database (irreversible).
# Add --remove-postgres to also uninstall the postgresql server package
#   (irreversible — affects any other database on this host).
# Add --yes to skip the confirmation prompt.

set -euo pipefail

PURGE=false
DROP_DB=false
REMOVE_PG=false
ASSUME_YES=false

DB_NAME="adc"
DB_USER="adc_user"

INSTALL_BIN="/usr/local/bin/adc"
INSTALL_CONFIG_DIR="/etc/adc"
INSTALL_LOG_DIR="/var/log/adc"
SYSTEMD_UNIT="/etc/systemd/system/adc.service"
ADC_USER="adc"
ADC_GROUP="adc"

usage() {
    cat <<'EOF'
uninstall.sh — remove WSO2 API Discovery Controller from a systemd host.

Usage:
  sudo ./deploy/systemd/uninstall.sh [OPTIONS]

Options:
  --purge              Also delete /etc/adc, /var/log/adc, and the adc user.
  --drop-db            Also DROP the bundled PostgreSQL database (data loss).
                       Reads --db-name (default: adc).
  --remove-postgres    Also uninstall the postgresql package (DANGER —
                       affects any other DB on this host).
  --db-name NAME       Database name to drop (default: adc).
  --db-user USER       Database role to drop (default: adc_user).
  --yes, -y            Skip the confirmation prompt.
  -h, --help           Show this help and exit.

Examples:
  # Conservative — keep config + data
  sudo ./deploy/systemd/uninstall.sh

  # Full removal of ADC, but leave PostgreSQL alone
  sudo ./deploy/systemd/uninstall.sh --purge --yes

  # Full clean wipe (CI / re-test scenario)
  sudo ./deploy/systemd/uninstall.sh --purge --drop-db --remove-postgres --yes
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --purge)            PURGE=true; shift ;;
        --drop-db)          DROP_DB=true; shift ;;
        --remove-postgres)  REMOVE_PG=true; shift ;;
        --db-name)          DB_NAME="$2"; shift 2 ;;
        --db-user)          DB_USER="$2"; shift 2 ;;
        --yes|-y)           ASSUME_YES=true; shift ;;
        -h|--help)          usage; exit 0 ;;
        *)                  echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

if [[ "$EUID" -ne 0 ]]; then
    echo "ERROR: must run as root (use sudo)" >&2
    exit 1
fi

echo
echo "════════════════════════════════════════════════════════════════════"
echo " WSO2 API Discovery Controller — uninstaller"
echo "════════════════════════════════════════════════════════════════════"
echo " Stop + disable service:    yes"
echo " Remove binary + unit:      yes"
echo " Purge config + logs + user: $PURGE"
echo " Drop database '$DB_NAME':   $DROP_DB"
echo " Remove postgresql package:  $REMOVE_PG"
echo "════════════════════════════════════════════════════════════════════"
echo

if ! $ASSUME_YES; then
    read -r -p "Proceed? [y/N]: " confirm
    case "${confirm:-N}" in
        [Yy]|[Yy][Ee][Ss]) ;;
        *) echo "Aborted."; exit 0 ;;
    esac
fi

# ── stop + disable service ──────────────────────────────────────────────────

if systemctl list-unit-files adc.service >/dev/null 2>&1; then
    if systemctl is-active --quiet adc.service; then
        echo "[uninstall] stopping adc.service"
        systemctl stop adc.service || true
    fi
    if systemctl is-enabled --quiet adc.service 2>/dev/null; then
        echo "[uninstall] disabling adc.service"
        systemctl disable adc.service || true
    fi
fi

# ── remove unit + binary ────────────────────────────────────────────────────

if [[ -f "$SYSTEMD_UNIT" ]]; then
    echo "[uninstall] removing $SYSTEMD_UNIT"
    rm -f "$SYSTEMD_UNIT"
    systemctl daemon-reload
fi

if [[ -f "$INSTALL_BIN" ]]; then
    echo "[uninstall] removing $INSTALL_BIN"
    rm -f "$INSTALL_BIN"
fi

# ── purge ───────────────────────────────────────────────────────────────────

if $PURGE; then
    if [[ -d "$INSTALL_CONFIG_DIR" ]]; then
        echo "[uninstall] removing $INSTALL_CONFIG_DIR"
        rm -rf "$INSTALL_CONFIG_DIR"
    fi
    if [[ -d "$INSTALL_LOG_DIR" ]]; then
        echo "[uninstall] removing $INSTALL_LOG_DIR"
        rm -rf "$INSTALL_LOG_DIR"
    fi
    if id -u "$ADC_USER" >/dev/null 2>&1; then
        echo "[uninstall] removing user $ADC_USER"
        userdel "$ADC_USER" 2>/dev/null || true
    fi
    if getent group "$ADC_GROUP" >/dev/null; then
        echo "[uninstall] removing group $ADC_GROUP"
        groupdel "$ADC_GROUP" 2>/dev/null || true
    fi
fi

# ── drop database (bundled mode) ────────────────────────────────────────────

if $DROP_DB; then
    if systemctl is-active --quiet postgresql 2>/dev/null; then
        echo "[uninstall] dropping database $DB_NAME and role $DB_USER"
        sudo -u postgres psql -v ON_ERROR_STOP=1 <<PSQL || true
DROP DATABASE IF EXISTS $DB_NAME;
DROP ROLE IF EXISTS $DB_USER;
PSQL
    else
        echo "[uninstall] postgresql not running — skipping --drop-db"
    fi
fi

# ── remove postgresql package ───────────────────────────────────────────────

if $REMOVE_PG; then
    if [[ -r /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        case "${ID:-} ${ID_LIKE:-}" in
            *debian*|*ubuntu*)
                echo "[uninstall] removing postgresql via apt-get purge"
                export DEBIAN_FRONTEND=noninteractive
                apt-get purge -y -qq postgresql 'postgresql-*' || true
                apt-get autoremove -y -qq || true
                ;;
            *rhel*|*centos*|*rocky*|*almalinux*|*fedora*)
                echo "[uninstall] removing postgresql via dnf"
                if command -v dnf >/dev/null 2>&1; then
                    dnf remove -y -q postgresql-server postgresql-contrib || true
                else
                    yum remove -y -q postgresql-server postgresql-contrib || true
                fi
                ;;
            *)
                echo "WARNING: unsupported distro; skipping postgres removal." >&2
                ;;
        esac
    fi
fi

echo
echo "[uninstall] done."
if ! $PURGE; then
    echo "Note: $INSTALL_CONFIG_DIR and $INSTALL_LOG_DIR were preserved."
    echo "      Use --purge to remove them as well."
fi
if ! $DROP_DB; then
    echo "Note: the bundled '$DB_NAME' database was preserved."
    echo "      Use --drop-db to drop it."
fi
