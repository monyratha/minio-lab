# Migration verification checklist

Checklist for verifying a MinIO Community → MinIO Enterprise (or any S3 →
S3) migration. Column **Tool** says whether `migration-verify` covers the
item automatically (✅), partially (◐), or whether it is a manual /
out-of-scope item (—).

## 1. Connectivity

| # | Check | Tool | How |
|---|-------|------|-----|
| 1.1 | Source endpoint reachable | ✅ | `ListBuckets` / `HeadBucket` — check *Source Connectivity* |
| 1.2 | Target endpoint reachable | ✅ | *Target Connectivity* |
| 1.3 | Credentials valid on both sides | ✅ | same calls; an auth error fails the check (exit 2) |
| 1.4 | API access works (list, head, get) | ✅ | exercised by every later check |
| 1.5 | TLS certificates valid | ◐ | verified by default; `--*-insecure` disables |

## 2. Bucket verification

| # | Check | Tool | How |
|---|-------|------|-----|
| 2.1 | Same bucket count / names | ✅ | `--all-buckets` → *Bucket Inventory* (missing = FAIL, extra = WARN) |
| 2.2 | Bucket exists on target | ✅ | *Bucket Exists* |
| 2.3 | Versioning configuration equal | ✅ | *Versioning* |
| 2.4 | Object Lock configuration equal | ✅ | *Object Lock* |
| 2.5 | Lifecycle configuration equal | ✅ | *Lifecycle* (literal comparison) |
| 2.6 | Encryption configuration equal | ✅ | *Encryption* |
| 2.7 | Bucket tags equal | ✅ | *Bucket Tags* |
| 2.8 | Bucket policy equal | ✅ | *Bucket Policy* (canonicalised JSON) |
| 2.9 | ACLs | — | not compared (MinIO has limited ACL support; Chorus `features.acl` was off) |
| 2.10 | Notification / replication / CORS config | — | manual: `mc event list`, `mc replicate ls`, `mc cors get` |
| 2.11 | Quotas | — | manual: `mc quota info` |

## 3. Object verification (every object)

| # | Check | Tool | How |
|---|-------|------|-----|
| 3.1 | Same object key / name | ✅ | *Object Names* |
| 3.2 | Same object count | ✅ | *Object Count* |
| 3.3 | No missing objects (on source, not on target) | ✅ | listed per key as `missing` |
| 3.4 | No unexpected extra objects on target | ✅ | listed per key as `extra` (FAIL, or WARN with `--fail-on-extra=false`) |
| 3.5 | Same size | ✅ | *Object Size* |
| 3.6 | Same ETag / checksum where meaningful | ✅ | *Checksum / ETag* — plain MD5 ETags compared directly; multipart/encrypted resolved by SHA-256 download (see README §4) |
| 3.7 | Same Content-Type | ✅ | *Content Type* (level ≥ 2) |
| 3.8 | Same user metadata (`x-amz-meta-*`) and content headers | ✅ | *Metadata* (level ≥ 2) |
| 3.9 | Same tags | ✅ | *Tags* (level ≥ 2, `--tags`) |
| 3.10 | Same content (byte level) | ✅ | *Content (SHA-256)* — level 3, or automatically for inconclusive ETags |
| 3.11 | Storage class | ◐ | recorded in the report per object, not a pass/fail criterion |
| 3.12 | Per-object retention / legal hold | — | manual: `mc retention info`, `mc legalhold info` |
| 3.13 | Original LastModified preserved | — | impossible over S3 API; check migration tool's metadata option if needed |

## 4. Version verification (versioned buckets)

| # | Check | Tool | How |
|---|-------|------|-----|
| 4.1 | Same number of versions per key | ✅ | *Versions* + `versionDiffs[]` |
| 4.2 | No missing versions | ✅ | ordered `(delete-marker,size,ETag)` history compared |
| 4.3 | No unexpected versions | ✅ | same |
| 4.4 | Delete markers match | ✅ | same |
| 4.5 | Version IDs identical | — | not possible: IDs are server-generated |

## 5. Application-level verification

| # | Check | Tool | How |
|---|-------|------|-----|
| 5.1 | PUT on target | ✅ | `--smoke-test` |
| 5.2 | HEAD on target | ✅ | `--smoke-test` |
| 5.3 | GET on target (body equals what was PUT) | ✅ | `--smoke-test` |
| 5.4 | Presigned URL works | ✅ | `--smoke-test` (presigned GET fetched over plain HTTP) |
| 5.5 | DELETE on target | ✅ | `--smoke-test` (probe object is removed, absence verified) |
| 5.6 | Application end-to-end test with real client config pointed at target | — | manual, application specific |
| 5.7 | Performance / throughput acceptable | — | manual (`mc admin speedtest`, application load test) |

## 6. Migration-tool level (Chorus) — informational

These are not data verification, but should be recorded in the migration
report. See [CHORUS_FINDINGS.md](CHORUS_FINDINGS.md).

| # | Item | How |
|---|------|-----|
| 6.1 | Replication reached `isInitDone: true`, `initObjDone == initObjListed` | `chorctl repl` / `POST /replication` |
| 6.2 | Event queue drained (`events == eventsDone`, lag 0) | same |
| 6.3 | Chorus diff check `consistent: true` | `chorctl diff check …` then `chorctl diff report …` |

## Suggested order for a real migration

1. Freeze writes to the source (or route writes through the migration
   tool's proxy).
2. Wait for the migration tool to report completion (§6).
3. Run `migration-verify --level 1` for a fast inventory check.
4. Run `migration-verify --level 2 --smoke-test` (default depth) — this is
   the report to archive.
5. If the report shows `etag-inconclusive` objects or you need byte-level
   proof for compliance, run `--level 3` (optionally per `--prefix`, with
   `--max-deep-bytes`).
6. Manually cover the "—" items above that apply to your deployment.
7. Archive `migration-report.json` + `.html` with the change record.
