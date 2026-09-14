# minio-lab — migrate a MinIO and prove the copy

Copies every bucket from one MinIO (or any S3 server) to another with
[Chorus](https://github.com/clyso/chorus), then checks the copy with
`migration-verify` and gives a PASS / FAIL report.

```
 source ──── Chorus copies ────▶ target
    └──── migration-verify compares ────┘
```

Want to try it first? [LAB.md](LAB.md) runs the same flow on a laptop
with two throw-away MinIO servers.

## Requirements

- One machine that can reach both servers, with Docker, Go 1.22+,
  `chorctl` (`brew install clyso/tap/chorctl`) and this repository.
  All data flows through it.
- A **source key** that can list and read, and a **target key** that can
  list, read, write and create buckets.
- **Empty target buckets.** Objects on the target that are not on the
  source count as failures.
- **A moment when writes to the source stop.** The copy is one-shot;
  later writes are caught up by hand in step 5.

## 1. Configure

```bash
cp .env.example .env
```

Fill in these lines; leave the rest.

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

Chorus runs in Docker, so `SOURCE_URL` / `TARGET_URL` must not be
`localhost`. The `_LOCAL` pair is used by verify from the machine itself
and is the same URL for remote servers.

## 2. Start Chorus

```bash
make chorus-up
```

```bash
chorctl storage
```

Both `main` (source) and `follower` (target) must be listed. If not, fix
`.env` and run `make chorus-reload`.

## 3. Copy

```bash
make repl
```

Adds a replication for every source bucket and waits. The wait stops after
180 s but the copy continues; watch it until every row shows `100.0 %`:

```bash
chorctl dash
```

## 4. Verify

```bash
make verify
```

Compares every bucket: names, sizes, metadata, tags, and a SHA-256 of
every object's content on both sides. Ends with `Overall PASS` or `FAIL`
(exit code 1) and lists every problem object. The report is in
`minio-migration-verification/out/migration-report.html` — keep it as
evidence.

Large migration? `make verify VERIFY_LEVEL=2` skips the content download
for a quick first pass.

## 5. Switch over

1. Stop writes to the source.
2. Re-copy what changed since step 3, per bucket:

```bash
chorctl diff fix --source main:BUCKET follower:BUCKET --user user1
```

3. `make verify` until it reports `PASS`.
4. Point applications at the target. Retire the source only after that.

## 6. Clean up

```bash
make chorus-down
```

Stops Chorus on this machine; touches neither server. Remove the keys
from `.env`.

## More

- [GUIDE.md](GUIDE.md) — every setting, what each command does, troubleshooting
- [LAB.md](LAB.md) — the lab version with two local MinIO servers
- [MC_CHEATSHEET.md](MC_CHEATSHEET.md) — `mc` commands for inspecting buckets
- [minio-migration-verification/README.md](minio-migration-verification/README.md) — the verify tool in full
