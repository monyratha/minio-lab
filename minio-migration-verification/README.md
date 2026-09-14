# migration-verify — MinIO / S3 migration verification

`migration-verify` is a small, dependency-light Go CLI that independently
verifies that a bucket on a **source** S3/MinIO deployment was migrated
correctly to a **target** deployment. It does not care *how* the data was
moved (Chorus, `mc mirror`, rclone, vendor tooling…): it only speaks S3 to
both sides and produces a PASS / FAIL report in text, JSON and HTML.

```
MinIO A (source)  ──migration tool──▶  MinIO B (target)
        │                                     │
        └────────── migration-verify ─────────┘
                           │
                           ▼
          migration-report.json / migration-report.html
```

Related documents in this directory:

| File | What it is |
|------|------------|
| [CHECKLIST.md](CHECKLIST.md) | The migration verification checklist the tool implements |
| [CHORUS_FINDINGS.md](CHORUS_FINDINGS.md) | What Chorus does and does not provide for progress / status / diff / reporting |
| [TEST_RESULTS.md](TEST_RESULTS.md) | Exact commands and results of the Chorus A→B replication test and the verification runs |
| [reports/](reports/) | Recorded reports for the lab data (`migration-report*.*`) and self-test evidence, as cited in TEST_RESULTS.md |
| `out/` | Where `run-verify.sh` / `make verify` write their reports; git-ignored, overwritten on every run |
| [AGENT_HANDOFF.md](AGENT_HANDOFF.md) | Original task hand-off / environment description |

---

## 1. Installation

Requirements: Go 1.22+ (developed with Go 1.27). No other dependencies —
the only module used is the official `github.com/minio/minio-go/v7` SDK.

```bash
cd ~/minio-lab/minio-migration-verification
make build          # -> bin/migration-verify
# or
go build -o bin/migration-verify ./cmd/migration-verify
```

Run unit tests:

```bash
make test
```

Install to `$GOPATH/bin`:

```bash
go install ./cmd/migration-verify
```

Cross-compile (e.g. to run on a Linux jump host next to the storage):

```bash
GOOS=linux GOARCH=amd64 go build -o migration-verify-linux-amd64 ./cmd/migration-verify
```

Or as a container (reports are written to the mounted `/reports`):

```bash
docker build -t migration-verify .
docker run --rm --network host -v "$PWD/reports:/reports" \
  -e SOURCE_ACCESS_KEY -e SOURCE_SECRET_KEY -e TARGET_ACCESS_KEY -e TARGET_SECRET_KEY \
  migration-verify --source http://localhost:9000 --target http://localhost:9002 --bucket migration-test
```

## 2. Configuration

Credentials are read from the environment (recommended) and can be
overridden by flags. Secrets are never written into the reports.

| Env var | Flag | Meaning |
|---------|------|---------|
| `SOURCE_ENDPOINT` | `--source` | `http(s)://host:port` of the source |
| `TARGET_ENDPOINT` | `--target` | `http(s)://host:port` of the target |
| `SOURCE_ACCESS_KEY` / `SOURCE_SECRET_KEY` | `--source-access-key` / `--source-secret-key` | source credentials |
| `TARGET_ACCESS_KEY` / `TARGET_SECRET_KEY` | `--target-access-key` / `--target-secret-key` | target credentials |
| `SOURCE_REGION` / `TARGET_REGION` | `--source-region` / `--target-region` | optional region |
| `SOURCE_NAME` / `TARGET_NAME` | `--source-name` / `--target-name` | labels used in the report |

Required permissions on both sides: `s3:ListBucket`, `s3:GetObject`
(only for deep checks), `s3:GetObjectTagging`, `s3:GetBucketVersioning`,
`s3:GetBucketObjectLockConfiguration`, `s3:GetBucketTagging`,
`s3:GetBucketPolicy`, `s3:GetLifecycleConfiguration`,
`s3:GetEncryptionConfiguration`. `s3:ListAllMyBuckets` is optional (used for
connectivity and `--all-buckets`; the tool falls back to `HeadBucket`).
`--smoke-test` additionally needs `s3:PutObject` / `s3:DeleteObject` on the
target bucket.

## 3. Usage

Minimal run against the lab environment:

