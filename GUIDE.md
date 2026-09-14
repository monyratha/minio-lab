# Guide

The details behind the [README](README.md) (real servers) and
[LAB.md](LAB.md) (two MinIO servers on a laptop). Come here when you want
to know what a command does, change a setting, or fix an error.

## 1. How the parts fit together

```
   MinIO A / B              Chorus                  migration-verify
 ┌──────────────┐      ┌───────────────┐          ┌──────────────────┐
 │ two S3       │ ───▶ │ worker copies │ ───────▶ │ compares A and B │
 │ servers      │      │ A → B         │          │ PASS / FAIL      │
 └──────────────┘      └───────────────┘          └──────────────────┘
   make minio-up         make chorus-up             make verify
   make seed             make repl
```

The three parts are independent:

| I want to… | Run | Needs |
|---|---|---|
| Two S3 servers to experiment with | `make minio-up`, `make seed` | Docker |
| Copy buckets with Chorus | `make chorus-up`, `make repl` | Docker, `chorctl`, two S3 endpoints |
| Verify a copy made by any tool | `make verify` | Go, two S3 endpoints |
| All of it | `make lab` | everything above |

Every target is safe to re-run. `make` alone lists them.

Repository layout:

| Directory | Contents |
|---|---|
| `minio-a/`, `minio-b/` | compose file for each MinIO; data lives in `data/` (git-ignored) |
| `chorus/` | upstream Chorus clone, made by `make chorus-up` (git-ignored) |
| `scripts/` | seed, config rendering, wait-for-copy |
| `minio-migration-verification/` | the `migration-verify` tool, its docs, and `out/` for reports (git-ignored) |

## 2. Settings

Everything is set in `.env`. Without that file the lab defaults apply, so
the lab runs with no setup. Copy the example to change anything:

```bash
cp .env.example .env
```

Any setting can also be given for one command: `make verify BUCKET=photos`.
`make config` shows what is in use.

