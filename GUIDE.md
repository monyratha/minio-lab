# Lab guide — use the whole lab, or only one part

This repository holds three parts. They are **independent**. You can run one
part alone, or all of them together.

```
   Path A                  Path B                     Path C
 ┌──────────┐          ┌───────────┐              ┌──────────────────┐
 │ MinIO A  │ ───────▶ │  Chorus   │ ───────────▶ │ migration-verify │
 │ MinIO B  │  copies  │  worker   │   result is  │  PASS / FAIL     │
 └──────────┘  data    └───────────┘   checked by └──────────────────┘
  two S3 servers        the migration tool          the audit tool
```

## 1. Pick your path

| I want to… | Path | Need Docker? | Need Chorus? | Need Go? |
|---|---|---|---|---|
| Practise S3 commands against two servers | **A** | yes | no | no |
| Learn or demo Chorus replication | **B** | yes | yes | no |
| Verify a migration I did with any tool | **C** | no | no | yes |
| Reproduce the full recorded experiment | **D** | yes | yes | yes |

Every command below is safe to re-run. Each path also has a `make` shortcut.

> **zsh warning:** do not paste `#` comments into an interactive `zsh`.
> It fails with `parse error near '#'`. All code blocks here are comment-free.

## 2. All settings live in .env

You never edit anything inside the `chorus/` clone. Copy the example file
once and change only that:

```bash
cp .env.example .env
```

Without a `.env` file the built-in lab defaults are used, so the lab works
out of the box. Check what is in use at any time:

```bash
make config
```

The file holds two kinds of URL, and mixing them up is the usual mistake:

| Setting | Who uses it | Lab value |
|---|---|---|
| `SOURCE_URL` / `TARGET_URL` | the Chorus worker, from inside a container | `http://minio-a:9000` / `http://minio-b:9000` |
| `SOURCE_URL_LOCAL` / `TARGET_URL_LOCAL` | `make seed` and `make verify`, from your machine | `http://localhost:9000` / `http://localhost:9002` |

With your own servers both pairs are usually the same URL.

`make chorus-config` turns `.env` into
`chorus/docker-compose/s3-credentials.yaml`. `make chorus-up` does it for
you. After changing `.env` on a running lab:

```bash
make chorus-reload
```

That regenerates the file and recreates the worker, which reads the config
only at startup.

## 3. Ports and credentials

| Service | URL | Login |
|---|---|---|
| MinIO A (source) S3 API | `http://localhost:9000` | `minioadmin` / `minioadmin` |
| MinIO A console | `http://localhost:9001` | same |
| MinIO B (target) S3 API | `http://localhost:9002` | same |
| MinIO B console | `http://localhost:9003` | same |
| Chorus worker REST API | `http://localhost:9671` | none |
| Chorus gRPC management API | `localhost:9670` | none |
| Chorus Web UI | `http://localhost:8080` | none |

Inside the `minio-migration` docker network the containers reach each other
as `http://minio-a:9000` and `http://minio-b:9000`. Chorus uses those names.
You, on your laptop, use the `localhost` ports.

**These credentials are lab-only.** Never reuse `minioadmin` anywhere real.

---

## Path A — only MinIO A and MinIO B

Use this when you just want two S3 servers to play with.

```bash
make minio-up
make seed
```

Manual version:

```bash
docker network inspect minio-migration >/dev/null 2>&1 || docker network create minio-migration
(cd minio-a && docker compose up -d)
(cd minio-b && docker compose up -d)
docker network connect minio-migration minio-a 2>/dev/null || true
docker network connect minio-migration minio-b 2>/dev/null || true
```

The shared network is only needed if another container (Chorus) must reach
them. For Path A alone you can skip the two `network connect` lines.

`make seed` does this for you. It uses your local `mc` if you have one, and
otherwise runs `mc` in a container. Note that MinIO images live on
**quay.io**, not Docker Hub.

It writes four objects on purpose:

| Object | Uploaded with | ETag |
|---|---|---|
| `hello.txt`, `file1.txt`, `file2.txt` | `mc cp` | plain MD5, compared directly |
| `file3.bin` (10 MiB) | `mc pipe` | multipart, so the verification falls back to SHA-256 |

`mc pipe` streams, and a streamed upload is always multipart. A multipart
ETag is not the MD5 of the content, so it cannot be compared between two
servers. Keeping one such object means every lab run exercises the deep
check as well as the cheap one.

Or put data in by hand with the MinIO client:

```bash
mc alias set a http://localhost:9000 minioadmin minioadmin
mc alias set b http://localhost:9002 minioadmin minioadmin
mc mb -p a/migration-test
echo "hello lab" | mc pipe a/migration-test/hello.txt
mc ls --summarize a/migration-test
```

Data lives in `minio-a/data/` and `minio-b/data/` on your disk. Those folders
are git-ignored. Deleting them resets the servers.

Stop with `make minio-down`.

---

## Path B — only Chorus

Use this to see how Chorus replication works. Chorus needs two S3 endpoints.
The easy choice is Path A. You can also point it at your own storage.

```bash
make minio-up
make chorus-up
make repl
```

Manual version:

```bash
[ -d chorus ] || git clone https://github.com/clyso/chorus
git -C chorus checkout 8b68045
git -C chorus apply --reverse --check ../chorus-lab-config.patch 2>/dev/null \
  || git -C chorus apply ../chorus-lab-config.patch
scripts/render-s3-credentials.sh
(cd chorus/docker-compose && docker compose up -d)
until curl -sf http://localhost:9671/storage >/dev/null; do sleep 2; done
```

`chorus/` is a clone of upstream and is git-ignored. Two things change it:

1. `chorus-lab-config.patch` joins redis, worker and web-ui to the external
   `minio-migration` network.
2. `scripts/render-s3-credentials.sh` writes `s3-credentials.yaml` from your
   `.env`, replacing the fake demo storages.

Nothing inside `chorus/` is edited by hand, so a fresh clone always ends up
with your settings.

The S3 proxy service is **not** started. It is only needed to capture live
writes. An initial copy does not need it.

Then drive it with `chorctl` (install: `brew install clyso/tap/chorctl`):

```bash
chorctl storage
chorctl repl buckets -u user1 -f main -t follower
chorctl repl add -u user1 -f main -t follower -b migration-test
chorctl repl
chorctl dash
```

`repl add` is a one-shot copy in this lab. Chorus only learns about later
writes through its S3 proxy or bucket notifications, and the lab starts
neither, so objects uploaded to A after `make repl` are **not** copied to B.
Use the built-in diff to find and re-copy them. `diff` takes
`storage:bucket` arguments, not `-f/-t/-b`:

```bash
chorctl diff check  main:migration-test follower:migration-test --user user1
chorctl diff report main:migration-test follower:migration-test --user user1
chorctl diff fix --source main:migration-test follower:migration-test --user user1
```

`report` shows the result of the last `check` and does not refresh by
itself; run `check` (or `recheck`) again after a `fix` to confirm.

`repl add` returns immediately. The worker lists and copies the objects in
the background, so `chorctl repl` right after it shows `0.0 %`. Wait for
`isInitDone` before you verify:

```bash
scripts/wait-replication.sh
```

`make repl` already does this for you.

Same data over plain HTTP, no CLI:

```bash
curl -s -X POST http://localhost:9671/replication -d '{}' | jq
```

### Point Chorus at your own MinIO servers

MinIO A and B are only examples. Chorus never needs them. Put your servers
in `.env`:

```bash
cp .env.example .env
```

```dotenv
SOURCE_URL=https://s3-old.example.com
SOURCE_ACCESS_KEY=YOUR_SOURCE_KEY
SOURCE_SECRET_KEY=YOUR_SOURCE_SECRET

TARGET_URL=https://s3-new.example.com
TARGET_ACCESS_KEY=YOUR_TARGET_KEY
TARGET_SECRET_KEY=YOUR_TARGET_SECRET

SOURCE_URL_LOCAL=https://s3-old.example.com
TARGET_URL_LOCAL=https://s3-new.example.com

BUCKET=your-bucket
```