```bash
export SOURCE_ACCESS_KEY=minioadmin SOURCE_SECRET_KEY=minioadmin
export TARGET_ACCESS_KEY=minioadmin TARGET_SECRET_KEY=minioadmin

./bin/migration-verify \
  --source http://localhost:9000 --target http://localhost:9002 \
  --source-name "MinIO A" --target-name "MinIO B" \
  --bucket migration-test \
  --json reports/migration-report.json --html reports/migration-report.html
```

There is a convenience wrapper with the same defaults: `./run-verify.sh [extra flags]`.
It writes to `out/` instead (git-ignored), or to `REPORT_DIR` when set.

### All flags

```
--bucket NAME              bucket to verify; repeatable or comma separated;
                           use SRC:DST when the target bucket has a different name
--all-buckets              verify every bucket that exists on the source
--prefix P                 only objects under this key prefix
--level 1|2|3              verification depth (default 2, see §4)
--deep-on-inconclusive     download+hash objects whose ETags differ but are not
                           comparable (default true)
--max-deep-bytes N         cap on bytes downloaded for deep checks (0 = unlimited)
--tags                     compare object tags (default true)
--versions auto|on|off     compare version histories (default auto = when the
                           source bucket has versioning enabled/suspended)
--fail-on-extra            objects that exist only on target are FAIL (default true;
                           false = WARN)
--ignore-meta-key K        metadata header to ignore (repeatable), e.g. a header the
                           migration tool adds on the target
--list-matched             include fully matching objects in the object list
--concurrency N            parallel object comparisons (default 8)
--smoke-test               PUT/HEAD/GET/presigned-GET/DELETE a probe object on the
                           target bucket (writes to target, probe is always removed)
--smoke-test-prefix P      key prefix for the probe (default .migration-verify-smoke/)
--request-timeout D        max wait for response headers of one S3 request (default 2m)
--fail-on-warn             exit 1 when the overall status is WARN
--json PATH                JSON report path (default migration-report.json, '' = off)
--html PATH                HTML report path (default migration-report.html, '' = off)
--quiet                    no text summary on stdout
--source-insecure / --target-insecure   skip TLS verification
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | overall `PASS` (or `WARN` unless `--fail-on-warn`) |
| 1 | overall `FAIL` — at least one verification check failed |
| 2 | could not run (bad arguments, endpoint unreachable, `ERROR` status) |

This makes it usable as a gate in a migration runbook or CI job.

### Examples

```bash
# Verify every bucket, quietly, JSON only
./bin/migration-verify --source $A --target $B --all-buckets --quiet --html ''

# Migration renamed the bucket
./bin/migration-verify --source $A --target $B --bucket old-name:new-name

# Full byte-level verification of one prefix, with a 50 GiB download budget
./bin/migration-verify --source $A --target $B --bucket data --prefix 2025/ \
  --level 3 --max-deep-bytes 53687091200

# Cheapest possible pass on a huge bucket: listing only
./bin/migration-verify --source $A --target $B --bucket huge --level 1 --concurrency 32

