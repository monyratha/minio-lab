# MinIO Migration Verification — Agent Handoff

> **Status update (2026-09-12): all phases below are complete.**
> - Chorus A → B replication of `migration-test` done (4/4 objects, `isInitDone: true`) — see [TEST_RESULTS.md](TEST_RESULTS.md).
> - Chorus progress / status / diff / report capabilities investigated — see [CHORUS_FINDINGS.md](CHORUS_FINDINGS.md).
> - Verification checklist — [CHECKLIST.md](CHECKLIST.md).
> - Go CLI `migration-verify` built and run: overall **PASS** — [README.md](README.md), reports in [reports/](reports/).
> - Environment left running; MinIO A data untouched; proxy profile not started (not required).
>
> The original hand-off text follows unchanged for reference.


## Goal

Complete the investigation/test requested by the team lead:

1. Test Chorus for MinIO A → MinIO B replication.
2. Determine whether Chorus provides a replication/report/diff result.
3. Define a data verification checklist.
4. Build/run a verification script against the checklist.
5. Produce a useful migration verification report.

The final goal is:

```text
MinIO A
   │
   │ Chorus replication
   ▼
MinIO B
   │
   │ verification script
   ▼
Migration Verification Report
```

Do not assume Chorus is the final verification tool. The verification tool should ideally be storage/migration-method independent.

---

## Current environment

Host:

```text
macOS
Apple Silicon / ARM64
Docker Desktop
```

Working directory:

```text
~/minio-lab
```

### MinIO A

Directory:

```text
~/minio-lab/minio-a
```

Container:

```text
minio-a
```

API:

```text
http://localhost:9000
```

Console:

```text
http://localhost:9001
```

Credentials:

```text
access key: minioadmin
secret key: minioadmin
```

Image:

```text
quay.io/minio/minio:RELEASE.2025-04-22T22-12-26Z
```

### MinIO B

Directory:

```text
~/minio-lab/minio-b
```

Container:

```text
minio-b
```

API:

```text
http://localhost:9002
```

Console:

```text
http://localhost:9003
```

Credentials:

```text
access key: minioadmin
secret key: minioadmin
```

Image:

```text
quay.io/minio/minio:RELEASE.2025-04-22T22-12-26Z
```

### Shared Docker network

Created:

```text
minio-migration
```

Containers currently attached:

```text
minio-a
minio-b
```

Docker addresses:

```text
minio-a = 172.25.0.2
minio-b = 172.25.0.3
```

Inside Docker, use:

```text
http://minio-a:9000
http://minio-b:9000
```

Do NOT use localhost for these endpoints from inside Chorus containers.

---

## Test data

Bucket on MinIO A:

```text
migration-test
```

Objects:

```text
hello.txt
file1.txt
file2.txt
file3.bin
```

Current source data:

```text
hello.txt   12 B
file1.txt   19 B
file2.txt   22 B
file3.bin   10 MiB
```

Source ETags observed:

```text
hello.txt  ba4aae7b7c72dcdee71af2d5a0843d70
file1.txt  c088939e04b16763fd82c3ada1e24a7f
file2.txt  5d615834de9f1e5fb64497c6123fbb91
file3.bin  f1c9645dbc14efddc7d8a322685f26eb
```

MinIO B was empty before starting the Chorus test.

---

## Chorus repository

Cloned from:

```text
https://github.com/clyso/chorus
```

Directory:

```text
~/minio-lab/chorus
```

Docker Compose directory:

```text
~/minio-lab/chorus/docker-compose
```

Important files:

```text
docker-compose.yml
s3-credentials.yaml
worker-conf.yaml
README.md
```

---

## Chorus Docker setup

The Compose file was modified to use the external network:

```yaml
networks:
  minio-migration:
    external: true
```

The following services use that network:

```text
redis
worker
web-ui
```

`docker compose config` succeeds.

Current Chorus services:

```text
redis    → UP
worker   → UP
web-ui   → UP
```

Ports:

```text
web-ui → localhost:8080
worker HTTP → localhost:9671
worker gRPC → localhost:9670
```

---

## Chorus S3 configuration

Current `s3-credentials.yaml` has:

```yaml
storage:
  main: main

  storages:
    main:
      type: S3
      address: "http://minio-a:9000"
      credentials:
        user1:
          accessKeyID: minioadmin
          secretAccessKey: minioadmin
      provider: Minio
      healthCheckInterval: 10s
      httpTimeout: 1m
      isSecure: false
      defaultRegion: ""
      rateLimit:
        enable: true
        rpm: 600

    follower:
      type: S3
      address: "http://minio-b:9000"
      credentials:
        user1:
          accessKeyID: minioadmin
          secretAccessKey: minioadmin
      provider: Minio
      healthCheckInterval: 10s
      httpTimeout: 1m
      isSecure: false
      defaultRegion: ""
      rateLimit:
        enable: true
        rpm: 600
```