Then start Chorus alone and copy:

```bash
make chorus-up
chorctl storage
make repl
make verify
```

`chorctl storage` must list both storages. That proves the worker reached
them. If you change `.env` later, run `make chorus-reload`.

Four things to keep right:

* **Never use `localhost` in `SOURCE_URL` or `TARGET_URL`.** Inside the
  worker container it means the container. Use a hostname or IP it can
  reach; on a Mac, a server on your laptop is `host.docker.internal`.
* **`https://` is enough to turn TLS on.** There is no separate flag.
* **The source key needs read, the target key needs write.**
* **`CHORUS_USER` is a label, not an S3 user.** It only has to match the
  `-u` value that `chorctl` uses, which `make repl` handles for you.

What Chorus does and does not report is written down in
[`minio-migration-verification/CHORUS_FINDINGS.md`](minio-migration-verification/CHORUS_FINDINGS.md).

Stop with `make chorus-down`.

---

## Path C — only migration-verify

This part needs **no Docker and no Chorus**. It only speaks S3. It works
after any migration tool: Chorus, `mc mirror`, rclone, or a vendor tool.

```bash
cd minio-migration-verification
make build
```

Point it at any two S3 endpoints:

```bash
export SOURCE_ACCESS_KEY=... SOURCE_SECRET_KEY=...
export TARGET_ACCESS_KEY=... TARGET_SECRET_KEY=...

./bin/migration-verify \
  --source https://old-storage.example.com \
  --target https://new-storage.example.com \
  --bucket my-bucket \
  --json report.json --html report.html
```

For the lab defaults (A → B, bucket `migration-test`) use the wrapper:

```bash
./run-verify.sh --smoke-test
```

### Verification levels

`--level` (or `VERIFY_LEVEL` for `make verify`) sets how deep each object is
checked. Each level includes the ones below it.

| Level | Checks | Cost | Use it for |
|---|---|---|---|
| 1 | bucket settings, object names, count, size, ETag | one listing per side | huge buckets, quick first pass |
| 2 (default) | + content type, user metadata, tags | one HEAD and one GetObjectTagging per object per side | normal verification |
| 3 | + SHA-256 of the content of every object | downloads all data from both sides | proof of byte-identical content |

Level 2 is enough in most cases: an ETag is the MD5 of the content for
single-part unencrypted uploads, so equal ETags already prove equal content.
Objects whose ETag cannot be trusted (multipart `…-N`, SSE) and differ are
SHA-256 checked on demand at any level, so a doubtful object never passes
silently. Level 3 forces that download for everything.

```bash
make verify VERIFY_LEVEL=3
```

Exit codes make it usable as a gate in CI or a runbook:

| Code | Meaning |
|---|---|
| 0 | PASS (or WARN) |
| 1 | FAIL — a check did not pass |
| 2 | could not run — bad flags or endpoint unreachable |

Read [`minio-migration-verification/README.md`](minio-migration-verification/README.md)
for all flags, the three verification levels, and the report format.

---

## Path D — the full lab

```bash
make lab
```

That runs Path A, then Path B, then Path C in order. It is the same as:

```bash
make minio-up
make seed
make chorus-up
make repl
make verify
```

Expected result: 4 objects on A, 4 objects on B, overall status `PASS`.
The recorded run is in
[`minio-migration-verification/TEST_RESULTS.md`](minio-migration-verification/TEST_RESULTS.md).

---

## 4. Check the state

```bash
make status
docker network inspect minio-migration --format '{{range .Containers}}{{.Name}} {{end}}'
git -C chorus rev-parse --short HEAD
curl -sf http://localhost:9671/storage && echo " worker OK"
```

## 5. Common errors

