# Ephemeral chat persistence in the EKS pod (Option 2)

Date: 2026-09-15
Status: proposed

## Problem

Locally, chat history persists to the `postgres:16-alpine` container in
`deploy/docker/docker-compose.yml`. In the EKS deployment there is no database:
`deploy/k8s/base/chat-api.yaml` sets no `KA_POSTGRES_DSN`, so the pod falls back
to the config default (`postgres://ka:ka@localhost:5432/ka`), which points at a
`localhost` that does not exist in the pod. chat-api's single startup connect
fails, it logs `postgres unavailable; chats will not persist`
(`services/chat-api/cmd/server/main.go:212-220`), leaves `repository == nil`,
and never even registers the `/v1/chats*` routes. Result: chat works turn to
turn within a live session, but nothing is durable and the history sidebar has
no backend.

## Goal

Chat history is preserved for as long as the daily stack is up, and discarded
when the stack comes down. Explicitly **lifecycle-coupled to the stack**, not
durable across teardown. No cross-session history is a requirement, not a
limitation.

## Non-goals

- No managed AWS database (RDS / Aurora Serverless). Those are durable stores;
  making them ephemeral means destroying them nightly for no benefit over an
  in-cluster Postgres, at added cost and slower `day.sh up`/`down`. Rejected in
  the cost discussion that led here.
- No change to `terraform/` (foundation or daily). This is a k8s-base-only
  change.
- No application code change. The persistence layer already exists and is
  gated on `KA_POSTGRES_DSN`.

## Design

Add an in-cluster Postgres to `deploy/k8s/base`, on a ClusterIP Service, and
point chat-api at it. Storage is **`emptyDir`** so the data lives and dies with
the pod (and therefore the stack) — zero EBS cost, nothing to clean up, no risk
of an orphaned volume outliving `terraform destroy`.

### 1. New manifest: `deploy/k8s/base/postgres.yaml`

A single-replica Deployment (not a StatefulSet — with `emptyDir` there is no
per-replica volume to template, and we run exactly one replica). Credentials
match the local default DSN (`ka`/`ka`/`ka`); the data is throwaway and the
Service is ClusterIP-only, reachable only from inside the namespace.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: knowledge-assistant
spec:
  replicas: 1
  strategy: { type: Recreate }   # one writer to one emptyDir; never two pods
  selector:
    matchLabels: { app: postgres }
  template:
    metadata:
      labels: { app: postgres }
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          env:
            - { name: POSTGRES_USER,     value: ka }
            - { name: POSTGRES_PASSWORD, value: ka }
            - { name: POSTGRES_DB,       value: ka }
            # Data under a subdir of the mount; the image refuses an existing
            # lost+found at the mount root, which emptyDir does not create but
            # this is the documented-safe placement anyway.
            - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }
          ports: [{ containerPort: 5432 }]
          volumeMounts:
            - { name: data, mountPath: /var/lib/postgresql/data }
          readinessProbe:
            exec: { command: ["pg_isready", "-U", "ka", "-d", "ka"] }
            initialDelaySeconds: 5
            periodSeconds: 5
          livenessProbe:
            exec: { command: ["pg_isready", "-U", "ka", "-d", "ka"] }
            initialDelaySeconds: 15
            periodSeconds: 10
          resources:
            requests: { cpu: "100m", memory: "128Mi" }
            limits:   { cpu: "500m", memory: "256Mi" }
      volumes:
        - name: data
          emptyDir: {}          # dies with the pod == dies with the stack
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: knowledge-assistant
spec:
  selector: { app: postgres }
  ports:
    - { port: 5432, targetPort: 5432 }
```

### 2. Point chat-api at it + gate its start on Postgres readiness

Two edits to `deploy/k8s/base/chat-api.yaml`:

**a. Set the DSN** (add to the `env:` list), so it no longer relies on the
`localhost` default:

```yaml
            - name: KA_POSTGRES_DSN
              value: postgres://ka:ka@postgres:5432/ka?sslmode=disable
