# Chorus findings — replication progress, status, diff and reporting

Tested against `clyso/chorus` commit `8b68045` ("v4 signature proxy fix"),
`chorctl 0.7.10`, worker built from the same source, on 2026-09-12.
Environment: MinIO A (`main`, `http://minio-a:9000`) → MinIO B (`follower`,
`http://minio-b:9000`), bucket `migration-test` (4 objects, 10 MiB).

## TL;DR — answers to the team lead's questions

| Question | Answer | Where |
|----------|--------|-------|
| 1. Replication progress? | **Yes.** Per replication: `initObjListed` / `initObjDone` (initial copy), `events` / `eventsDone` + `eventLag` (live changes). Shown as a % bar in `chorctl repl`, live in `chorctl dash`, and in the Web UI. | `POST /replication`, `chorctl repl` |
| 2. Replication completion status? | **Yes.** `isInitDone: true` once the initial copy is finished. No explicit per-object success/failure list. | same |
| 3. Object counts? | **Approximate only.** `initObjListed`/`initObjDone` are *task* counts ("approximate number of objects listed/processed" per the proto). Not an authoritative source/target object count. | `proto/chorus/policy.proto` |
| 4. Diff between source and destination? | **Yes — built in.** `chorctl diff check` lists both buckets and compares by object key + size + ETag (options to ignore ETags / sizes, and a versioned mode). The result is a list of objects that are not present with the same key/size/ETag on all storages, with a per-storage ✓/X column. | `chorctl diff check/report`, `POST /diff/start`, `/diff/report`, `/diff/report-entries` |
| 5. Integrity verification? | **Partial.** Only key + size + ETag. It does **not** compare Content-Type, user metadata, tags, bucket configuration, and it does not download/hash content, so multipart-ETag differences show as mismatches even when bytes are identical, and encrypted objects cannot be verified at all. It has a `diff fix` to re-copy the objects it flags. | `service/worker/handler/diff_handlers.go` |
| 6. Exportable report? | **Not as a file.** Nothing writes a report document. The diff result is queryable as JSON through the REST API (paginated `report-entries`) and printed as a table by `chorctl diff report`. You can `curl … > file.json` yourself, but there is no summary document, no PASS/FAIL verdict with counts, no HTML. | `proto/http.yaml` |
| 7. JSON / API output? | **Yes.** Every command is a gRPC/REST call; the REST API returns JSON (`http://localhost:9671`). `chorctl` itself only prints tables (no `--output json`). | `proto/http.yaml`, `tools/chorctl` |
| 8. CLI-only status? | **No** — CLI (`chorctl`), interactive TUI (`chorctl dash`), REST/gRPC API and Web UI (`http://localhost:8080`, pages *Replication* and *Diff Reports*) all expose the same data. | |

**Conclusion:** Chorus gives good *operational* visibility (progress, done/not
done, and a basic key/size/ETag consistency check with an automatic fix),
which is enough to know *when* the migration is finished. It does not
produce a migration *report* in the sense of an archivable, self-contained
PASS/FAIL document with counts and per-object evidence, and its consistency
check is narrower than the verification checklist (no metadata, content
type, tags, bucket config, or content hashing). That gap is what
`migration-verify` fills, and because it only speaks S3 it also works if the
migration is later done with a different tool.

## 1. Replication progress / status

Created with:

```
chorctl repl add -u user1 -f main -t follower -b migration-test
```

`chorctl repl` (5 s later):

```
NAME                                                   PROGRESS                 OBJECTS     EVENTS     LAG       PAUSED     AGE       ARCHIEVED     HAS_SWITCH
user1:main:migration-test->follower:migration-test     [##########] 100.0 %     4/4         0/0        0s        false      5s        false         -
```

REST (`curl -s -X POST http://localhost:9671/replication -d '{}'`):

```json
{
  "replications": [{
    "id": {"user": "user1", "fromStorage": "main", "toStorage": "follower",
           "fromBucket": "migration-test", "toBucket": "migration-test"},
    "createdAt": "2026-09-12T16:05:08.211128053Z",
    "isPaused": false,
    "isInitDone": true,
    "initObjListed": "4", "initObjDone": "4",
    "events": "0", "eventsDone": "0", "eventLag": null,
    "hasSwitch": false, "isArchived": false,
    "eventSource": "EVENT_SOURCE_PROXY"
  }]
}
```

Semantics (from `proto/chorus/policy.proto`):

* `isInitDone` — initial phase (copy of everything that existed when the
  policy was created) is complete.
* `initObjListed` / `initObjDone` — "approximate number of objects
  listed/processed during initial replication; corresponds to total number
  of tasks created/completed". Good for a progress bar, not an inventory.
* `events` / `eventsDone` / `eventLag` — live change events captured by the
  proxy (or S3 notifications / webhook) and how far behind the target is.
* `StreamReplication` gRPC streams these updates; `chorctl dash` renders
  them as an interactive TUI.

There is **no** per-object status, no error list and no "objects failed"
counter in the API. Failed copy tasks are retried by the worker queue
(asynq) and only visible in worker logs / metrics (`metrics.enabled` was
false in this setup).

## 2. Diff check (Chorus' consistency feature)

```
chorctl diff check main:migration-test follower:migration-test --user user1
chorctl diff                                   # list checks
chorctl diff report main:migration-test follower:migration-test
```

Result on the replicated bucket:

```
STORAGES                                         STATUS         STATS
follower:migration-test, main:migration-test     consistent     3/3

CHECK
READY:        true
QUEUED:       3
COMPLETED:    3
CONSISTENT:   true
VERSIONED:    false
IGNORE SIZES: false
IGNORE ETAGS: false
```