| Variable | Default | Meaning |
|---|---|---|
| `SOURCE_URL` / `TARGET_URL` | `http://minio-a:9000` / `http://minio-b:9000` | source and target **as the Chorus worker reaches them** from inside Docker |
| `SOURCE_URL_LOCAL` / `TARGET_URL_LOCAL` | `http://localhost:9000` / `http://localhost:9002` | the same servers **as your machine reaches them** (`make seed`, `make verify`) |
| `SOURCE_ACCESS_KEY` / `SOURCE_SECRET_KEY` | `minioadmin` | source credentials (read) |
| `TARGET_ACCESS_KEY` / `TARGET_SECRET_KEY` | `minioadmin` | target credentials (read + write) |
| `SOURCE_PROVIDER` / `TARGET_PROVIDER` | `Minio` | `Minio`, `Ceph` or `Other` |
| `SOURCE_REGION` / `TARGET_REGION` | empty | optional S3 region |
| `BUCKET` | `all` | what `repl` and `verify` handle: `all` = every bucket on the source, a name = only that bucket. `seed` always creates `migration-test` |
| `VERIFY_LEVEL` | `3` | depth of `make verify`, see [§5](#5-migration-verify) |
| `CHORUS_USER` | `user1` | label of the credentials inside the Chorus config; not an S3 user |
| `NETWORK` | `minio-migration` | Docker network shared by MinIO and Chorus |
| `CHORUS_REF` | `8b68045` | pinned Chorus commit |
| `MC_IMAGE` | `quay.io/minio/mc:latest` | `mc` image, used by `make seed` only when you have no local `mc` |

Rules that save time:

- **Two kinds of URL.** The Chorus worker runs in a container, so
  `localhost` there means the container itself. Give it a hostname or IP; a
  server on your own Mac is `host.docker.internal`. Your machine uses the
  `_LOCAL` pair. With remote servers both pairs are simply the same URL.
- **`https://` turns TLS on.** There is no separate secure flag.
- **Chorus reads `.env` only at startup.** After a change run
  `make chorus-reload`. `make verify` reads it on every run.
- `.env` is git-ignored, so real keys stay out of the repository.

Ports on your machine:

| Service | URL | Login |
|---|---|---|
| MinIO A S3 API / console | `http://localhost:9000` / `:9001` | `minioadmin` / `minioadmin` |
| MinIO B S3 API / console | `http://localhost:9002` / `:9003` | same |
| Chorus REST API / gRPC | `http://localhost:9671` / `:9670` | none |
| Chorus web UI | `http://localhost:8080` | none |

These credentials are for the lab only.

## 3. MinIO A and B

```bash
make minio-up
```

Starts both servers on the shared Docker network so Chorus can reach them
as `minio-a:9000` and `minio-b:9000`. Data is stored in `minio-a/data/` and
`minio-b/data/`; deleting those folders resets the servers.

```bash
make seed
```

Creates bucket `migration-test` on A with four objects, and does nothing if
the bucket already exists. Three are uploaded with `mc cp` (plain MD5 ETag)
and one 10 MiB file with `mc pipe` (multipart ETag). That last one matters:
a multipart ETag is not the MD5 of the content, so the verification has to
fall back to hashing the object. Every lab run therefore exercises both the
cheap check and the deep one.

To load your own data, see [MC_CHEATSHEET.md](MC_CHEATSHEET.md). Write to
A only; anything written to B by hand shows up as an *extra object* FAIL.

```bash
make minio-down
```

## 4. Chorus

```bash
make chorus-up
```

Does three things, all repeatable: clones Chorus at the pinned commit into
`chorus/`, applies `chorus-lab-config.patch` (joins its containers to the
lab network), and renders `chorus/docker-compose/s3-credentials.yaml` from
`.env`. Then it starts redis, the worker (`:9671`) and the web UI
(`:8080`). Nothing in `chorus/` is ever edited by hand.

```bash
make repl
```

With the default `BUCKET=all` this lists the source buckets that have no
replication policy yet (`chorctl repl buckets`), adds one policy per
bucket, and waits until every policy reports `isInitDone`. Re-run it after
creating new buckets on A; buckets that already have a policy are skipped.
With `BUCKET=name` it adds a policy for that bucket only.

Chorus does not allow one user-level "all buckets" policy next to
bucket-level ones, which is why the lab always uses bucket-level policies.

Watch progress:

```bash
chorctl repl
```

```bash
chorctl dash
```

The same data as JSON: `curl -s -X POST http://localhost:9671/replication -d '{}'`.

The wait times out after 180 s but the copy keeps running; for a large
transfer use `TIMEOUT=3600 scripts/wait-replication.sh` or just watch
`chorctl dash`.

### New files after the copy

The copy is one-shot. Chorus only learns about later writes through its S3
proxy or bucket notifications, and the lab starts neither, so an object
uploaded to A after `make repl` is **not** copied to B. `make verify` will
report it as missing. To find and re-copy such objects:

```bash
chorctl diff check main:migration-test follower:migration-test --user user1
```

```bash
chorctl diff report main:migration-test follower:migration-test --user user1
```

```bash
chorctl diff fix --source main:migration-test follower:migration-test --user user1
```

`report` shows the result of the last `check` and does not refresh on its
own; run `check` again after `fix` to confirm. Note that `diff` takes
`storage:bucket` arguments, unlike `repl add`.

### Chorus with your own servers

The full procedure is in the [README](README.md). In short: set the
URLs and keys in `.env`, then:

```bash
make chorus-up
```

```bash
chorctl storage
```

`chorctl storage` must list both `main` and `follower`; that proves the
worker reached them. Then `make repl` and `make verify` as usual. All data
flows through the machine running Chorus, so for a large migration run it
somewhere with good bandwidth to both sides.

What Chorus reports, and what it does not, is written down in
[CHORUS_FINDINGS.md](minio-migration-verification/CHORUS_FINDINGS.md).

```bash
make chorus-down
```

## 5. migration-verify

```bash
make verify
```

Builds the tool, then compares every bucket (or `BUCKET`) on
`SOURCE_URL_LOCAL` with `TARGET_URL_LOCAL`. It needs no Docker and no
Chorus, so it also verifies a copy made with `mc mirror`, rclone or a
vendor tool. Reports land in `minio-migration-verification/out/`
(git-ignored).

`VERIFY_LEVEL` sets how deep each object is checked. Each level includes
the ones below it.

| Level | Checks | Cost | Use for |
|---|---|---|---|
| 1 | bucket settings; object names, count, size, ETag | one listing per side | huge buckets, first pass |
| 2 | + content type, user metadata, tags | one HEAD and one tagging request per object per side | quick pass on a large bucket |
| 3 (default) | + SHA-256 of every object's content | downloads all data from both sides | proof of byte-identical content |

Level 3 is the default so that a plain `make verify` proves the content.
Level 2 is a sound shortcut when the download is too expensive: an ETag is
the MD5 of the content for single-part unencrypted uploads, so equal ETags
already prove equal content, and objects whose ETag cannot be trusted
(multipart, SSE) and differ are hashed on demand at any level.

Exit codes: `0` PASS (or WARN), `1` FAIL, `2` could not run.

The tool can be used on its own against any two endpoints:

```bash
cd minio-migration-verification && BUCKET=photos ./run-verify.sh --level 3
```

All flags, checks and the report format:
[minio-migration-verification/README.md](minio-migration-verification/README.md).

## 6. Troubleshooting

```bash
make status
```

| Message | Cause | Fix |
|---|---|---|
| `chorctl storage` shows only one storage, or none | the worker cannot reach a server | URL uses `localhost`, wrong key, or firewall; fix `.env`, then `make chorus-reload` |
| `dial tcp [::1]:9671: connection refused` | Chorus worker not running | `make chorus-up` |
| `InvalidArg: unknown user … for storage main` | `CHORUS_USER` does not match the Chorus config | use `user1`, or `make chorus-reload` after changing it |
| `AlreadyExists: replication already exists` | policy for that bucket already added | harmless; `make repl` ignores it |
| `make repl` prints `timed out after 180s` | large copy still running | normal; watch `chorctl dash` |
| `Bucket Exists FAIL target bucket does not exist` | verify ran before the copy finished | wait for `chorctl repl` to show 100 %, then `make verify` again |
| `missing` objects | on source, not on target: written after the copy started, or copy unfinished | [New files after the copy](#new-files-after-the-copy) |
| `extra` objects | on target, not on source: the target was not empty | delete them on the target, or `--fail-on-extra=false` |
| `mismatched` objects | same name, different content or metadata | investigate that object; re-copy with `chorctl diff fix` |
| verify fails at `Source Connectivity` / `Target Connectivity` with a certificate error | self-signed certificate | `cd minio-migration-verification && ./run-verify.sh --source-insecure --target-insecure` |
| verify is slow | level 3 downloads everything twice | `make verify VERIFY_LEVEL=2` first, or one bucket: `make verify BUCKET=name` |
| `Bucket 'migration-test' does not exist` | nothing seeded on A | `make seed` |
| `pull access denied for minio/mc` | MinIO images moved to quay.io | update the Makefile, or `make seed MC_IMAGE=…` |
| `zsh: parse error near '#'` | a `#` comment was pasted into zsh | remove the comment |
| port already in use | another service on 9000–9003 or 8080 | change the left side of `ports:` in the compose file |

## 7. Clean up

```bash
make down
```

Stops all containers and removes the network. Data is kept.

```bash
make clean
```

Also deletes `minio-a/data`, `minio-b/data`, the redis volume, the Chorus
clone and the built binary. The redis volume matters: Chorus keeps its
replication policies there, and with stale policies a fresh lab thinks the
copy already happened and copies nothing. `make clean` then `make lab`
starts from zero.

## 8. Install on Ubuntu

Everything in this repository is Docker, Go and shell scripts, so it runs
the same on Linux. The commands below were run on Ubuntu 22.04 and 24.04.
They are for amd64; on arm64 replace `amd64` with `arm64` in the URLs.

**Docker** with the Compose plugin — follow
<https://docs.docker.com/engine/install/ubuntu/>, then let your user run
it without `sudo`:

```bash
sudo usermod -aG docker $USER && newgrp docker
```

**Go** — install the version `go.mod` asks for from go.dev. Ubuntu's own
`golang-go` package is 1.18 on 22.04, too old for this project (any Go
1.21 or newer would also work, since the build downloads the toolchain it
needs):

```bash
curl -fsSL https://go.dev/dl/go1.27.1.linux-amd64.tar.gz | sudo tar -C /usr/local -xz && echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && export PATH=$PATH:/usr/local/go/bin
```

**chorctl** — the version the lab is tested with, a single binary from
the Chorus GitHub release:

```bash
curl -fsSL https://github.com/clyso/chorus/releases/download/v0.7.10/chorctl_v0.7.10_linux_amd64.tar.gz | tar -xz && sudo install chorctl /usr/local/bin/ && rm chorctl
```

**mc** — a single binary from the `minio/mc` GitHub release.
`dl.min.io`, which older MinIO docs point to, now answers `410 Gone`:

```bash
curl -fsSL https://github.com/minio/mc/releases/download/RELEASE.2025-08-13T08-35-41Z/mc.linux-amd64.RELEASE.2025-08-13T08-35-41Z -o mc && sudo install mc /usr/local/bin/ && rm mc
```

Check:

```bash
docker compose version && go version && chorctl --version && mc --version
```

The lab's own MinIO A and B run on Linux too: they are plain Compose
files with a multi-arch image, so `make lab` and [LAB.md](LAB.md) apply
unchanged.

Three Linux-specific points:

- `host.docker.internal` does not exist on Linux Docker by default. If a
  MinIO server runs on the same host **outside** Docker, put the host's
  LAN IP in `SOURCE_URL` / `TARGET_URL`. The lab's own MinIO A and B are
  containers on the shared network, so they need nothing.
- Use `docker compose` (the plugin), not the old `docker-compose` binary;
  the Makefile calls the former.
- The MinIO containers run as root, so on Linux the bind-mounted
  `minio-a/data` and `minio-b/data` folders end up root-owned (Docker
  Desktop on a Mac maps them to your user). `make clean` then needs
  `sudo rm -rf minio-a/data minio-b/data` first.