```

**b. Block startup until Postgres accepts connections.** This is the crux:
chat-api connects exactly once at boot and, on failure, degrades to
no-persistence *for the life of the pod*. An initContainer makes the main
container wait, so `repo.New` always succeeds and `schema.sql` runs.

```yaml
      initContainers:
        - name: wait-for-postgres
          image: postgres:16-alpine   # already pulled for the DB; has pg_isready
          command: ["sh", "-c"]
          args:
            - |
              until pg_isready -h postgres -p 5432 -U ka -d ka; do
                echo "waiting for postgres..."; sleep 2
              done
```

(No app change; no schema/migration Job — `repo.New` execs the idempotent
`schema.sql` on connect, exactly as it does against local docker-compose.)

### 3. Register it in kustomization

Add `postgres.yaml` to `deploy/k8s/base/kustomization.yaml` `resources:`, before
`chat-api.yaml`, so ordering reads DB-before-consumer (kustomize apply order is
cosmetic here — the initContainer is what actually enforces readiness — but keep
the list honest).

## Lifecycle behavior

- **Pod restart / rollout of chat-api:** history is preserved. The Postgres pod
  keeps running; only chat-api reconnects.
- **Postgres pod restart or reschedule:** history is **lost** (`emptyDir` is
  tied to the pod). `strategy: Recreate` plus a single replica keeps this to
  genuine pod loss (node drain, OOM), not routine chat-api deploys. Acceptable
  for a demo; see the variant below if restart-durability is wanted.
- **`deploy.sh down`:** `kubectl delete namespace knowledge-assistant` removes
  the Deployment, its pod, and the `emptyDir` with it. Nothing to clean up, no
  EBS volume to orphan ahead of `terraform destroy`. This is the whole point of
  choosing `emptyDir` over a PVC.

## Variant: survive pod restarts (only if wanted later)

Swap the Deployment for a StatefulSet with `volumeClaimTemplates` (a small gp3
PVC) and set `persistentVolumeClaimRetentionPolicy: { whenDeleted: Delete }`.
Trade-offs: history survives a Postgres pod reschedule; costs a few cents of EBS
while up; and cleanup now depends on the namespace delete completing (which
deletes the PVC, whose default gp3 reclaim policy deletes the EBS volume)
*before* `terraform destroy` removes the nodes/CSI driver — a small orphan-EBS
timing risk that `emptyDir` avoids entirely. Not recommended unless restart
durability becomes a real need.

## Security

- Credentials `ka`/`ka` match the local default and are acceptable because the
  data is throwaway and the Service is ClusterIP-only (no ingress path; the ALB
  routes only to the UI Service). If we later want to avoid a literal password
  in the manifest, move `POSTGRES_PASSWORD` and the password half of
  `KA_POSTGRES_DSN` into a Secret — optional hardening, not required for the
  demo.
- No new IAM, no Pod Identity, no Secrets Manager entry. chat-api's existing
  service account is untouched.

## Cost

~$0. `emptyDir` uses node ephemeral disk (already paid for via the node), no EBS
provisioned, no managed-DB bill. The Postgres pod's CPU/memory requests fit the
existing daily nodes.

## Verification

1. **Offline gate:** `make k8s-check` must pass (renders `kubectl kustomize
   base` with no cluster/AWS). Confirms the new manifest and kustomization edit
   are valid.
2. **Live smoke (on a running daily stack):**
   - `kubectl -n knowledge-assistant get pods` shows `postgres` Ready and
     chat-api's `wait-for-postgres` init completing.
   - chat-api logs `postgres connected` (not the "will not persist" warning).
   - In the UI: send a message, reload — the chat appears in the sidebar; the
     `/v1/chats` endpoints now respond (they are only registered when the repo
     is non-nil).
   - `kubectl -n knowledge-assistant exec deploy/postgres -- psql -U ka -d ka
     -c 'select count(*) from chats;'` returns a row.
3. **Teardown check:** after `deploy.sh down`, the namespace (and thus the DB)
   is gone; a fresh `up` starts with empty history.

## Rollback

Remove `postgres.yaml` from the kustomization and drop the `KA_POSTGRES_DSN`
env + initContainer from `chat-api.yaml`. chat-api reverts to the current
no-persistence behavior. No data migration to unwind (there is no durable data).

## Files touched

- `deploy/k8s/base/postgres.yaml` (new)
- `deploy/k8s/base/chat-api.yaml` (add env + initContainer)
- `deploy/k8s/base/kustomization.yaml` (add resource)