# Ignore a header the migration tool stamps on target objects
./bin/migration-verify ... --ignore-meta-key x-amz-meta-migrated-at
```

## 4. Verification logic

The tool runs the checklist in [CHECKLIST.md](CHECKLIST.md). Work is layered
so that the expensive part (downloading data) is only done when it is
actually needed.

### Global checks

1. **Connectivity / credentials** – `ListBuckets` on both sides (fallback
   `HeadBucket`). Failure aborts the run with exit code 2.
2. **Bucket inventory** (`--all-buckets` only) – bucket names present on source
   but missing on target → FAIL; extra buckets on target → WARN.

### Per bucket

| Check | Level | How |
|-------|-------|-----|
| Bucket Exists | 1 | `HeadBucket` on both |
| Versioning | 1 | `GetBucketVersioning` status must be equal |
| Object Lock | 1 | `GetObjectLockConfiguration` must be equal |
| Bucket Tags / Policy / Lifecycle / Encryption | 1 | fetched on both sides and compared for equality (policy JSON is canonicalised first) |
| Object Listing | 1 | full recursive `ListObjectsV2` of both sides (current versions) |
| Object Count | 1 | number of keys must be equal |
| Object Names | 1 | keys only on source → **missing** (FAIL); keys only on target → **extra** (FAIL, or WARN with `--fail-on-extra=false`) |
| Object Size | 1 | from the listing |
| Checksum / ETag | 1 | see ETag rules below |
| Content Type | 2 | `HeadObject` on both |
| Metadata | 2 | `HeadObject`: all `x-amz-meta-*` user metadata plus `Cache-Control`, `Content-Encoding`, `Content-Language`, `Content-Disposition`, `x-amz-object-lock-*`, `x-amz-website-redirect-location` |
| Tags | 2 | `GetObjectTagging` on both (`--tags=false` to skip) |
| Content (SHA-256) | 3 / on demand | `GetObject` streamed through SHA-256 on both sides, hashes compared |
| Versions | auto | `ListObjectVersions` on both; per key the ordered history is compared |
| App Smoke Test | opt-in | PUT → HEAD → GET → presigned GET → DELETE of a probe object on the target; the exact version written is deleted, so versioned buckets keep no probe version or delete marker |

### ETag rules (why "checksum where reliable")

An S3 ETag is the MD5 of the content **only** for single-part, unencrypted
uploads. The tool therefore classifies each pair of ETags:

| Situation | Verdict |
|-----------|---------|
| ETags identical | **match** — content identical (even for multipart, an identical multipart ETag implies identical parts) |
| Both are plain 32-hex MD5s, no SSE header, and differ | **mismatch → FAIL** |
| Either side has a multipart ETag (`…-N`), a non-MD5 ETag, or an `x-amz-server-side-encryption` header, and they differ | **inconclusive** |

Inconclusive objects with equal sizes are, by default
(`--deep-on-inconclusive`), downloaded from both sides and compared by
SHA-256; the object then passes or fails on real content. If deep checking
is disabled or the `--max-deep-bytes` budget is exhausted, the object is
reported as `WARN etag-inconclusive` and the overall status becomes `WARN`,
never a silent PASS. A real example from the lab self-test: the same 6 MB
file uploaded with `mc cp` (plain MD5 ETag) and `mc pipe` (multipart ETag
`…-1`) was correctly recognised as identical after the SHA-256 comparison.

### Versions

Version IDs are generated by the server and cannot be preserved by any
S3-level migration, so they are reported but not compared. For each key the
tool compares the ordered sequence (oldest → newest) of versions on
`(delete-marker, size, ETag)`. This detects missing/extra versions, missing
delete markers and re-ordered histories. Enabled automatically when the
source bucket has versioning enabled or suspended.

### Status roll-up

Each check is `PASS`, `WARN`, `FAIL`, `ERROR`, `N/A` or `SKIPPED`. A bucket's
status is the worst of its checks (`N/A`/`SKIPPED` count as `PASS`); the
overall status is the worst of the global checks and all buckets.

## 5. Report format

Three renderings of the same data are produced from one run:

* **stdout text** – the summary table shown in the handoff document.
* **`migration-report.json`** – machine-readable, stable field names.
* **`migration-report.html`** – self-contained single file, no external assets, light/dark aware.

JSON structure (abridged):

```json
{
  "tool": "migration-verify", "version": "0.1.0",
  "generatedAt": "2026-09-12T16:15:02Z",
  "source": {"label": "MinIO A", "endpoint": "http://localhost:9000", "secure": false, "accessKey": "minioadmin"},
  "target": {"label": "MinIO B", "endpoint": "http://localhost:9002", "secure": false, "accessKey": "minioadmin"},
  "options": {"level": 2, "deepOnInconclusive": true, "checkTags": true, "versions": "auto", "failOnExtra": true, "...": "..."},
  "status": "PASS",
  "checks": [ {"name": "Source Connectivity", "status": "PASS", "detail": "..."} ],
  "buckets": [
    {
      "sourceBucket": "migration-test", "targetBucket": "migration-test",
      "status": "PASS",
      "checks": [ {"name": "Object Count", "status": "PASS", "source": "4", "target": "4"}, "..." ],
      "summary": {
        "sourceObjects": 4, "targetObjects": 4, "sourceBytes": 10485813, "targetBytes": 10485813,
        "matchedObjects": 4, "missingObjects": 0, "extraObjects": 0, "mismatchedObjects": 0,
        "errorObjects": 0, "etagInconclusive": 0, "deepVerified": 0, "deepBytes": 0
      },
      "objects": [
        {
          "key": "file3.bin", "status": "FAIL", "reasons": ["size", "etag"],
          "detail": "size 5 != 7; etag ae5b… != 41e9…",
          "source": {"size": 5, "etag": "ae5b…", "contentType": "text/plain", "lastModified": "…", "metadata": {"…": "…"}, "tags": {}},
          "target": {"size": 7, "etag": "41e9…", "..." : "..."}
        }
      ],
      "versionDiffs": [ {"key": "v.txt", "sourceVersions": 2, "targetVersions": 1, "reasons": ["version count 2 != 1"]} ],
      "elapsed": "12ms"
    }
  ],
  "totals": { "...same fields as summary, summed over buckets..." },
  "elapsed": "28ms",
  "limitations": ["..."]
}
```

Field notes:

* `objects[]` lists every **missing**, **extra**, **mismatched**, **error**
  and **deep-verified** object with both sides' observed attributes. Fully
  matching objects are counted in `summary.matchedObjects` and only listed
  with `--list-matched` (keeps reports small on large buckets).
* `objects[].reasons` values: `missing`, `extra`, `size`, `etag`,
  `etag-inconclusive`, `contentType`, `metadata`, `tags`, `content`,
  `deep-verified`.
* `objects[].source.sha256` / `target.sha256` are present only when the
  object was deep-verified.
* `versionDiffs[]` is present only for versioned buckets with differences.
* `limitations[]` repeats the caveats below so a report is self-explaining.

## 6. Limitations

* **ETag is not always a checksum.** Multipart uploads produce
  `md5(md5(part1)…)-N` which depends on the part size the uploading tool
  used; SSE-KMS/SSE-C objects have non-content ETags. These cases are
  detected and resolved by downloading (see §4), which costs bandwidth.
  Objects where deep verification could not run are reported as
  inconclusive (`WARN`), never PASS.
* **Version IDs are not comparable** across deployments; only the shape and
  content of the version history is compared.
* **LastModified is not compared** – on the target it is the copy time.
  If the migration tool stores the original timestamp in user metadata,
  that metadata *is* compared.
* **Bucket configuration is compared for literal equality** (after JSON key
  canonicalisation for policies). Two policies that are semantically
  equivalent but written differently will be reported as a mismatch.
* **Not compared:** ACLs / grants, per-object retention and legal hold,
  bucket notification, replication and CORS configuration, IAM users /
  policies / service accounts (out of scope for a data verification tool).
* **Memory:** keys of one bucket (both sides) are held in memory; roughly
  200 bytes per object, so 10 M objects ≈ 2 GB. Use `--prefix` to shard
  very large buckets.
* **Request volume:** level 2 issues 1 HEAD + 1 GetObjectTagging per object
  per side. On very large buckets run level 1 first, then level 2 on
  prefixes, or disable tags with `--tags=false`.
* The tool verifies a **snapshot**: if the source is still being written to,
  run it after writes are frozen (or after the migration tool's switch-over).
* **No resume:** an interrupted run must be restarted; shard very large
  buckets with `--prefix` so each run is short.
* `--smoke-test` cannot delete its probe on buckets with Object Lock in
  COMPLIANCE/GOVERNANCE mode with a default retention; the check reports
  FAIL at the DELETE step in that case.

## 7. Project layout

```
cmd/migration-verify/main.go      CLI flags, env handling, exit codes
internal/verify/config.go         Config + validation, endpoint/bucket parsing
internal/verify/client.go         minio-go client construction, connectivity ping
internal/verify/bucket.go         bucket-level configuration checks
internal/verify/objects.go        listing, HEAD, tags, ETag rules, deep SHA-256 compare
internal/verify/versions.go       version-history comparison
internal/verify/smoke.go          application-level PUT/GET/HEAD/DELETE/presign test
internal/verify/verifier.go       orchestration, per-bucket checks, limitations
internal/verify/verify_test.go    unit tests for the pure comparison logic
Dockerfile                        container build (multi-stage, static binary)
internal/report/model.go          JSON report model + status roll-up
internal/report/render.go         text / JSON / HTML renderers
reports/                          recorded reports for the lab data + self-test evidence
out/                              reports from run-verify.sh (git-ignored)
```
