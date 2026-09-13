# minio-lab — MinIO Community → Enterprise migration verification

| Directory | Purpose |
|-----------|---------|
| `minio-a/` | Source MinIO (`http://localhost:9000`, console `:9001`), compose file + data dir (ignored) |
| `minio-b/` | Target MinIO (`http://localhost:9002`, console `:9003`), compose file + data dir (ignored) |
| `chorus/` | Upstream clone of <https://github.com/clyso/chorus> (ignored, see below) |
| `minio-migration-verification/` | `migration-verify` Go CLI, checklist, Chorus findings, test results, reports |

## Reproduce the lab

```bash
docker network create minio-migration
(cd minio-a && docker compose up -d) && docker network connect minio-migration minio-a
(cd minio-b && docker compose up -d) && docker network connect minio-migration minio-b

git clone https://github.com/clyso/chorus && git -C chorus checkout 8b68045
git -C chorus apply ../chorus-lab-config.patch      # external network + minio-a/minio-b storages
(cd chorus/docker-compose && docker compose up -d)   # redis, worker, web-ui (proxy not needed)

brew install clyso/tap/chorctl
chorctl repl add -u user1 -f main -t follower -b migration-test
cd minio-migration-verification && make build && ./run-verify.sh --smoke-test
```

See `minio-migration-verification/README.md` for the tool and
`minio-migration-verification/TEST_RESULTS.md` for the recorded results.
