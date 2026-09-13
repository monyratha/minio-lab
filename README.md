# minio-lab — MinIO Community → Enterprise migration verification

A small lab that migrates a bucket from one MinIO to another with
[Chorus](https://github.com/clyso/chorus), then verifies the result with an
independent S3 tool.

| Directory | Purpose |
|-----------|---------|
| `minio-a/` | Source MinIO (`http://localhost:9000`, console `:9001`), compose file + data dir (ignored) |
| `minio-b/` | Target MinIO (`http://localhost:9002`, console `:9003`), compose file + data dir (ignored) |
| `chorus/` | Upstream clone of <https://github.com/clyso/chorus> (ignored; lab changes are in `chorus-lab-config.patch`) |
| `minio-migration-verification/` | `migration-verify` Go CLI, checklist, Chorus findings, test results, reports |

## Quick start

```bash
make
```

That lists every target. To run the whole lab — MinIO A+B, sample data,
Chorus, replication and verification:

```bash
make lab
```

Requirements: Docker, Go 1.22+, and `chorctl`
(`brew install clyso/tap/chorctl`) for the replication step.

## The three parts are independent

You do not have to run everything. Pick what you need:

| Goal | Command | Details |
|---|---|---|
| Two S3 servers to experiment with | `make minio-up && make seed` | [GUIDE.md — Path A](GUIDE.md#path-a--only-minio-a-and-minio-b) |
| Chorus replication only | `make chorus-up && make repl` | [GUIDE.md — Path B](GUIDE.md#path-b--only-chorus) |
| Verify any S3 migration, no lab needed | `cd minio-migration-verification && make build` | [GUIDE.md — Path C](GUIDE.md#path-c--only-migration-verify) |
| The full recorded experiment | `make lab` | [GUIDE.md — Path D](GUIDE.md#path-d--the-full-lab) |

Stop everything with `make down`. Delete the data too with `make clean`.

## Documents

* **[GUIDE.md](GUIDE.md)** — step-by-step setup, ports, credentials, common errors.
* [minio-migration-verification/README.md](minio-migration-verification/README.md) — the `migration-verify` tool: flags, checks, report format.
* [minio-migration-verification/CHECKLIST.md](minio-migration-verification/CHECKLIST.md) — the verification checklist.
* [minio-migration-verification/CHORUS_FINDINGS.md](minio-migration-verification/CHORUS_FINDINGS.md) — what Chorus reports, and what it does not.
* [minio-migration-verification/TEST_RESULTS.md](minio-migration-verification/TEST_RESULTS.md) — recorded commands and results.
