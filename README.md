# minio-lab

Copy every bucket from one MinIO to another with
[Chorus](https://github.com/clyso/chorus), then prove the copy is complete
with `migration-verify`, an independent checker that only speaks S3.

```
 MinIO A (source) ──── Chorus copies ────▶ MinIO B (target)
        │                                        │
        └────── migration-verify compares ───────┘
                       PASS / FAIL report
```

**Two ways to read this:**

- **Try it on a laptop** — keep reading. The lab ships two MinIO servers in
  Docker and sample data, so nothing else is needed.
- **Migrate a real server** — go to **[MIGRATION.md](MIGRATION.md)**. It is
  a runbook for an existing source and target, and skips the lab parts.

## Requirements

- Docker
- Go 1.22 or newer (builds `migration-verify`)
- `chorctl` — `brew install clyso/tap/chorctl`
- `mc`, optional, to upload your own data — `brew install minio/stable/mc`

## Try it in one command

```bash
make lab
```

Starts MinIO A and B, seeds four sample objects, starts Chorus, copies the
bucket and verifies it. The last line is `Overall PASS`.

## Step by step

### 1. Start the two MinIO servers

```bash
make minio-up
```

| | S3 API | Web console | Login |
|---|---|---|---|
| MinIO A (source) | http://localhost:9000 | http://localhost:9001 | `minioadmin` / `minioadmin` |
| MinIO B (target) | http://localhost:9002 | http://localhost:9003 | `minioadmin` / `minioadmin` |

### 2. Put data on MinIO A

Sample data (four small objects in bucket `migration-test`):

```bash
make seed
```

Or your own files, with `mc`:

```bash
mc alias set minio-a http://localhost:9000 minioadmin minioadmin
```

```bash
mc mb minio-a/my-bucket
```

```bash
mc mirror ~/Downloads/my-folder minio-a/my-bucket
```

You can also drag files into the console at http://localhost:9001.
More `mc` commands: [MC_CHEATSHEET.md](MC_CHEATSHEET.md).

### 3. Copy A → B with Chorus

```bash
make chorus-up
```

```bash
make repl
```

`make repl` copies every bucket on A and waits until the copy is finished.
Watch it with `chorctl repl`, `chorctl dash`, or the web UI at
http://localhost:8080.

The copy is one-shot. Files added to A afterwards are not copied: run
`make repl` again for new buckets, and see
[GUIDE.md](GUIDE.md#new-files-after-the-copy) for new files in a bucket
that was already copied.

### 4. Verify

```bash
make verify
```

Compares every bucket on A with B: object names, sizes, content type,
metadata, tags, and a SHA-256 of every object's content on both sides.
Prints a summary and writes
`minio-migration-verification/out/migration-report.html`.

```
Source Objects     3620 (5.8 GiB)
Target Objects     3620 (5.8 GiB)
Missing Objects    0
Overall            PASS
```

Exit code is 1 on FAIL, so it works as a gate in a script.
The content check downloads everything from both sides; for a quick pass
on a large bucket that skips it:

```bash
make verify VERIFY_LEVEL=2
```

## Use your own servers

Copy `.env.example` to `.env`, fill in the source and target URLs and keys,
and run steps 3 and 4 only. [MIGRATION.md](MIGRATION.md) walks through it,
including checks before the copy and the switch-over afterwards.

## Stop

```bash
make down
```

`make clean` also deletes all lab data and the Chorus clone.

## More

- [MIGRATION.md](MIGRATION.md) — runbook for migrating a real server
- [GUIDE.md](GUIDE.md) — every setting, how each part works, troubleshooting
- [MC_CHEATSHEET.md](MC_CHEATSHEET.md) — `mc` commands for uploading, inspecting, and breaking the migration on purpose
- [minio-migration-verification/README.md](minio-migration-verification/README.md) — the verify tool: all flags, checks, report format
- [CHECKLIST.md](minio-migration-verification/CHECKLIST.md), [CHORUS_FINDINGS.md](minio-migration-verification/CHORUS_FINDINGS.md), [TEST_RESULTS.md](minio-migration-verification/TEST_RESULTS.md) — the verification checklist, what Chorus can and cannot report, and the recorded runs