Notes on what those numbers mean (verified in
`service/worker/handler/diff_handlers.go`):

* `QUEUED / COMPLETED` = **worker tasks** (1 coordinator + 1 list task per
  storage here), not object counts. There is no object count in the diff
  result.
* The check lists every object on each storage and adds it to a Redis set
  keyed by `(diffID, key, size, etag)` (or `(key, size)` with
  `--ignore-etags`, or `(key)` with `--ignore-sizes`). An object is
  consistent when that exact key is present on all N storages.
  `CONSISTENT` is simply "no set entry is missing a storage".
* Report entries are therefore only produced for **inconsistent** objects;
  a consistent check has an empty entry list (`{"entries": [], "cursor": "0"}`).
* Versioned mode (`--last-versions-only` false + versioned buckets) compares
  per version index.
* `chorctl diff fix` re-copies flagged objects from the chosen source index;
  `chorctl diff recheck` reruns, `chorctl diff purge` deletes the result.

### Negative test — Chorus does detect a difference

A probe object was added **only to MinIO B** and the check rerun:

```
$ echo "extra-on-target-only" | mc pipe minio-b/migration-test/zz-probe-extra.txt
$ chorctl diff recheck main:migration-test follower:migration-test
$ chorctl diff report main:migration-test follower:migration-test
CONSISTENT:   false
...
PATH                   SIZE      ETAG                                   follower     main
zz-probe-extra.txt     21        490bde896b7e2e830682caede7b1dec1-1     ✓            X
```

REST `POST /diff/report-entries`:

```json
{"entries": [{"object": "zz-probe-extra.txt", "versionIdx": "0", "size": "21",
              "etag": "490bde896b7e2e830682caede7b1dec1-1",
              "storageEntries": [{"storage": "follower", "versionId": "", "bucket": "migration-test"}]}],
 "cursor": "0"}
```

The probe was then removed and the check rerun → `consistent` again.

### What the Chorus diff does *not* cover

| Not compared by Chorus diff | Covered by `migration-verify` |
|-----------------------------|-------------------------------|
| Content-Type | ✅ level 2 |
| User metadata (`x-amz-meta-*`), Cache-Control, Content-Encoding, … | ✅ level 2 |
| Object tags | ✅ level 2 |
| Bucket versioning / object-lock / policy / lifecycle / tags / encryption config | ✅ |
| Content when ETags are not comparable (multipart part-size differences, SSE) — Chorus reports these as *inconsistent* even if bytes are identical, or you must run with `--ignore-etags` and lose the check entirely | ✅ automatic SHA-256 fallback |
| Object / byte totals, PASS/FAIL verdict, archivable JSON/HTML report | ✅ |
| Application-level PUT/GET/HEAD/DELETE/presign test | ✅ `--smoke-test` |

### Practical consequence for MinIO → MinIO Enterprise

Chorus copies objects with minio-go `GetObject → PutObject`
(`service/worker/copy/copy.go`), preserving `Content-Type` and user metadata
(tags only with `features.tagging: true`). minio-go uploads anything larger
than **16 MiB as multipart**, so for objects > 16 MiB the target ETag is a
multipart ETag that generally differs from the source ETag unless the source
was uploaded with the same part layout. Expect:

* Chorus `diff check` (default mode) to flag such objects as inconsistent
  even when they are byte-identical; use `migration-verify` (SHA-256
  fallback) to decide, or run the Chorus check with `--ignore-etags` and
  accept a size-only comparison.
* For versioned replication Chorus adds
  `x-amz-meta-chorus-source-version-id` to target objects; pass
  `--ignore-meta-key x-amz-meta-chorus-source-version-id` to
  `migration-verify` so this expected difference is not reported.

## 3. Proxy — required or not?

Not required for a one-shot bucket migration. `AddReplication` (bucket
level, default `EVENT_SOURCE_PROXY`) only enqueues a listing+copy task for
the worker; the initial copy ran with the proxy profile **off**. The proxy
is needed only to capture *ongoing* writes (clients must send their S3
traffic through `:9669`) and for the zero-downtime switch feature. The
repeated `NotImplemented: proxy is not listening` errors in the worker log
come from the Web UI's "S3 Proxy Credentials" widget polling
`GetProxyCredentials`; they are harmless here.

## 4. Configuration changes made to Chorus in this task

None beyond what the handoff already recorded (external network
`minio-migration`, `s3-credentials.yaml` pointing at `minio-a`/`minio-b`).
`chorctl` was installed with `brew install clyso/tap/chorctl` and talks to
the worker REST API on `http://localhost:9671` (its default `--address`).

## 5. How to consume Chorus status from scripts

```bash
# replication status (JSON)
curl -s -X POST http://localhost:9671/replication -H 'Content-Type: application/json' -d '{}'

# start a diff check
curl -s -X POST http://localhost:9671/diff/start -H 'Content-Type: application/json' -d '{
  "locations":[{"storage":"main","bucket":"migration-test"},{"storage":"follower","bucket":"migration-test"}],
  "user":"user1"}'

# poll until ready, then read result + entries
curl -s -X POST http://localhost:9671/diff/report -H 'Content-Type: application/json' -d '{
  "locations":[{"storage":"main","bucket":"migration-test"},{"storage":"follower","bucket":"migration-test"}]}'
curl -s -X POST http://localhost:9671/diff/report-entries -H 'Content-Type: application/json' -d '{
  "locations":[{"storage":"main","bucket":"migration-test"},{"storage":"follower","bucket":"migration-test"}],
  "cursor":0,"pageSize":1000}'
```

OpenAPI spec: `proto/gen/openapi/chorus/chorus.swagger.json` in the repo.
