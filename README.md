# minio-lab — MinIO Community → Enterprise migration verification

| Directory | Purpose |
|-----------|---------|
| `minio-a/` | Source MinIO (`http://localhost:9000`, console `:9001`), compose file + data dir (ignored) |
| `minio-b/` | Target MinIO (`http://localhost:9002`, console `:9003`), compose file + data dir (ignored) |
| `chorus/` | Upstream clone of <https://github.com/clyso/chorus> (ignored, see below) |
| `minio-migration-verification/` | `migration-verify` Go CLI, checklist, Chorus findings, test results, reports |

## Reproduce the lab

Run the steps from the repository root. Every step is safe to re-run.

Do not paste `#` comments into `zsh`. Interactive `zsh` does not allow them
by default, so the line fails with `parse error near '#'`.

**1. Shared docker network**

```bash
docker network inspect minio-migration >/dev/null 2>&1 || docker network create minio-migration
```

**2. Source and target MinIO**

```bash
(cd minio-a && docker compose up -d)
(cd minio-b && docker compose up -d)
docker network connect minio-migration minio-a 2>/dev/null || true
docker network connect minio-migration minio-b 2>/dev/null || true
```

**3. Chorus at the pinned commit**

```bash
[ -d chorus ] || git clone https://github.com/clyso/chorus
git -C chorus checkout 8b68045
```

**4. Lab config patch** (external network + `minio-a`/`minio-b` storages)

```bash
git -C chorus apply --reverse --check ../chorus-lab-config.patch 2>/dev/null \
  || git -C chorus apply ../chorus-lab-config.patch
```

The first command asks "is the patch already applied?". If yes, nothing happens.

**5. Chorus stack** (redis, worker, web-ui; the S3 proxy is not needed)

```bash
(cd chorus/docker-compose && docker compose up -d)
```

Wait for the worker REST API before the next step:

```bash
until curl -sf http://localhost:9671/storage >/dev/null; do sleep 2; done
```

**6. Replication rule**

```bash
brew install clyso/tap/chorctl
chorctl repl add -u user1 -f main -t follower -b migration-test
```

**7. Verification tool**

```bash
cd minio-migration-verification && make build && ./run-verify.sh --smoke-test
```

## Reset the lab

```bash
(cd chorus/docker-compose && docker compose down -v)
(cd minio-a && docker compose down) && (cd minio-b && docker compose down)
docker network rm minio-migration
rm -rf chorus minio-a/data minio-b/data
```

See `minio-migration-verification/README.md` for the tool and
`minio-migration-verification/TEST_RESULTS.md` for the recorded results.
