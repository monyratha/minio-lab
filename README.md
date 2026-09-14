# minio-lab — migrate a MinIO and prove the copy

Copy every bucket from an existing MinIO (or any S3 server) to a new one
with [Chorus](https://github.com/clyso/chorus), then prove the copy is
complete with `migration-verify`, an independent checker that only speaks
S3 and produces a PASS / FAIL report.

This page is the runbook for a real source and target. To try the whole
thing first on a laptop with two throw-away MinIO servers, see
[LAB.md](LAB.md).

```
 your old server ──── Chorus copies ────▶ your new server
   (source)                                  (target)
        └──────── migration-verify compares ─────┘
```

Both tools run on one machine — a laptop or a jump host — and **all data
flows through it**: source → this machine → target for the copy, and both
servers → this machine again for the verification. Pick a machine with
good bandwidth to both servers.

## Before you start

- On that machine: Docker, Go 1.22+, `chorctl` (`brew install clyso/tap/chorctl`),
  `mc` (`brew install minio/stable/mc`), and this repository.
- **Source credentials**: list and read on every bucket to migrate.
- **Target credentials**: list, read, write, create bucket. `make verify`
  also writes and deletes one small probe object per bucket on the target
  to prove an application can use it.
- The copy is **one-shot**. Objects written to the source after the copy
  starts are not picked up automatically (see step 6). Plan a moment when
  writes to the source stop.
- The target buckets should be empty or not exist. Anything already on the
  target that is not on the source is reported as an *extra object* FAIL.

## 1. Configure

```bash
cp .env.example .env
```

Fill in these lines. Leave the rest as it is.

```dotenv
SOURCE_URL=https://old.example.com
SOURCE_ACCESS_KEY=...
SOURCE_SECRET_KEY=...

TARGET_URL=https://new.example.com
TARGET_ACCESS_KEY=...
TARGET_SECRET_KEY=...

SOURCE_URL_LOCAL=https://old.example.com
TARGET_URL_LOCAL=https://new.example.com
```

- `SOURCE_URL` / `TARGET_URL` are used by Chorus, which runs in a Docker
  container. **Never write `localhost` there**: inside the container it
  means the container. A server on the same Mac is `host.docker.internal`.
- `SOURCE_URL_LOCAL` / `TARGET_URL_LOCAL` are used by `make verify` from
  the machine itself. For remote servers they are the same as above.
- `https://` turns TLS on; there is no separate setting.
- `BUCKET` stays `all` to migrate every bucket. Set it to a name to migrate
  one bucket at a time.

```bash
make config
```

shows what will be used. `.env` is git-ignored.

## 2. Check that you can reach both servers

```bash
mc alias set src https://old.example.com SOURCE_KEY SOURCE_SECRET
```

```bash
mc alias set dst https://new.example.com TARGET_KEY TARGET_SECRET
```

```bash
mc ls src
```

```bash
mc ls dst
```

`mc ls src` must list the buckets you expect. If a server uses a
self-signed certificate add `--insecure` to `mc alias set`.

## 3. Start Chorus and confirm it sees both servers

```bash
make chorus-up
```

```bash
chorctl storage
```

`chorctl storage` must list `main` (the source) and `follower` (the
target). If one is missing, the worker could not reach it: check the URL
(step 1), the credentials, and that Docker on this machine can reach the
server. After fixing `.env`, run `make chorus-reload`.

## 4. Copy

```bash
make repl
```

This adds one replication policy per source bucket and waits for the copy.
The wait gives up after 180 seconds, **but the copy keeps running**. For a
real migration just watch it instead:

```bash
chorctl dash
```

or, less interactive:

```bash
chorctl repl
```

Every row must reach `100.0 %`. Progress is also at http://localhost:8080.
`make repl` is safe to run again; buckets that already have a policy are
skipped, so it also picks up buckets created on the source later.

## 5. Verify

```bash
make verify
```

Compares every bucket on the source with the target: bucket settings,
object names, sizes, content type, metadata, tags, and — by default — a
SHA-256 of every object's content on both sides. That last check downloads
all the data twice. For a first quick pass on a large migration:

```bash
make verify VERIFY_LEVEL=2
```

then run the full `make verify` once you expect it to pass.

The result ends with `Overall PASS` or `Overall FAIL`, and the exit code is
`1` on FAIL. Every object with a problem is listed by name. The full report
is written to `minio-migration-verification/out/migration-report.html` and
`.json` — keep a copy as evidence of the migration.

Typical failures:

| Report says | Meaning | Do this |
|---|---|---|
| `missing` | on source, not on target | writes happened after the copy started; step 6 |
| `extra` | on target, not on source | the target bucket was not empty; delete on target, or `--fail-on-extra=false` |
| `Bucket Exists FAIL` | target bucket not created yet | the copy has not finished; wait, re-run |
| `mismatched` | same name, different content or metadata | investigate that object; re-copy with step 6 |

## 6. Catch up and switch over

When you are ready to switch applications to the new server:

1. Stop writes to the source (maintenance window, or read-only).
2. Re-copy anything that changed since step 4. For each bucket:

```bash
chorctl diff check main:BUCKET follower:BUCKET --user user1
```

```bash
chorctl diff fix --source main:BUCKET follower:BUCKET --user user1
```

3. Run `make verify` again. Repeat until it reports `PASS`.
4. Point your applications at the target.

Only after the final `PASS` should the source be retired.

## 7. Clean up

```bash
make chorus-down
```

Stops Chorus on this machine. It does nothing to either server.
`make clean` additionally deletes the Chorus clone and its redis volume
(where the replication policies live) and the lab's local MinIO data
folders, which are empty if you never ran the lab. Remove the keys from
`.env` when you are done.

## If something goes wrong

| Symptom | Cause | Fix |
|---|---|---|
| `chorctl storage` shows only one storage, or none | worker cannot reach a server | URL uses `localhost`, wrong key, firewall; fix `.env`, `make chorus-reload` |
| `dial tcp [::1]:9671: connection refused` | Chorus is not running | `make chorus-up` |
| `InvalidArg: unknown user … for storage main` | `CHORUS_USER` in `.env` was changed without a reload | `make chorus-reload` |
| `make repl` prints `timed out after 180s` | large copy still running | normal; watch `chorctl dash` |
| verify fails at `Source Connectivity` / `Target Connectivity` with a certificate error | self-signed certificate | `cd minio-migration-verification && ./run-verify.sh --source-insecure --target-insecure` |
| verify is slow | level 3 downloads everything twice | `make verify VERIFY_LEVEL=2` first, or verify one bucket: `make verify BUCKET=name` |

## More

- [LAB.md](LAB.md) — the same flow on a laptop with two throw-away MinIO servers
- [GUIDE.md](GUIDE.md) — every setting, what each command does, troubleshooting
- [MC_CHEATSHEET.md](MC_CHEATSHEET.md) — `mc` commands for inspecting buckets and objects
- [minio-migration-verification/README.md](minio-migration-verification/README.md) — the verify tool: all flags, checks, report format
- [CHECKLIST.md](minio-migration-verification/CHECKLIST.md), [CHORUS_FINDINGS.md](minio-migration-verification/CHORUS_FINDINGS.md), [TEST_RESULTS.md](minio-migration-verification/TEST_RESULTS.md) — the verification checklist, what Chorus can and cannot report, and the recorded runs
