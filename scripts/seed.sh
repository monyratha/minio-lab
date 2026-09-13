#!/usr/bin/env sh
# Creates the lab bucket on MinIO A and fills it with a few sample objects.
# Does nothing if the bucket already exists, so it never overwrites data.
#
# Needs the mc client. The Makefile runs this either with your local mc or
# inside the mc container, which is why the endpoint is a variable: from your
# laptop MinIO A is localhost:9000, from a container it is minio-a:9000.
set -eu

ENDPOINT="${ENDPOINT:-http://localhost:9000}"
BUCKET="${BUCKET:-migration-test}"
ACCESS_KEY="${ACCESS_KEY:-minioadmin}"
SECRET_KEY="${SECRET_KEY:-minioadmin}"
ALIAS=lab-seed

mc alias set "$ALIAS" "$ENDPOINT" "$ACCESS_KEY" "$SECRET_KEY" >/dev/null

if mc ls "$ALIAS/$BUCKET" >/dev/null 2>&1; then
  echo "bucket $BUCKET already exists on $ENDPOINT, leaving it alone"
else
  echo "creating bucket $BUCKET on $ENDPOINT"
  mc mb "$ALIAS/$BUCKET" >/dev/null
  echo "hello lab"          | mc pipe "$ALIAS/$BUCKET/hello.txt" >/dev/null
  echo "second test object" | mc pipe "$ALIAS/$BUCKET/file1.txt" >/dev/null
  echo "third sample test object" | mc pipe "$ALIAS/$BUCKET/file2.txt" >/dev/null
  head -c 10485760 /dev/urandom | mc pipe "$ALIAS/$BUCKET/file3.bin" >/dev/null
fi

mc ls --summarize "$ALIAS/$BUCKET"
