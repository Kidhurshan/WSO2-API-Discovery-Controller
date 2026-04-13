# systemd Deployment — WSO2 API Discovery Controller

This directory contains the scripts and unit file for running ADC as a
systemd-managed service on a Linux VM or bare-metal host.

The default install is **self-contained**: a single command installs
PostgreSQL locally, provisions the ADC database with a randomly generated
password, copies the binary into place, and brings the service up under
systemd. To use an existing PostgreSQL instead, pass `--external-db`.

---

## Contents

| File                    | Purpose                                                          |
|-------------------------|------------------------------------------------------------------|
| `install.sh`            | Main installer — interactive or `--yes` for CI / cloud-init.     |
| `uninstall.sh`          | Reverse the install. Conservative by default, `--purge` for full wipe. |
| `postgres-bootstrap.sh` | Helper script that installs + provisions bundled PostgreSQL.     |
| `adc.service`           | systemd unit file (installed to `/etc/systemd/system/adc.service`). |

---

## Supported Platforms

| Distribution      | Tested | Notes                                       |
|-------------------|--------|---------------------------------------------|
| Ubuntu 22.04 LTS  | yes    | Primary target.                             |
| Ubuntu 20.04 LTS  | yes    | Works.                                      |
| Debian 12         | yes    | Works.                                      |
| RHEL / Rocky 9    | yes    | Uses `dnf` and `postgresql-setup --initdb`. |
| Other systemd hosts | maybe | Manual postgres install + `--external-db`.  |

The installer requires **systemd**, **bash**, **curl**, and **python3** (used
for safe TOML editing of the `[catalog.datastore]` section).

---

## Quick Start (bundled PostgreSQL)

From the repository root:

```bash
# Build the Linux binary first (once)
make build-linux

# Install — interactive
sudo ./deploy/systemd/install.sh

# OR: install non-interactively (CI, Ansible, cloud-init, packer, etc.)
sudo ./deploy/systemd/install.sh --yes
```

What this does:

1. Installs PostgreSQL via your distro's package manager.
2. Generates a random 32-character DB password.
3. Creates the `adc` database and `adc_user` role.
4. Creates the `adc` system user.
5. Installs the binary to `/usr/local/bin/adc`.
6. Writes `/etc/adc/adc.env` (mode 0640, root:adc) with the DB credentials.
7. Writes `/etc/adc/config.toml` (mode 0640) — the same template as
   `config/config.toml` but with `[catalog.datastore]` set to:
   ```toml
   host = "localhost"
   port = 5432
   database = "${POSTGRES_DB}"
   user = "${POSTGRES_USER}"
   password = "${POSTGRES_PASSWORD}"
   ssl_mode = "disable"
   ```
   ADC's loader expands those `${VAR}` references against the environment
   that systemd injects from `/etc/adc/adc.env` — credentials never appear
   in the config file.
8. Validates the config with `adc --validate`.
9. Installs the systemd unit, enables it, starts the service.
10. Polls `/readyz` until ADC reports healthy.

After install:

```bash
systemctl status adc                   # service state
journalctl -u adc -f                   # live logs
tail -f /var/log/adc/adc.log           # file logs
curl http://127.0.0.1:8090/readyz      # health endpoint
sudo -u postgres psql -d adc -c '\dt'  # confirm tables created by auto-migration
```

Out of the box, **all five pipeline phases are disabled**. ADC will start,
run schema migrations, and idle. Edit `/etc/adc/config.toml` to enable
phases:

```bash
sudoedit /etc/adc/config.toml          # enable [discovery.source], [managed.source], etc.
sudo systemctl restart adc
```

---

## Quick Start (external PostgreSQL)

If you already operate PostgreSQL — RDS, Cloud SQL, an in-cluster operator,
on-prem, or a Patroni cluster — skip the bundled instance:

```bash
sudo ./deploy/systemd/install.sh --external-db
```

The installer will prompt for host, port, database, user, password, and
SSL mode, then verify connectivity before continuing.

For non-interactive installs (CI / Ansible / cloud-init):

