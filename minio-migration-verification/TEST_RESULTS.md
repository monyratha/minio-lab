# Test results — Chorus A → B replication and verification

Date: 2026-09-12 (times below are UTC unless marked +07).
Host: macOS / Apple Silicon, Docker Desktop. Tools: `mc RELEASE.2026-09-06`,
`chorctl 0.7.10`, Go 1.27.1, Chorus commit `8b68045`.

## 1. State before the test

```
$ docker ps --format '{{.Names}}\t{{.Status}}'
docker-compose-web-ui-1   Up
docker-compose-worker-1   Up   (0.0.0.0:9670-9671)
docker-compose-redis-1    Up
minio-b                   Up   (0.0.0.0:9002->9000, 9003->9001)
minio-a                   Up   (0.0.0.0:9000-9001)

$ docker network inspect minio-migration   # members
minio-a 172.25.0.2, minio-b 172.25.0.3, redis 172.25.0.4, worker 172.25.0.5, web-ui 172.25.0.6

$ mc ls --summarize minio-a/migration-test
file1.txt 19B, file2.txt 22B, file3.bin 10MiB, hello.txt 12B   Total Objects: 4
$ mc ls --summarize minio-b/migration-test
Unable to list bucket. Bucket `migration-test` does not exist.
$ mc version info minio-a/migration-test
minio-a/migration-test is un-versioned
```

Proxy profile was **not** started (not needed for initial copy, see
CHORUS_FINDINGS.md §3). No changes were made to MinIO A data.

## 2. chorctl installation and connectivity

```
$ brew install clyso/tap/chorctl          # chorctl 0.7.10
$ brew install go                         # go 1.27.1 (needed for the verification CLI)
$ chorctl storage
NAME            ADDRESS                 PROVIDER     USERS
follower        http://minio-b:9000     S3           user1
main [MAIN]     http://minio-a:9000     S3           user1
$ chorctl repl buckets -u user1 -f main -t follower
migration-test
```

## 3. Chorus replication A → B

```
$ chorctl repl add -u user1 -f main -t follower -b migration-test      # 16:05:08 UTC
$ sleep 5; chorctl repl
NAME                                                   PROGRESS                 OBJECTS     EVENTS     LAG       PAUSED     AGE       ARCHIEVED     HAS_SWITCH
user1:main:migration-test->follower:migration-test     [##########] 100.0 %     4/4         0/0        0s        false      5s        false         -
```

REST status: `isInitDone: true, initObjListed: 4, initObjDone: 4,
events: 0, eventsDone: 0` (full JSON in CHORUS_FINDINGS.md §1). All four
objects landed on B at 23:05:11 +07, i.e. ~3 s after the policy was added.

```
$ mc ls --summarize minio-b/migration-test
[2026-09-12 23:05:11 +07]    19B STANDARD file1.txt
[2026-09-12 23:05:11 +07]    22B STANDARD file2.txt
[2026-09-12 23:05:11 +07]  10MiB STANDARD file3.bin
[2026-09-12 23:05:11 +07]    12B STANDARD hello.txt
Total Size: 10 MiB   Total Objects: 4
```

### Per-object comparison with `mc stat` (A vs B)

| Object | Size | ETag (A = B) | Content-Type (A = B) |
|--------|------|--------------|----------------------|
| hello.txt | 12 B | `ba4aae7b7c72dcdee71af2d5a0843d70` | text/plain |
| file1.txt | 19 B | `c088939e04b16763fd82c3ada1e24a7f` | text/plain |
| file2.txt | 22 B | `5d615834de9f1e5fb64497c6123fbb91` | text/plain |
| file3.bin | 10 MiB | `f1c9645dbc14efddc7d8a322685f26eb` | application/octet-stream |

ETags match the values recorded in the handoff document. No user metadata
or tags exist on the source objects.

## 4. Chorus diff check