| Message | Meaning | Do this |
|---|---|---|
| `network with name minio-migration already exists` | Already created. | Ignore it. |
| `endpoint with name minio-a already exists in network` | Already connected. | Ignore it. |
| `destination path 'chorus' already exists` | Already cloned. | Ignore it, but run the `checkout` yourself. `&&` skips it after a failed clone. |
| `patch does not apply` | Usually already applied. | `git -C chorus apply --reverse --check ../chorus-lab-config.patch` — no output means it is applied. |
| `zsh: parse error near '#'` | zsh rejects `#` comments when pasted. | Remove the comment from the line. |
| `dial tcp [::1]:9671: connection refused` | The Chorus worker is not running. | `make chorus-up && make chorus-wait` |
| `Bucket Exists FAIL target bucket does not exist` | Verification ran before Chorus finished copying. | Update to the latest `Makefile`; `make repl` now waits for `isInitDone`. Or run `make verify` again. |
| `pull access denied for minio/mc` | MinIO images are no longer on Docker Hub. | Update to the latest `Makefile`; it pulls `quay.io/minio/mc`. Override with `make seed MC_IMAGE=…`. |
| `InvalidArg: unknown user … for storage main` | Chorus does not know that user name. | Use `user1`, or `make repl CHORUS_USER=<name>`. The name must exist under `credentials:` in `s3-credentials.yaml`. |
| `Bucket 'migration-test' does not exist` | Nothing was seeded on MinIO A. | `make seed` |
| Port already in use | Another service holds 9000/9001/9002/9003. | Change the left side of `ports:` in the compose file. |

## 6. Stop and clean up

Stop the containers and remove the network:

```bash
make down
```

Also delete the MinIO data, the redis volume, the chorus clone and the built
binary:

```bash
make clean
```

`make clean` deletes the `minio-a/data` and `minio-b/data` folders. All lab
objects are lost. That is the point of a lab, but do not run it in a
directory holding data you want.

The redis volume matters. Chorus stores its replication policies there. If
you delete the MinIO data but keep that volume, Chorus still believes the old
replication finished, copies nothing, and the next verification run fails
with an empty target. `make clean` removes it. A plain
`docker compose down` does not.

**Start again from zero:**

```bash
make clean
make lab
```

## 7. Settings reference

Set these in `.env`. They can also be overridden for one command, for
example `make verify BUCKET=my-bucket`.

| Variable | Default | Meaning |
|---|---|---|
| `SOURCE_URL` | `http://minio-a:9000` | source as the Chorus worker reaches it |
| `TARGET_URL` | `http://minio-b:9000` | target as the Chorus worker reaches it |
| `SOURCE_ACCESS_KEY` / `SOURCE_SECRET_KEY` | `minioadmin` | source credentials |
| `TARGET_ACCESS_KEY` / `TARGET_SECRET_KEY` | `minioadmin` | target credentials |
| `SOURCE_PROVIDER` / `TARGET_PROVIDER` | `Minio` | `Minio`, `Ceph` or `Other` |
| `SOURCE_REGION` / `TARGET_REGION` | empty | optional S3 region |
| `SOURCE_URL_LOCAL` | `http://localhost:9000` | source as **your machine** reaches it |
| `TARGET_URL_LOCAL` | `http://localhost:9002` | target as **your machine** reaches it |
| `BUCKET` | `migration-test` | bucket used by `seed`, `repl` and `verify` |
| `VERIFY_LEVEL` | `2` | depth of `make verify`: 1 listing and ETags, 2 adds metadata and tags, 3 hashes all content |
| `CHORUS_USER` | `user1` | credentials label in the Chorus config |
| `NETWORK` | `minio-migration` | shared docker network |
| `CHORUS_REF` | `8b68045` | pinned upstream chorus commit |
| `MC_IMAGE` | `quay.io/minio/mc:latest` | mc image, used only when you have no local `mc` |

`isSecure` is not a setting. An `https://` URL turns TLS on by itself.

`.env` is git-ignored, so real keys stay out of the repository.

The Chorus user is **not** your login name. It must match a key under
`credentials:` in `chorus/docker-compose/s3-credentials.yaml`. The variable is
called `CHORUS_USER`, not `USER`, because `USER` is already set by your shell
and would silently win.