```bash
sudo ./deploy/systemd/install.sh \
    --external-db --yes \
    --db-host adc-prod.cluster-abc.us-east-1.rds.amazonaws.com \
    --db-port 5432 \
    --db-name adc \
    --db-user adc_user \
    --db-password "$ADC_DB_PASSWORD" \
    --db-ssl-mode require
```

For provisioning, TLS, backups, and cloud-provider notes, see
[`docs/external-postgres.md`](../../docs/external-postgres.md).

---

## File Layout After Install

```
/usr/local/bin/adc                      # binary (mode 0755)
/etc/adc/config.toml                    # config (mode 0640, root:adc)
/etc/adc/adc.env                        # DB credentials (mode 0640, root:adc)
/var/log/adc/adc.log                    # ADC log file (rotated by lumberjack)
/etc/systemd/system/adc.service         # systemd unit
```

The `adc` system user owns `/var/log/adc/` and runs the service. Both
`/etc/adc/config.toml` and `/etc/adc/adc.env` are mode 0640 owned by
`root:adc`, so the service user can read them but they are not
world-readable.

---

## Credential Rotation

```bash
# Generate a new password
NEW_PW="$(openssl rand -base64 32 | tr -d '+/=' | head -c 32)"

# Update PostgreSQL
sudo -u postgres psql -c "ALTER ROLE adc_user WITH PASSWORD '$NEW_PW';"

# Update the env file
sudo sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$NEW_PW|" /etc/adc/adc.env

# Restart
sudo systemctl restart adc
```

The config file does not change — only `/etc/adc/adc.env` and the
PostgreSQL role.

---

## Uninstall

```bash
# Conservative — keeps /etc/adc, /var/log/adc, the database, and the adc user
sudo ./deploy/systemd/uninstall.sh

# Remove the service AND the config + logs + adc user
sudo ./deploy/systemd/uninstall.sh --purge --yes

# Full clean wipe (CI / re-test scenario — irreversible!)
sudo ./deploy/systemd/uninstall.sh --purge --drop-db --remove-postgres --yes
```

---

## Troubleshooting

### Service won't start

```bash
sudo systemctl status adc
sudo journalctl -u adc --no-pager -n 50
```

| Symptom                                       | Cause                                          | Fix                                                                  |
|-----------------------------------------------|------------------------------------------------|----------------------------------------------------------------------|
| `connect: connection refused` to localhost:5432 | postgres not running                         | `sudo systemctl status postgresql && sudo systemctl start postgresql`|
| `password authentication failed`              | `/etc/adc/adc.env` out of sync with the role   | Rotate password, update env file, restart adc.                       |
| `${POSTGRES_USER}` appears verbatim in error  | Unit file missing `EnvironmentFile=`           | Re-run `install.sh` (it reinstalls the unit).                        |
| `validate config: ...`                        | Bad TOML in `/etc/adc/config.toml`             | Edit, fix syntax, `sudo systemctl restart adc`.                      |
| `permission denied` on `/var/log/adc/adc.log` | Log dir owned by wrong user                    | `sudo chown -R adc:adc /var/log/adc`                                 |

### Re-validate the config without restarting

```bash
sudo -u adc env $(cat /etc/adc/adc.env | grep -v '^#') \
    /usr/local/bin/adc --config /etc/adc/config.toml --validate
```

### Reset everything for a clean re-install

```bash
sudo ./deploy/systemd/uninstall.sh --purge --drop-db --yes
sudo ./deploy/systemd/install.sh --yes
```

---

## Idempotency

All three scripts are safe to re-run:

- `install.sh` — detects existing postgres, existing adc user, existing
  config (backed up to `config.toml.bak.<timestamp>` before overwrite),
  reinstalls the unit, restarts the service.
- `postgres-bootstrap.sh` — `CREATE ROLE` is replaced with `ALTER ROLE` if
  the role exists; `CREATE DATABASE` is skipped if the database exists;
  `GRANT` statements are no-ops on re-run.
- `uninstall.sh` — every removal step is gated on existence checks.
