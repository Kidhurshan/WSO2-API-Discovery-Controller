# Kubernetes Deployment — WSO2 API Discovery Controller

This directory contains the Kustomize-managed manifests for running ADC and its
bundled PostgreSQL database on any Kubernetes cluster (K3s, kind, EKS, GKE,
AKS, or vanilla kubeadm).

The default deployment is **self-contained**: applying this directory brings up
both ADC and a PostgreSQL 17 instance with no external dependencies. To use an
existing PostgreSQL instead, see [External PostgreSQL](#external-postgresql)
below.

---

## Contents

| File                       | Purpose                                                              |
|----------------------------|----------------------------------------------------------------------|
| `install.sh`               | Wrapper for `kubectl kustomize | kubectl apply -f -` (required — see below). |
| `kustomization.yaml`       | Kustomize entrypoint — generates ConfigMap from `../../config/config.toml`. |
| `adc-namespace.yaml`       | Creates the `adc-system` namespace.                                  |
| `adc-serviceaccount.yaml`  | ServiceAccount used by the ADC pod.                                  |
| `postgres-secret.yaml`     | PostgreSQL credentials (`POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`). |
| `postgres-pvc.yaml`        | 20Gi PersistentVolumeClaim for the database files.                   |
| `postgres-deployment.yaml` | `postgres:17-alpine` Deployment with `Recreate` strategy.            |
| `postgres-service.yaml`    | ClusterIP Service exposing port 5432 inside the cluster.             |
| `adc-deployment.yaml`      | ADC controller Deployment with health probes + env vars.             |
| `adc-service.yaml`         | ClusterIP Service for ADC's health/readiness endpoints (`:8090`).    |

The ADC `config.toml` is **not** in this directory — it's generated on each
install from the single canonical [`config/config.toml`](../../config/config.toml)
at the repo root by kustomize's `configMapGenerator`. Edit that one file to
change ADC's behavior; re-run `install.sh` and kustomize hashes the new
content into the ConfigMap name, triggering a rolling restart of the adc pod.

---

## Quick Start

```bash
# 1. Build (or pull) the ADC image and make it available to your cluster.
make docker
# For K3s: k3s ctr images import bin/adc-image.tar
# For kind: kind load docker-image wso2/adc:latest
# For remote clusters: docker push <registry>/wso2/adc:latest and update
#                      adc-deployment.yaml's image: field.

# 2. Apply everything in the right order (wrapper — see note below).
./deploy/kubernetes/install.sh

# 3. Watch the pods come up.
kubectl -n adc-system get pods -w
```

> **Why `install.sh`?** Kustomize v5's default security model blocks
> cross-directory file references, so `kubectl apply -k deploy/kubernetes/`
> cannot read `config/config.toml` directly. `install.sh` runs
> `kubectl kustomize --load-restrictor=LoadRestrictionsNone` (the flag is
> not accepted by `kubectl apply -k`) and pipes the rendered manifests to
> `kubectl apply -f -`. Use `./install.sh render` to preview without
> applying, and `./install.sh delete` to tear everything down.

You should see two pods: `postgres-...` and `adc-...`. PostgreSQL becomes ready
in ~10 seconds; ADC waits for the DB and then runs its schema migrations on
first start.

### Verify

```bash
# Logs
kubectl -n adc-system logs deploy/adc -f

# Health probe (port-forward, then curl)
kubectl -n adc-system port-forward svc/adc-health 8090:8090
curl http://localhost:8090/healthz
curl http://localhost:8090/readyz

# Database — confirm tables exist
kubectl -n adc-system exec deploy/postgres -- \
  psql -U adc_user -d adc -c '\dt'
```

You should see `adc_schema_version`, `adc_pipeline_state`, `adc_discovered_apis`,
`adc_managed_apis`, `adc_managed_api_operations`, `adc_unmanaged_apis`.

---

## Configuration

Out of the box, **all five pipeline phases are disabled**. ADC will start, run
migrations, and idle until you turn on the phases you want. Edit the canonical
[`config/config.toml`](../../config/config.toml) at the repo root and re-apply:

```bash
./deploy/kubernetes/install.sh
# The ConfigMap name is content-hashed, so kustomize rewrites the Deployment's
# configMap.name automatically and the rolling restart happens on its own.
```

### Enabling Phase 1 (Discovery)

```toml
[discovery.source]
type = "deepflow"
server_ip = "deepflow-server.deepflow.svc.cluster.local"
querier_port = 20416
```

### Enabling Phase 2 (Managed API Sync)

```toml
[managed.source]
type = "wso2_apim"
base_url = "https://apim.example.com:9443"

[managed.source.auth]
auth_type = "basic"
username = "admin"
password = "admin"
```

### Phases 3, 4, 5

Set `enabled = true` under `[comparison]`, `[spec_generation]`, and
`[service_catalog]` once Phase 2 has been verified.

---

## Credentials

Database credentials live in [`postgres-secret.yaml`](postgres-secret.yaml).
The same Secret is consumed by **both** the postgres pod (via `envFrom` in
`postgres-deployment.yaml`) and the ADC pod (via `envFrom` in
`adc-deployment.yaml`). ADC's config loader expands `${POSTGRES_DB}`,
`${POSTGRES_USER}`, and `${POSTGRES_PASSWORD}` references in `config.toml`
against the pod environment, so the credentials never appear in the ConfigMap.

To rotate:

```bash
kubectl -n adc-system edit secret postgres-secret
kubectl -n adc-system rollout restart deploy/postgres
kubectl -n adc-system rollout restart deploy/adc
```

> **Production**: change the default `CHANGE-ME-BEFORE-DEPLOYING` placeholder password
> in `postgres-secret.yaml` before exposing the cluster beyond a trusted boundary.

---

## Storage

The bundled PostgreSQL uses a 20Gi PVC ([`postgres-pvc.yaml`](postgres-pvc.yaml))
with no `storageClassName` set, so your cluster's default StorageClass is used.
To pin a specific class, uncomment the line in `postgres-pvc.yaml`:

| Cluster | Recommended class |
|---------|-------------------|
| AWS EKS | `gp3`             |
| GCP GKE | `standard-rwo`    |
| Azure AKS | `managed-csi`   |
| K3s     | `local-path`      |
| kind    | `standard`        |

20Gi is sized for 1–2 years of typical ADC growth (working set ~1–3 GB plus
indexes, WAL, and headroom). To resize after deployment, edit the PVC's
`spec.resources.requests.storage` — most cloud StorageClasses support online
expansion.

---

## External PostgreSQL

If you already operate PostgreSQL (RDS, Cloud SQL, an in-cluster operator,
etc.), skip the bundled instance:

1. **Edit [`kustomization.yaml`](kustomization.yaml)** — remove the four
   `postgres-*.yaml` lines from the `resources` list.

2. **Edit [`config/config.toml`](../../config/config.toml)** at the repo
   root — follow the `[catalog.datastore]` comment block: replace
   `${POSTGRES_HOST}` with your DB hostname (or override it via a kustomize
   overlay) and set `ssl_mode = "require"` for cloud/remote DBs.

3. **Replace [`postgres-secret.yaml`](postgres-secret.yaml)** with your own
   Secret named `postgres-secret` containing `POSTGRES_DB`, `POSTGRES_USER`,
   `POSTGRES_PASSWORD` keys — or change the `secretRef.name` in
   `adc-deployment.yaml` to point at your existing Secret.

4. **Override `POSTGRES_HOST`** in `adc-deployment.yaml` (or via a kustomize
   overlay) — change the `POSTGRES_HOST` env value from the bundled default
   `postgres.adc-system` to your external DB's hostname.

5. **Apply**:

   ```bash
   ./deploy/kubernetes/install.sh
   ```

For provisioning, TLS, backups, and cloud-provider notes, see
[`docs/external-postgres.md`](../../docs/external-postgres.md).

---

## Troubleshooting

### ADC pod stuck in `CrashLoopBackOff`

```bash
kubectl -n adc-system logs deploy/adc --previous
```

Common causes:

| Symptom in logs                              | Cause                                       | Fix                                                 |
|----------------------------------------------|---------------------------------------------|-----------------------------------------------------|
| `connect: connection refused` to `postgres`  | DB pod not ready yet                        | Wait — ADC has built-in retry, will recover.        |
| `password authentication failed`             | ConfigMap and Secret out of sync            | Rotate Secret, restart both pods.                   |
| `${POSTGRES_USER}` appears verbatim in error | ADC pod missing `envFrom` block             | Check `adc-deployment.yaml`, re-apply.              |
| `validate config: ...`                       | Bad TOML in ConfigMap                       | Fix syntax, re-apply, rollout restart.              |

### PostgreSQL pod won't schedule

```bash
kubectl -n adc-system describe pod -l app=postgres
```

Usually a missing default StorageClass. Either install one (`local-path` for
K3s, `standard` for kind) or set `storageClassName` explicitly in
`postgres-pvc.yaml`.

### Reset everything

```bash
./deploy/kubernetes/install.sh delete
# WARNING: this also deletes the PVC and all data.
```

---

## Image Distribution

For air-gapped clusters or local testing, build the image once and import it
into the node's container runtime:

```bash
make docker                                # builds wso2/adc:latest
docker save wso2/adc:latest -o adc.tar

# K3s
sudo k3s ctr images import adc.tar

# kind
kind load image-archive adc.tar

# containerd directly
sudo ctr -n=k8s.io images import adc.tar
```

For multi-node clusters, push to a registry and update the `image:` field in
[`adc-deployment.yaml`](adc-deployment.yaml).
