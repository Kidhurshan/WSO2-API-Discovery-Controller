# WSO2 API Discovery Controller — Docker Compose Deployment

This directory contains everything needed to run ADC under Docker Compose,
with two supported topologies:

| Topology       | Compose file                       | Postgres                | Use case                                |
|----------------|------------------------------------|-------------------------|-----------------------------------------|
| **Bundled**    | `docker-compose.yml` (default)     | Runs as a sidecar       | Local dev, CI, single-host POCs         |
| **External**   | `docker-compose.external-db.yml`   | Your own (cloud / VM)   | Pre-existing managed PostgreSQL         |

The two topologies share the same image, the same env-var-driven config
loader, and the same `${POSTGRES_*}` placeholder convention used by the
Kubernetes and systemd deployments — only the surrounding wiring differs.

---

## File inventory

| File                              | Purpose                                                            |
|-----------------------------------|--------------------------------------------------------------------|
| `Dockerfile`                      | Multi-stage build (golang:1.25 → distroless static)                |
| `docker-compose.yml`              | Bundled topology (postgres:17-alpine + adc).                       |
| `docker-compose.external-db.yml`  | External-DB topology (adc only).                                   |
| `.env.example`                    | Credentials template for bundled mode. **Copy to `.env`**.          |
| `.env.external-db.example`        | Credentials template for external mode. **Copy to `.env.external-db`**. |
| `README.md`                       | This file.                                                         |

`.env` and `.env.external-db` are gitignored — never commit them.

The ADC config is **not** in this directory. Both compose files bind-mount
the single canonical [`config/config.toml`](../../config/config.toml) at
the repo root into `/etc/adc/config.toml`. Edit that one file and
`docker compose restart adc` — the same file drives the VM and Kubernetes
deployments too.

---

## Quick start — bundled PostgreSQL (default)

```bash
cd deploy/docker/

# 1. Create credentials file
cp .env.example .env
# Edit .env and replace POSTGRES_PASSWORD with a strong value

# 2. Start the stack
docker compose up -d

# 3. Watch ADC come up
docker compose logs -f adc
```

What happens:

1. `postgres` container starts on the internal `adc-net` bridge network.
   Port 5432 is **not** published to the host — only the `adc` container
   can reach it via Docker's embedded DNS.
2. The `pg_isready` healthcheck waits for postgres to accept connections.
3. `depends_on: condition: service_healthy` blocks `adc` from starting
   until postgres is ready.
4. `adc` starts, ADC's config loader expands `${POSTGRES_DB}`,
   `${POSTGRES_USER}`, `${POSTGRES_PASSWORD}` against the env vars from
   `.env`, connects to `postgres:5432`, runs the embedded schema
   migrations, and begins idling. All five pipeline phases are
   **disabled** by default (see "Enabling phases" below).
5. `adc`'s healthcheck polls `http://127.0.0.1:8090/readyz` until it
   returns 200.

Verify both containers are healthy:

```bash
docker compose ps
# Both should show "Up" with "(healthy)"

curl http://localhost:8090/healthz
curl http://localhost:8090/readyz
```

Stop the stack (data persists in the `adc-pgdata` named volume):

```bash
docker compose down
```

Stop **and** wipe the database:

```bash
docker compose down -v
```

---

## Quick start — external PostgreSQL

Use this when you have your own PostgreSQL — AWS RDS, Azure Database for
PostgreSQL, GCP Cloud SQL, on-prem cluster, etc.

```bash
cd deploy/docker/

# 1. Create credentials file
cp .env.external-db.example .env.external-db
# Edit .env.external-db with your real DB host, port, db, user, password

# 2. (Optional) Edit ../../config/config.toml to enable phases or tune
#    datastore settings. For external DBs, set ssl_mode = "require" in
#    [catalog.datastore]; POSTGRES_HOST is supplied by .env.external-db.

# 3. Start ADC
docker compose -f docker-compose.external-db.yml up -d
docker compose -f docker-compose.external-db.yml logs -f adc
```

### Provisioning the external database

ADC will auto-create its tables on first startup, but the database and
the user must already exist. Run this on your DB once before starting ADC:

```sql
CREATE USER adc_user WITH PASSWORD '<strong-password>';
CREATE DATABASE adc OWNER adc_user;
\c adc
GRANT ALL ON SCHEMA public TO adc_user;   -- PostgreSQL 15+ requires this
```

Then put `adc`, `adc_user`, and the password into `.env.external-db`.

### `POSTGRES_HOST` gotchas

`POSTGRES_HOST` resolves **inside the adc container**, not on your host.
The following will NOT work:

| Value                  | Outcome                                                |
|------------------------|--------------------------------------------------------|
| `localhost`            | Resolves to the adc container itself — connection refused |
| `127.0.0.1`            | Same as above                                           |

Use one of these instead:

