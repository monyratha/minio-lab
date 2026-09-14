# mc cheat sheet for this lab

The `mc` commands you actually need while testing the migration. Every
command below assumes the two aliases the lab already set up:

| Alias     | Endpoint                | Role                          |
|-----------|-------------------------|-------------------------------|
| `minio-a` | `http://localhost:9000` | source — put test data here   |
| `minio-b` | `http://localhost:9002` | target — Chorus copies here   |

Rule of thumb: **write to A, only read from B.** Anything you write to B by
hand will show up as an "extra object" FAIL in `make verify`.

Full reference: <https://min.io/docs/minio/linux/reference/minio-mc.html>.

---

## 1. Aliases (one-time setup)

```bash
# create or overwrite an alias: name, URL, access key, secret key
mc alias set minio-a http://localhost:9000 minioadmin minioadmin
mc alias set minio-b http://localhost:9002 minioadmin minioadmin

mc alias list                 # show all aliases and their keys
mc alias rm lab-seed          # remove one
mc admin info minio-a         # prove the alias works (server info)
```

Aliases live in `~/.mc/config.json`. The lab's own `make seed` creates a
temporary alias called `lab-seed`; you can ignore it.

## 2. Buckets

```bash
mc mb minio-a/migration-test              # create
mc mb --with-versioning minio-a/vtest     # create with versioning on
mc ls minio-a                             # list buckets
mc rb --force minio-a/scratch             # delete bucket and everything in it
```

## 3. Upload test data to A

```bash
# one file
mc cp ~/Downloads/report.pdf minio-a/migration-test/

# rename on the way in
mc cp ~/Downloads/report.pdf minio-a/migration-test/docs/q3.pdf

# a whole directory, keeping the tree (trailing slash matters)
mc cp --recursive ~/test-data/ minio-a/migration-test/data/

# generate a file of a given size without touching disk
head -c 5242880 /dev/urandom | mc pipe minio-a/migration-test/random-5mb.bin

# many small files quickly
for i in $(seq 1 50); do echo "object $i" | mc pipe minio-a/migration-test/many/obj-$i.txt; done
```

`mc cp` of a small file gives a plain MD5 ETag; `mc pipe` always produces a
multipart ETag. The verifier handles both — the multipart one forces the
SHA-256 deep check, which is a useful thing to test.

### With metadata, content-type and tags

The verifier compares all of these between A and B, so include some:

```bash
mc cp --attr "Content-Type=application/json;x-amz-meta-owner=monyratha;x-amz-meta-env=lab" \
      ~/sample.json minio-a/migration-test/sample.json

mc tag set minio-a/migration-test/sample.json "team=infra&stage=test"
mc tag list minio-a/migration-test/sample.json
```

## 4. Inspect what is there

```bash
mc ls minio-a/migration-test                           # top level
mc ls --recursive minio-a/migration-test               # everything
mc ls --recursive --summarize minio-a/migration-test   # + total count and size
mc du minio-a/migration-test                           # size per prefix

mc stat minio-a/migration-test/sample.json             # size, ETag, type, metadata
mc cat minio-a/migration-test/hello.txt                # print contents
mc head -n 5 minio-a/migration-test/big.log            # first lines only
mc find minio-a/migration-test --name "*.pdf"          # search by name
mc find minio-a/migration-test --larger 1MB            # search by size
```

## 5. Compare A and B by hand

`make verify` does this properly, but for a quick look:

```bash
# side-by-side count and size
mc ls --recursive --summarize minio-a/migration-test | tail -2
mc ls --recursive --summarize minio-b/migration-test | tail -2

# list the differences (nothing printed = identical listing)
mc diff minio-a/migration-test minio-b/migration-test

# same object on both sides?
mc stat minio-a/migration-test/sample.json
mc stat minio-b/migration-test/sample.json

# download from each side and hash locally
mc cat minio-a/migration-test/random-5mb.bin | shasum -a 256
mc cat minio-b/migration-test/random-5mb.bin | shasum -a 256
```

`mc diff` only compares names, sizes and ETags. It does not see metadata,
tags, versions or content — that is what `migration-verify` adds.

## 6. Versioning tests

```bash
mc version enable minio-a/migration-test
mc version info minio-a/migration-test

# create a version history for one key
for v in 1 2 3; do echo "version $v" | mc pipe minio-a/migration-test/versioned.txt; done
mc ls --versions minio-a/migration-test/versioned.txt

# delete marker on top
mc rm minio-a/migration-test/versioned.txt
mc ls --versions minio-a/migration-test/versioned.txt
```

The verifier compares version histories automatically when the source
bucket has versioning enabled (`--versions auto`). Enable versioning on the
bucket **before** Chorus replication so both sides have the same history.

## 7. Make the migration fail on purpose

Useful to prove the verifier catches problems. Do this **after** `make repl`
has finished, then run `make verify` and watch it turn red.

```bash
# missing object on target
mc rm minio-b/migration-test/file1.txt

# extra object on target
echo "should not be here" | mc pipe minio-b/migration-test/extra.txt

# content mismatch (same name, different bytes)
echo "tampered" | mc pipe minio-b/migration-test/hello.txt

# metadata mismatch
mc cp --attr "x-amz-meta-owner=someone-else" minio-a/migration-test/sample.json minio-b/migration-test/sample.json

# tag mismatch
mc tag remove minio-b/migration-test/sample.json
```

To get back to a clean state, delete the bucket on B and let Chorus copy
again, or simply `make clean && make lab`.

## 8. Users and access keys

```bash
# a dedicated user with read/write rights
mc admin user add minio-a alice alice-secret-123
mc admin policy attach minio-a readwrite --user alice
mc admin user ls minio-a

# an access key under root (what the console "Access Keys" page does)
mc admin accesskey create minio-a minioadmin
mc admin accesskey ls minio-a
mc admin accesskey info minio-a <ACCESS_KEY>
mc admin accesskey rm minio-a <ACCESS_KEY>

# use the new key
mc alias set minio-a-alice http://localhost:9000 alice alice-secret-123
```

Root (`minioadmin`) keeps working alongside any key you add. To have the lab
use another key, change `SOURCE_ACCESS_KEY` / `SOURCE_SECRET_KEY` in `.env`
and the matching `mc alias set`.

## 9. Clean up

```bash
mc rm --recursive --force minio-a/migration-test/many/   # one prefix
mc rm --recursive --force minio-a/migration-test         # empty the bucket
mc rb --force minio-a/migration-test                     # remove the bucket
mc rb --force minio-b/migration-test                     # and the target copy
```

Or stop the containers and delete all data with `make clean`.

## 10. Typical test loop

```bash
make minio-up
mc mb minio-a/migration-test
mc cp --recursive ~/test-data/ minio-a/migration-test/
mc ls --recursive --summarize minio-a/migration-test

make chorus-up && make repl
mc diff minio-a/migration-test minio-b/migration-test   # quick look

make verify                                             # the real check
open minio-migration-verification/reports/migration-report.html
```

## Handy flags

| Flag | Where | Meaning |
|------|-------|---------|
| `--recursive` / `-r` | `cp`, `ls`, `rm`, `find` | walk into prefixes |
| `--force` | `rm`, `rb` | no confirmation, delete non-empty |
| `--summarize` | `ls` | print total objects / size |
| `--versions` | `ls`, `stat`, `rm` | operate on all versions |
| `--attr "k=v;k=v"` | `cp`, `put` | set content-type / user metadata |
| `--json` | any | machine-readable output |
| `--quiet` / `-q` | any | suppress progress bars |
| `--insecure` | `alias set` | skip TLS verification for self-signed https |