```
$ chorctl diff check main:migration-test follower:migration-test --user user1
Diff check has been created.
$ chorctl diff
STORAGES                                         STATUS         STATS
follower:migration-test, main:migration-test     consistent     3/3
```

Negative test (probe object added to B only) → `not consistent`, entry
`zz-probe-extra.txt  follower ✓  main X`; probe removed → `consistent`
again. Details in CHORUS_FINDINGS.md §2.

## 5. Verification tool — final run against the lab data

Build and test:

```
$ cd ~/minio-lab/minio-migration-verification
$ go vet ./... && go test ./...
ok  	github.com/monyratha/migration-verify/internal/verify	0.486s
$ go build -o bin/migration-verify ./cmd/migration-verify
```

Run (level 2 = list + HEAD + tags, with application smoke test):

```
$ export SOURCE_ACCESS_KEY=minioadmin SOURCE_SECRET_KEY=minioadmin \
         TARGET_ACCESS_KEY=minioadmin TARGET_SECRET_KEY=minioadmin
$ ./bin/migration-verify \
    --source http://localhost:9000 --target http://localhost:9002 \
    --source-name "MinIO A" --target-name "MinIO B" \
    --bucket migration-test --smoke-test --list-matched \
    --json reports/migration-report.json --html reports/migration-report.html
```

Output:

```
MinIO Migration Verification
============================

Source: MinIO A (http://localhost:9000)
Target: MinIO B (http://localhost:9002)
Generated: 2026-09-12 16:15:02 UTC

Source Connectivity    PASS    http://localhost:9000 reachable, credentials accepted (1 buckets visible)
Target Connectivity    PASS    http://localhost:9002 reachable, credentials accepted (1 buckets visible)

Bucket: migration-test -> migration-test
----------------------------
Bucket Exists          PASS    yes
Versioning             PASS    none
Object Lock            PASS    none
Bucket Tags            PASS    none
Bucket Policy          PASS    none
Lifecycle              PASS    none
Encryption             PASS    none
Object Listing         PASS    listed 4 source / 4 target objects
Object Count           PASS    4
Object Names           PASS    4 common
Object Size            PASS    4 of 4 compared
Checksum / ETag        PASS    4 of 4 match
Content Type           PASS    4 of 4 compared
Metadata               PASS    4 of 4 compared
Tags                   PASS    4 of 4 compared
Content (SHA-256)      SKIPPED not needed: all comparable ETags matched (use --level 3 to force)
Versions               N/A     bucket is not versioned
App Smoke Test         PASS    target: PUT, HEAD, GET, PRESIGNED-GET, DELETE ok (probe .migration-verify-smoke/probe-….txt removed)

Source Objects     4 (10.0 MiB)
Target Objects     4 (10.0 MiB)
Matched Objects    4
Missing Objects    0
Extra Objects      0
Mismatched Objects 0

Object details:
  [PASS] file1.txt  19 B etag=c088939e04b16763fd82c3ada1e24a7f text/plain
  [PASS] file2.txt  22 B etag=5d615834de9f1e5fb64497c6123fbb91 text/plain
  [PASS] file3.bin  10.0 MiB etag=f1c9645dbc14efddc7d8a322685f26eb application/octet-stream
  [PASS] hello.txt  12 B etag=ba4aae7b7c72dcdee71af2d5a0843d70 text/plain

Bucket Status      PASS

============================
Overall            PASS   (32ms)
============================
exit code 0
```

JSON totals (`reports/migration-report.json`):

```json
"status": "PASS",
"totals": {"sourceObjects": 4, "targetObjects": 4, "sourceBytes": 10485813, "targetBytes": 10485813,
           "matchedObjects": 4, "missingObjects": 0, "extraObjects": 0, "mismatchedObjects": 0,
           "errorObjects": 0, "etagInconclusive": 0, "deepVerified": 0, "deepBytes": 0}
```

### Level 3 — full content verification (extra evidence)