---

## Chorus worker configuration

Current `worker-conf.yaml`:

```yaml
api:
  enabled: true
  grpcPort: 9670
  httpPort: 9671
  secure: false

log:
  json: false
  level: info

metrics:
  enabled: false
  port: 9090

trace:
  enabled: false
  endpoint:

redis:
  address: "redis:6379"

features:
  tagging: false
  acl: false
  lifecycle: false
  policy: false
  preserveACLGrants: false
```

---

## Chorus worker status

Worker logs show successful startup:

```text
app redis connected
registered S3 workers
registered S3 versioned workers
registered diff fix s3 workers
registered diff workers
registered diff fix workers
management api created
starting workers...
server: start serving
```

So the worker is running correctly.

There are also repeated:

```text
NotImplemented: proxy is not listening
GetProxyCredentials
```

errors.

These appear to come from the Web UI requesting proxy functionality while the proxy service is not running.

The official Chorus README says that when using real S3 storages, the proxy profile is used:

```bash
docker-compose -f ./docker-compose/docker-compose.yml --profile proxy up
```

Investigate whether the proxy is required for the replication test or only for live-change capture / routing.

---

## Chorus official workflow discovered

The official README gives this flow:

```bash
chorctl storage

chorctl repl buckets -u user1 -f main -t follower

chorctl repl add -u user1 -b test -f main -t follower

chorctl dash
```

For all buckets:

```bash
chorctl repl add -u user1 -f main -t follower
```

Then verify the destination using an S3 client.

---

# What the agent should do next

## Phase 1 — Install/check chorctl

Check:

```bash
chorctl --help
```

If missing:

```bash
brew install clyso/tap/chorctl
```

Then:

```bash
chorctl --help
```

Determine how it connects to the local Chorus worker/API.

---

## Phase 2 — Verify Chorus storage configuration

Run:

```bash
chorctl storage
```

Expected logical configuration:

```text
main      http://minio-a:9000
follower  http://minio-b:9000
```

If it cannot connect, inspect Chorus API configuration and logs.

Do not modify MinIO data unnecessarily.

---

## Phase 3 — Start proxy if required

If Chorus requires proxy for the intended migration workflow, restart Compose with:

```bash
docker compose --profile proxy up -d
```

Make sure the proxy also joins:

```text
minio-migration
```

If necessary, update `docker-compose.yml` so proxy uses the external network.

Then:

```bash
docker compose ps
docker compose logs proxy --tail=100
```

---

## Phase 4 — Test replication

Use the existing:

```text
migration-test
```

bucket.

First verify:

```bash
mc ls --summarize minio-a/migration-test
mc ls --summarize minio-b/migration-test
```

Expected:

```text
A = 4 objects
B = 0 objects
```

Find available buckets:

```bash
chorctl repl buckets -u user1 -f main -t follower
```

Then add replication:

```bash
chorctl repl add -u user1 -b migration-test -f main -t follower
```

Monitor:

```bash
chorctl dash
```

Wait until replication completes.

---

## Phase 5 — Verify destination

Run:

```bash
mc ls --summarize minio-b/migration-test
```

Then compare:

```bash
mc stat minio-a/migration-test/hello.txt
mc stat minio-b/migration-test/hello.txt

mc stat minio-a/migration-test/file1.txt
mc stat minio-b/migration-test/file1.txt

mc stat minio-a/migration-test/file2.txt
mc stat minio-b/migration-test/file2.txt

mc stat minio-a/migration-test/file3.bin
mc stat minio-b/migration-test/file3.bin
```

Record:

* object name
* size
* ETag
* content type
* metadata
* tags if enabled
* versions if versioning is tested

---

# Required investigation: Chorus reporting

The team lead specifically asked:

> Does Chorus provide a report after replication?

Investigate this carefully.

Check:

```bash
chorctl --help
chorctl repl --help
chorctl dash --help
```

Also inspect the Chorus repository for:

```text
diff
report
integrity
verification
replication status
```

Useful searches:

```bash
grep -Rni "diff" .
grep -Rni "integrity" .
grep -Rni "report" .
grep -Rni "repl" cmd tools docs test service pkg
```

Determine exactly what Chorus provides:

