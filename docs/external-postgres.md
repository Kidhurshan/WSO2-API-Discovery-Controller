# Using External PostgreSQL with ADC

ADC ships with a bundled PostgreSQL by default. This guide is for operators who want to use their own PostgreSQL instance instead — typically for compliance, high availability, multi-region deployment, or integration with existing database infrastructure.

## Table of Contents

1. [When to Use External PostgreSQL](#when-to-use-external-postgresql)
2. [PostgreSQL Version Requirements](#postgresql-version-requirements)
3. [Required Permissions](#required-permissions)
4. [Provisioning the Database](#provisioning-the-database)
5. [TLS / SSL Configuration](#tls--ssl-configuration)
6. [Connection Pooling](#connection-pooling)
7. [Backup Recommendations](#backup-recommendations)
8. [Cloud Provider Notes](#cloud-provider-notes)
9. [Switching Each Deployment Mode to External PostgreSQL](#switching-each-deployment-mode-to-external-postgresql)
10. [Migrating from Bundled to External](#migrating-from-bundled-to-external)
11. [Troubleshooting](#troubleshooting)

---

## When to Use External PostgreSQL

The bundled PostgreSQL is suitable for most ADC deployments. Use an external PostgreSQL when one or more of these apply:

| Reason | Why bundled isn't enough |
|---|---|
| **Compliance (SOC2, ISO27001, HIPAA, PCI-DSS)** | Bundled DB has no audited backup story, no encryption-at-rest by default, no access logging |
| **High availability requirements** | Bundled DB is single-replica, single-PVC. Pod restart = brief downtime. No automatic failover. |
| **Multi-region deployment** | Bundled DB is local to one cluster. Cross-region replication needs operator-managed PostgreSQL or managed service |
| **Disaster recovery / RPO < 24h** | Bundled DB has no WAL archiving, no PITR (point-in-time recovery) |
| **Existing DBA team and database infrastructure** | Centralizing on managed/operator-deployed PostgreSQL fits org standards |
| **Very large scale (100K+ unmanaged APIs)** | Tuning, connection pooling, monitoring become essential |

If none of these apply, **the bundled PostgreSQL is the right choice** — simpler, fewer moving parts, and ADC's data is regenerable from DeepFlow.

---

## PostgreSQL Version Requirements

| Version | Status |
|---|---|
| PostgreSQL 17 | Recommended (latest stable) |
| PostgreSQL 16 | Supported |
| PostgreSQL 15 | Supported (minimum) |
| PostgreSQL 14 and older | Not supported |

ADC uses standard SQL features available in PostgreSQL 15+: `JSONB`, `TIMESTAMPTZ`, `ON CONFLICT`, generated columns, and `CREATE TABLE IF NOT EXISTS`. No extensions required.

---

## Required Permissions

The database user that ADC connects with needs:

- `CONNECT` on the `adc` database
- `CREATE` on the `public` schema (or whichever schema ADC uses) — needed for **auto-migration** to create tables on first startup
- Full CRUD (`SELECT`, `INSERT`, `UPDATE`, `DELETE`) on all `adc_*` tables

ADC creates the following tables automatically via the migration system:

| Table | Purpose |
|---|---|
| `adc_schema_version` | Tracks which migrations have run |
| `adc_pipeline_state` | Sliding-window watermarks per phase |
| `adc_discovered_apis` | Phase 1 output: traffic-derived API signatures |
| `adc_managed_apis` | Phase 2 output: APIs synced from APIM Publisher |
| `adc_managed_api_operations` | Phase 2 output: per-operation metadata |
| `adc_unmanaged_apis` | Phase 3 output: shadow / drift classifications + OpenAPI specs |

---

## Provisioning the Database

### Option A — Standard PostgreSQL (self-managed, on-prem, VM)

Run as the `postgres` superuser:

```sql
-- 1. Create the database
CREATE DATABASE adc;

-- 2. Create the user with a strong password
CREATE USER adc_user WITH PASSWORD 'CHANGE-ME-TO-A-STRONG-PASSWORD';

-- 3. Grant database-level privileges
GRANT ALL PRIVILEGES ON DATABASE adc TO adc_user;

-- 4. Switch into the adc database and grant schema privileges
--    (PostgreSQL 15+ revoked default CREATE on public schema for security)
\c adc
GRANT ALL ON SCHEMA public TO adc_user;
```

Verify the user can connect and create tables:

```bash
psql -h <host> -U adc_user -d adc -c "CREATE TABLE __adc_test (id INT); DROP TABLE __adc_test;"
```

If this succeeds, ADC's auto-migration will work on first startup.

### Option B — AWS RDS

```sql
-- Connect as the master user, then:
CREATE DATABASE adc;
CREATE USER adc_user WITH PASSWORD 'CHANGE-ME';
GRANT ALL PRIVILEGES ON DATABASE adc TO adc_user;
\c adc
GRANT ALL ON SCHEMA public TO adc_user;
```

RDS-specific notes:
- Master user does **not** have `SUPERUSER` — `GRANT ALL` is the right pattern
- Enable **automated backups** in the RDS console (not in this guide's scope)
- Enable **Performance Insights** for query monitoring
- Use a **parameter group** with `rds.force_ssl = 1` to require TLS

### Option C — Azure Database for PostgreSQL

Same SQL as Option A. Azure-specific notes:
- The admin username has the form `adminuser@servername` for connection strings, but inside SQL it's just `adminuser`
- Enable **TLS enforcement** in the server's networking settings (default: enabled)
- Configure **firewall rules** to allow your ADC pod/VM CIDR
- For private deployments, use **Private Link** instead of public endpoints

### Option D — GCP Cloud SQL

Same SQL as Option A. GCP-specific notes:
- The default user is `postgres` — connect with that and run the SQL above
- Use the **Cloud SQL Auth Proxy** if you don't want to manage IP allowlists
- For ADC running in GKE, use **Workload Identity** to authenticate without static credentials (advanced)

### Option E — CloudNativePG / Zalando Postgres Operator (in-cluster)

If you run a PostgreSQL operator in your Kubernetes cluster, create a `Database` (CloudNativePG) or `postgresql` (Zalando) custom resource. Both operators support per-database users and roles.

CloudNativePG example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: adc
spec:
  cluster:
    name: postgres-cluster
  name: adc
  owner: adc_user
```

Then point ADC's `[catalog.datastore].host` at the operator-managed service.

---

## TLS / SSL Configuration

ADC supports four `ssl_mode` values:

| Mode | Encryption | Cert validation | Hostname check | Use case |
|---|---|---|---|---|
| `disable` | None | — | — | Localhost only (bundled DB) |
| `require` | Yes | No | No | Quick setup, cloud DB |
| `verify-ca` | Yes | Yes | No | Stronger guarantees, no SAN match |
| `verify-full` | Yes | Yes | Yes | Production / compliance |

For external PostgreSQL, **always use at least `require`**. Use `verify-full` if you can mount the CA certificate and your hostname matches the cert SAN.

To use `verify-ca` or `verify-full`, mount the CA cert and set the `PGSSLROOTCERT` environment variable:

```bash
PGSSLROOTCERT=/etc/adc/certs/rds-ca-2019-root.pem adc --config /etc/adc/config.toml
```

For Kubernetes, mount the CA cert from a Secret into the ADC pod and set the env var via the Deployment.

---

## Connection Pooling

ADC's built-in pool (via pgx) defaults to 10 connections (`max_connections = 10` in config). For external DBs:

| ADC scale | Recommended `max_connections` |
|---|---|
| Small (<10K APIs, single phase) | 10 |
| Medium (10K–100K APIs, all phases) | 20 |
| Large (100K+ APIs, frequent cycles) | 40 |

If you run multiple ADC instances (multi-APIM deployments), consider adding **PgBouncer** in front of PostgreSQL with `pool_mode = transaction`. ADC's queries are short and don't use prepared statements across transactions, so transaction pooling works well.

---

## Backup Recommendations

ADC's data is regenerable from DeepFlow within the DeepFlow retention window — but rebuilding from scratch is slow and loses historical classifications. A backup strategy is recommended for any production deployment.

| Approach | RPO | Effort | Notes |
|---|---|---|---|
| **Cloud-managed automated backups** (RDS, Azure, GCP) | 5 min – 24 h | Click-ops | Easiest, recommended for cloud deployments |
| **`pg_dump` cron job** | 24 h | Low | Good enough for most internal deployments |
| **WAL archiving + base backups** (e.g., pgBackRest, Barman) | < 1 min | Medium | Required for low-RPO and PITR |
| **Volume snapshots** (k8s VolumeSnapshot, EBS snapshots) | Variable | Low | Crash-consistent only — verify with test restores |

Minimum recommendation: **daily `pg_dump` to off-host storage**.

```bash
# Example: daily dump to S3 via cron
0 2 * * * pg_dump -h <host> -U adc_user adc | gzip | aws s3 cp - s3://my-backups/adc-$(date +\%F).sql.gz
```

---

## Cloud Provider Notes

### AWS RDS

- Use `db.t4g.small` for small deployments, `db.m6g.large` and up for production
- Enable **Multi-AZ** for high availability
- Set the **parameter group** to require SSL: `rds.force_ssl = 1`
- Connection string host: `<instance>.<random>.<region>.rds.amazonaws.com`

### Azure Database for PostgreSQL — Flexible Server

- Use **Burstable** SKU for dev, **General Purpose** for production
- Enable **zone-redundant HA** in production
- TLS is on by default; CA cert downloadable from Azure docs
- Connection string host: `<server-name>.postgres.database.azure.com`

### GCP Cloud SQL

- Use `db-f1-micro` for dev (free tier), `db-custom-2-7680` and up for production
- Enable **automated backups** and **point-in-time recovery**
- Use **private IP** (VPC peering) to keep traffic off the public internet
- Auth options: password, IAM database authentication, Cloud SQL Auth Proxy

---

## Switching Each Deployment Mode to External PostgreSQL

### Kubernetes

1. Edit [`config/config.toml`](../config/config.toml) at the repo root — follow
   the `[catalog.datastore]` comment block: keep the `${POSTGRES_HOST}` /
   `${POSTGRES_DB}` / `${POSTGRES_USER}` / `${POSTGRES_PASSWORD}` placeholders
   (the K8s Deployment supplies them via `env` + `envFrom`), and set
   `ssl_mode = "require"` (or stronger).
2. Edit `deploy/kubernetes/kustomization.yaml` and remove these from `resources:`:
   ```
   - postgres-secret.yaml
   - postgres-pvc.yaml
   - postgres-deployment.yaml
   - postgres-service.yaml
   ```
3. Either replace `deploy/kubernetes/postgres-secret.yaml` with your own Secret
   (same name: `postgres-secret`) holding the external DB's credentials, or
   change the `secretRef.name` in `adc-deployment.yaml` to point at an existing
   Secret in the cluster.
4. Override `POSTGRES_HOST` in `adc-deployment.yaml` (or via a kustomize
   overlay): change the env value from the bundled default
   `postgres.adc-system` to your external DB's hostname.
5. Apply: `./deploy/kubernetes/install.sh`
6. Verify: `kubectl logs -n adc-system deployment/adc | grep migration`

### VM (systemd)

1. Run the installer in external mode:
   ```bash
   sudo ./deploy/systemd/install.sh --external-db
   ```
   This installs the ADC binary and systemd unit but **skips** PostgreSQL
   installation. `install.sh` prompts for the external DB's host / port / name /
   user / password / ssl-mode (or you can pass them as flags — see
   `install.sh --help`), writes the credentials to `/etc/adc/adc.env`, and
   patches `/etc/adc/config.toml` so `[catalog.datastore]` resolves to your DB.
2. (Optional) Verify the patched config:
   ```bash
   sudo cat /etc/adc/config.toml | grep -A8 '\[catalog.datastore\]'
   ```
3. `install.sh` has already started the service. Tail logs to confirm:
   ```bash
   sudo journalctl -u adc -f
   ```

### Docker Compose

1. Use the external variant:
   ```bash
   cd deploy/docker
   cp .env.external-db.example .env.external-db
   # Edit .env.external-db: set POSTGRES_HOST, POSTGRES_DB, POSTGRES_USER,
   # POSTGRES_PASSWORD to your external DB's coordinates.
   docker compose -f docker-compose.external-db.yml up -d
   ```
2. The canonical [`config/config.toml`](../config/config.toml) is bind-mounted
   into `/etc/adc/config.toml` and its `${POSTGRES_*}` placeholders are
   expanded against `.env.external-db` at startup. Set `ssl_mode = "require"`
   in `[catalog.datastore]` for cloud/remote DBs.
3. The external compose file does **not** start a postgres container — only
   ADC. ADC connects to whatever `POSTGRES_HOST` resolves to inside the
   container (see the "`POSTGRES_HOST` gotchas" section in
   [`deploy/docker/README.md`](../deploy/docker/README.md) if you run into
   connection issues).

---

## Migrating from Bundled to External

If you've been running ADC with the bundled PostgreSQL and want to move to an external one without losing state:

### 1. Provision the external database (see [Provisioning the Database](#provisioning-the-database) above).

### 2. Stop ADC

```bash
# K8s
kubectl scale -n adc-system deployment/adc --replicas=0

# VM
sudo systemctl stop adc

# Docker
docker compose stop adc
```

### 3. Dump the bundled database

```bash
# K8s — exec into the postgres pod
kubectl exec -n adc-system deployment/postgres -- \
  pg_dump -U adc_user adc > adc-backup.sql

# VM — direct dump
sudo -u postgres pg_dump adc > adc-backup.sql

# Docker
docker compose exec postgres pg_dump -U adc_user adc > adc-backup.sql
```

### 4. Restore into the external database

```bash
psql -h <external-host> -U adc_user -d adc < adc-backup.sql
```

### 5. Update ADC config to point at the external DB (see [Switching Each Deployment Mode](#switching-each-deployment-mode-to-external-postgresql) above).

### 6. Start ADC and verify

```bash
# Check that schema version is preserved
psql -h <external-host> -U adc_user -d adc -c "SELECT * FROM adc_schema_version;"

# Check that discovered APIs are intact
psql -h <external-host> -U adc_user -d adc -c "SELECT count(*) FROM adc_discovered_apis;"
```

### 7. (Optional) Tear down the bundled PostgreSQL

Once you've verified the external DB is working, you can remove the bundled postgres resources:

```bash
# K8s
kubectl delete -n adc-system deployment/postgres svc/postgres pvc/postgres-pvc secret/postgres-secret

# VM
sudo systemctl stop postgresql
sudo systemctl disable postgresql
sudo apt remove postgresql postgresql-contrib   # or distro equivalent
```

---

## Troubleshooting

### "permission denied for schema public" on first startup

PostgreSQL 15+ revoked the default `CREATE` privilege on the `public` schema. Grant it explicitly:

```sql
\c adc
GRANT ALL ON SCHEMA public TO adc_user;
```

### "FATAL: no pg_hba.conf entry for host"

Your DB doesn't allow connections from ADC's IP. Common fixes:

- **AWS RDS:** add ADC's security group to the RDS security group's inbound rules
- **Azure:** add the ADC VM/pod IP to the firewall rules
- **GCP:** add the IP to authorized networks (or use Private IP)
- **Self-managed:** edit `pg_hba.conf` and reload PostgreSQL

### "SSL connection required"

Your DB requires TLS but ADC is configured with `ssl_mode = "disable"`. Set:

```toml
ssl_mode = "require"
```

### Migration runs every startup

This means `adc_schema_version` isn't being persisted — check that the database name matches between ADC config and what you provisioned, and that the user has `INSERT` permission on the schema-version table.

### Connection pool exhausted under load

Increase `max_connections` in `[catalog.datastore]` and/or add PgBouncer. Check your DB's `max_connections` parameter — it must be greater than the sum of all client pools plus admin headroom.

---

## Support

- ADC issues / questions: [github.com/Kidhurshan/WSO2-API-Discovery-Controller/issues](https://github.com/Kidhurshan/WSO2-API-Discovery-Controller/issues)
- PostgreSQL docs: [postgresql.org/docs/current/](https://www.postgresql.org/docs/current/)
- pgx (ADC's driver) docs: [pkg.go.dev/github.com/jackc/pgx/v5](https://pkg.go.dev/github.com/jackc/pgx/v5)
