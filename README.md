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

One Ubuntu machine (22.04 or newer) that can reach both servers. All
data flows through it. Install, in this order:

**Docker** with the Compose plugin — runs Chorus.
Follow <https://docs.docker.com/engine/install/ubuntu/>, then:

```bash
sudo usermod -aG docker $USER && newgrp docker
```

**Go 1.27** — builds `migration-verify` (apt's Go on 22.04 is too old):

```bash
curl -fsSL https://go.dev/dl/go1.27.1.linux-amd64.tar.gz | sudo tar -C /usr/local -xz && echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && export PATH=$PATH:/usr/local/go/bin
```

**chorctl** — controls Chorus:

```bash
curl -fsSL https://github.com/clyso/chorus/releases/download/v0.7.10/chorctl_v0.7.10_linux_amd64.tar.gz | tar -xz && sudo install chorctl /usr/local/bin/ && rm chorctl
```

**mc** — the MinIO client, for checking the servers:

```bash
curl -fsSL https://github.com/minio/mc/releases/download/RELEASE.2025-08-13T08-35-41Z/mc.linux-amd64.RELEASE.2025-08-13T08-35-41Z -o mc && sudo install mc /usr/local/bin/ && rm mc
```

**This repository:**

```bash
git clone git@github.com:monyratha/minio-lab.git && cd minio-lab
```

On arm64 replace `amd64` with `arm64` in the URLs. On macOS:
`brew install go minio/stable/mc clyso/tap/chorctl`.

| | |
|---|---|
| Source key | can list and read |
| Target key | can list, read, write and create buckets |
| Target buckets | empty — objects not on the source count as failures |
| Writes to the source | must stop at some point; the copy is one-shot (step 5) |

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

Why two URLs for each server? Chorus runs in Docker and uses
`SOURCE_URL` / `TARGET_URL` from inside its container; `make verify` runs
on the machine itself and uses the `_LOCAL` pair. For real servers both
are the same address. Only `localhost` is different in Docker, so never
use it in `SOURCE_URL` / `TARGET_URL`.

Chorus has run on this machine before, or this is your second
migration? See [step 7](#7-start-fresh-or-switch-to-another-project).

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

### Quick checks with `mc`

Once, register both servers (`mc` asks for the key and secret):

```bash
mc alias set src https://old.example.com
```

```bash
mc alias set dst https://new.example.com
```

Then, at any time:

| Question | Command |
|---|---|
| Same buckets on both sides? | `mc ls src` and `mc ls dst` |
| Same size and object count per bucket? | `mc du --depth 1 src` and `mc du --depth 1 dst` |
| Which objects differ in one bucket? | `mc diff src/BUCKET dst/BUCKET` (no output = identical) |

`mc` is the 10-second sanity check; `make verify` is the proof.

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

## 7. Start fresh, or switch to another project

Every migration is one settings file. `.env` is the default; a second
migration gets its own file, e.g. `.env.project2`. The commands are the
same for all of them.

### Start fresh

Use this when Chorus has run before on this machine — a lab run, a test,
or an earlier project — and you want to begin a migration from zero.

1. Clear what Chorus remembers (neither server is touched):

```bash
make chorus-reset
```

2. Configure as in [step 1](#1-configure) and continue with
   [step 2](#2-start-chorus).

Why the reset: Chorus remembers finished replications by bucket name.
Without it, a bucket named `assets` in the new migration would be treated
as already copied and skipped.

### Switch to another project

Use this when project 1 is done and project 2 is next.

1. Finish project 1: every row of `chorctl repl` at `100.0 %`,
   `make verify` says `PASS`, and the report is saved where the next run
   will not overwrite it:

```bash
cp minio-migration-verification/out/migration-report.html ~/project1-report.html
```

2. Clear what Chorus remembers about project 1:

```bash
make chorus-reset
```

3. Create project 2's settings file and fill in the same lines as in
   [step 1](#1-configure):

```bash
cp .env.example .env.project2
```

4. Tell `make` to use it — once per terminal:

```bash
export ENV=.env.project2
```

5. Check that the switch took effect:

```bash
make config
```

The last line must say `settings from .env.project2` and the URLs must be
project 2's servers.

6. Run [steps 2 to 6](#2-start-chorus) exactly as before: `make chorus-up`,
   `make repl`, `make verify`, save the report, `make chorus-down`.

For project 3, repeat from 1 with `.env.project3`.

Good to know:

- A new terminal forgets `export ENV=…`; run it again, or write the file
  on each command: `make repl ENV=.env.project2`.
- `make config` always tells you which file is active. Check it when in
  doubt.
- All `.env*` files are git-ignored, so keeping the old ones as a record
  is safe.

## More

- [GUIDE.md](GUIDE.md) — every setting, what each command does, troubleshooting
- [LAB.md](LAB.md) — the lab version with two local MinIO servers
- [MC_CHEATSHEET.md](MC_CHEATSHEET.md) — `mc` commands for inspecting buckets
- [minio-migration-verification/README.md](minio-migration-verification/README.md) — the verify tool in full