| Where postgres is running        | Recommended `POSTGRES_HOST`                           |
|----------------------------------|-------------------------------------------------------|
| Cloud DB (AWS/Azure/GCP)         | The DB's public hostname                              |
| On-prem on a different machine   | LAN hostname or IP                                    |
| Same host, Docker Desktop        | `host.docker.internal`                                |
| Same host, Linux native docker   | The host's LAN IP (e.g., `192.168.1.10`)              |

---

## Enabling pipeline phases

All pipeline phases are disabled by default. To enable one, edit the
canonical [`../../config/config.toml`](../../config/config.toml) at the
repo root — for example, set `enabled = true` under `[comparison]` or
fill in the `[managed.source]` section for Phase 2 — then restart ADC:

```bash
# bundled
docker compose restart adc

# external
docker compose -f docker-compose.external-db.yml restart adc
```

The bind-mount means edits are picked up by `restart` — no rebuild needed.
The same file drives the Kubernetes and systemd deployments, so any change
you make here applies everywhere once re-deployed.

---

## Credential rotation

### Bundled mode

`POSTGRES_PASSWORD` is consumed by the postgres entrypoint **only on
first init**. Editing `.env` and re-running `up -d` will NOT reset the
existing role's password. To rotate:

```bash
# 1. Change the password in postgres
docker compose exec postgres \
  psql -U adc_user -d adc -c "ALTER USER adc_user PASSWORD '<new-password>'"

# 2. Update .env to match
$EDITOR .env

# 3. Restart adc so it picks up the new env
docker compose restart adc
```

### External mode

Rotate at your DB provider, update `.env.external-db`, then:

```bash
docker compose -f docker-compose.external-db.yml restart adc
```

---

## Building the image

The compose files build from the local source tree by default
(`build.context: ../..`). To rebuild after a Go code change:

```bash
docker compose build adc
docker compose up -d adc
```

To use a pre-built image instead, comment out the `build:` block and
ensure the `image:` line points at your registry:

```yaml
adc:
  # build:
  #   context: ../..
  #   dockerfile: deploy/docker/Dockerfile
  image: ghcr.io/your-org/adc:1.0.0
```

---

## Networking

Both compose files declare the `adc-net` bridge network with a fixed
name. The bundled stack keeps everything internal (postgres is unreachable
from the host); the external stack only contains the `adc` service but
keeps the network declaration so you can attach sidecars later if needed.

The host-facing surface is one TCP port:

| Port  | Purpose                          |
|-------|----------------------------------|
| 8090  | ADC health server (`/healthz`, `/readyz`) |

If you need to expose ADC behind a reverse proxy, attach an nginx /
traefik service to `adc-net` rather than publishing additional ports.

---

## Troubleshooting

| Symptom                                                | Likely cause                                                  | Fix                                                                                          |
|--------------------------------------------------------|----------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `adc` exits immediately, logs show `parse config file` | A `${VAR}` reference is unset                                 | Verify `.env` contains `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`                   |
| `password authentication failed for user "adc_user"`   | Bundled: `.env` was edited *after* first init                 | See "Credential rotation" above                                                              |
| `dial tcp postgres:5432: i/o timeout` (bundled)        | `pg_isready` healthcheck never went green                     | `docker compose logs postgres` — usually a permission/PVC issue                              |
| `dial tcp <host>:5432: connect: connection refused` (external) | `POSTGRES_HOST` is `localhost`/`127.0.0.1`, or DB not reachable from container | Use the host's LAN IP / `host.docker.internal` / cloud hostname                              |
| `adc` healthcheck stuck in `(starting)` for >2 minutes | Migrations failed or DB unreachable                           | `docker compose logs adc` for the actual error                                               |
| Port 8090 already in use                               | Another process bound to 8090 on the host                     | Edit the `ports:` line to remap (e.g., `"18090:8090"`)                                       |
| Want to inspect the database directly                  | —                                                              | `docker compose exec postgres psql -U adc_user -d adc`                                       |

---

## Relationship to other deployment modes

| Concern                   | Docker Compose                | Kubernetes (`deploy/kubernetes/`)         | systemd (`deploy/systemd/`)             |
|---------------------------|-------------------------------|-------------------------------------------|------------------------------------------|
| Credentials source        | `.env` file                   | `postgres-secret.yaml` Secret             | `/etc/adc/adc.env` (mode 0640)          |
| ADC reads credentials via | `${POSTGRES_*}` in TOML       | `${POSTGRES_*}` in TOML                   | `${POSTGRES_*}` in TOML                  |
| Credential injection      | compose `env_file:`           | `envFrom: secretRef:`                     | systemd `EnvironmentFile=`               |
| Bundled DB image          | `postgres:17-alpine`          | `postgres:17-alpine`                      | distro package via `postgres-bootstrap.sh` |
| Bundled DB storage        | named volume `adc-pgdata`     | PVC (default 20Gi)                        | `/var/lib/postgresql/<version>/main`     |
| External DB switch        | `-f docker-compose.external-db.yml` | Remove `postgres-*` from kustomization | `install.sh --external-db`               |

The same `internal/config/envvar.go` env var expander handles all three.