```
$ ./bin/migration-verify --source http://localhost:9000 --target http://localhost:9002 \
    --source-name "MinIO A" --target-name "MinIO B" --bucket migration-test --level 3 \
    --json reports/migration-report-level3.json --html reports/migration-report-level3.html
Checksum / ETag        PASS    4 of 4 match
Content (SHA-256)      PASS    4 objects fully hashed on both sides
Deep Verified      4 (20.0 MiB downloaded)
Overall            PASS   (97ms)
```

SHA-256 (A = B): file1.txt `d1856fba…`, file2.txt `b14040da…`,
file3.bin `e5b844cc…`, hello.txt `6cbc1a31…` (full hashes in the JSON).

## 6. Self-tests proving the tool detects problems

All of these ran against **scratch buckets** (`verify-selftest`,
`verify-selftest-v`) created on both servers for the test and deleted
afterwards, plus one temporary probe object on B. `migration-test` on A was
never modified. Reports are kept under `reports/self-test/`.

### 6a. Extra object on target (`reports/self-test/extra-object-report.*`)

```
Object Count           FAIL    object counts differ [4 -> 5]
Object Names           FAIL    4 common; 1 extra on target: zz-probe-extra.txt
  [FAIL] zz-probe-extra.txt  extra  present on target, absent on source
Overall            FAIL      exit code 1
```

### 6b. Every mismatch class (`reports/self-test/mismatch-report.*`)

Scratch bucket with: identical file, size difference, same-size content
difference, Content-Type difference, metadata difference, tag difference,
object missing on target, and a 6 MB file uploaded single-part on A
(ETag `a602d19d…`) but multipart on B (ETag `25f8a8ac…-1`) with identical
bytes.

```
Object Count           FAIL    object counts differ [8 -> 7]
Object Names           FAIL    1 missing on target: missing.txt
Object Size            FAIL    1 mismatched of 7
Checksum / ETag        FAIL    2 mismatched
Content Type           FAIL    1 mismatched of 7
Metadata               FAIL    1 mismatched of 7
Tags                   FAIL    1 mismatched of 7
Content (SHA-256)      PASS    1 objects fully hashed on both sides     <- big.bin resolved by SHA-256
  [FAIL] ct.txt        contentType  content-type "text/plain" != "application/json"
  [FAIL] etag.txt      etag         etag f5ac8127… != 971bd7f7…
  [FAIL] meta.txt      metadata     x-amz-meta-env missing on target (source="prod"), x-amz-meta-owner "alice" != "bob"
  [FAIL] missing.txt   missing      present on source, absent on target
  [FAIL] sizediff.txt  size,etag    size 5 != 7; etag ae5b468c… != 41e9bbd8…
  [FAIL] tag.txt       tags         team "a" != "b"
Matched Objects 2 (same.txt, big.bin)   Deep Verified 1 (11.4 MiB downloaded)
```

### 6c. Versioned bucket (`reports/self-test/versioned-mismatch-report.*`)

Versioning enabled on both; `w.txt` 2 identical versions on both, `v.txt`
2 versions on A / 1 on B, `d.txt` deleted on A (delete marker) but present on B.

```
Versioning             PASS    Enabled
Versions               FAIL    2 keys with differing version history [6 -> 4]
versionDiffs: d.txt  version count 2 != 1, delete markers 1 != 0
              v.txt  version count 2 != 1
```

## 7. State after the test

* MinIO A: `migration-test` unchanged (4 objects).
* MinIO B: `migration-test` with the 4 replicated objects; probe removed.
* Scratch buckets removed from both servers.
* Chorus: one bucket replication policy `user1:main:migration-test->follower:migration-test`
  (active, init done) and one diff check result (consistent) remain in
  Redis. Remove with `chorctl repl delete …` / `chorctl diff purge …` if a
  clean re-run is wanted.
* Chorus compose stack (redis, worker, web-ui) still running; proxy not started.