1. Replication progress?
2. Replication completion status?
3. Object counts?
4. Diff between source and destination?
5. Integrity verification?
6. Exportable report?
7. JSON/API output?
8. CLI-only status?

Do not call something a "report" unless it can actually be consumed/exported or clearly provides report-like verification information.

---

# Required verification checklist

Create a migration verification checklist.

Minimum:

## 1. Connectivity

* Source reachable
* Destination reachable
* Credentials valid
* API access works

## 2. Bucket verification

* Same bucket count
* Same bucket names
* Versioning configuration
* Object Lock configuration
* Lifecycle configuration
* Encryption configuration
* Tags/policies/ACLs where applicable

## 3. Object verification

For every object:

* Same object key/name
* Same object count
* No missing objects
* No unexpected extra objects
* Same size
* Same ETag/checksum where meaningful
* Same Content-Type
* Same relevant metadata
* Same tags

## 4. Version verification

If versioning is enabled:

* Same version count
* No missing versions
* No unexpected versions
* Delete markers match

## 5. Application-level verification

Test:

* PUT
* GET
* HEAD
* DELETE
* presigned URL if relevant

---

# Verification tool

Build a small Go CLI.

Preferred goal:

```bash
migration-verify \
  --source http://minio-a:9000 \
  --target http://minio-b:9000 \
  --source-access-key minioadmin \
  --source-secret-key minioadmin \
  --target-access-key minioadmin \
  --target-secret-key minioadmin \
  --bucket migration-test
```

However, do not hardcode credentials.

Prefer environment variables:

```bash
SOURCE_ACCESS_KEY
SOURCE_SECRET_KEY
TARGET_ACCESS_KEY
TARGET_SECRET_KEY
```

The tool should produce machine-readable output.

Example:

```text
migration-report.json
```

And preferably:

```text
migration-report.html
```

---

# Verification strategy

Do NOT download every object by default.

Use layered verification:

### Level 1 — Cheap checks

* bucket names
* object count
* object names
* object sizes

### Level 2 — Metadata/checksum

* ETag/checksum
* Content-Type
* metadata
* tags

### Level 3 — Deep content verification

Only when required or when metadata/checksum comparison indicates a mismatch.

For large migration datasets, downloading every object just to verify migration would be unnecessarily expensive.

---

# Example report

The final report should look approximately like:

```text
MinIO Migration Verification
============================

Source: MinIO A
Target: MinIO B
Bucket: migration-test

Connectivity       PASS
Bucket             PASS
Object Count       PASS
Object Names       PASS
Object Size        PASS
Checksum / ETag    PASS
Metadata           PASS
Tags               PASS
Versions           N/A

Missing Objects    0
Extra Objects      0
Mismatched Objects 0

Overall             PASS
```

JSON should contain enough detail to identify individual failures.

Example:

```json
{
  "source": "MinIO A",
  "target": "MinIO B",
  "bucket": "migration-test",
  "status": "PASS",
  "summary": {
    "sourceObjects": 4,
    "targetObjects": 4,
    "missingObjects": 0,
    "extraObjects": 0,
    "mismatchedObjects": 0
  }
}
```

---

# Important constraints

* Do not destroy or recreate MinIO A data.
* Do not delete the source bucket.
* Do not use `mc mirror` as the final migration solution; this task is specifically testing Chorus.
* Do not blindly enable every Chorus feature.
* Keep the first test small and reproducible.
* Preserve the current test bucket/data.
* Prefer minimal changes to the existing Chorus Compose setup.
* Document every configuration change.
* Record exact commands and results.
* If Chorus has a built-in diff/integrity feature, test it and document it before implementing duplicate functionality.

---

# Final deliverables

The agent should finish with:

1. Successful Chorus A → B replication test.
2. Evidence that `migration-test` was replicated.
3. Findings about Chorus replication progress/report/diff capabilities.
4. Migration verification checklist.
5. Go verification CLI.
6. JSON verification report.
7. Optional HTML report.
8. README explaining how to run everything.
9. Exact test results and any limitations.

Final architecture:

```text
             ┌──────────────┐
             │   MinIO A    │
             │   :9000      │
             └──────┬───────┘
                    │
                    │ Chorus
                    ▼
             ┌──────────────┐
             │   MinIO B    │
             │   :9002      │
             └──────┬───────┘
                    │
                    │ Verification
                    ▼
          ┌─────────────────────┐
          │ migration-verify    │
          │ Go CLI               │
          └──────────┬──────────┘
                     │
                     ▼
              JSON / HTML Report
```

