#!/usr/bin/env sh
# Creates the lab bucket on MinIO A and fills it with a few sample objects.
# Does nothing if the bucket already exists, so it never overwrites data.
#
# Needs the mc client. The Makefile runs this either with your local mc or
# inside the mc container, which is why the endpoint is a variable: from your
# laptop MinIO A is localhost:9000, from a container it is minio-a:9000.
#
# The three small files are uploaded with 'mc cp', so their ETag is a plain
# MD5 and the verification can compare them directly. The 10 MiB file is
# streamed with 'mc pipe', which always produces a multipart ETag. That gives
# the lab one object whose ETag cannot be compared, which is what exercises
# the SHA-256 deep check in migration-verify.
set -eu

ENDPOINT="${ENDPOINT:-http://localhost:9000}"
BUCKET="${BUCKET:-migration-test}"
ACCESS_KEY="${ACCESS_KEY:-minioadmin}"
SECRET_KEY="${SECRET_KEY:-minioadmin}"
ALIAS=lab-seed

mc --quiet alias set "$ALIAS" "$ENDPOINT" "$ACCESS_KEY" "$SECRET_KEY" >/dev/null

if mc --quiet ls "$ALIAS/$BUCKET" >/dev/null 2>&1; then
  echo "bucket $BUCKET already exists on $ENDPOINT, leaving it alone"
  mc --quiet ls --summarize "$ALIAS/$BUCKET"
  exit 0
fi

echo "creating bucket $BUCKET on $ENDPOINT"
mc --quiet mb "$ALIAS/$BUCKET" >/dev/null

TMP="${TMPDIR:-/tmp}/minio-lab-seed.$$"
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT

printf 'hello lab\n'               > "$TMP/hello.txt"
printf 'second test object\n'      > "$TMP/file1.txt"
printf 'third sample test object\n' > "$TMP/file2.txt"

mc --quiet cp "$TMP/hello.txt" "$TMP/file1.txt" "$TMP/file2.txt" "$ALIAS/$BUCKET/" >/dev/null

head -c 10485760 /dev/urandom | mc --quiet pipe "$ALIAS/$BUCKET/file3.bin" >/dev/null

mc --quiet ls --summarize "$ALIAS/$BUCKET"
