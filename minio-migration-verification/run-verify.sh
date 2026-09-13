#!/usr/bin/env bash
# Runs migration-verify against the lab environment (MinIO A -> MinIO B).
# Credentials default to the lab's minioadmin/minioadmin but can be
# overridden through the environment. Any extra arguments are passed on,
# e.g.:  ./run-verify.sh --level 3 --smoke-test
set -euo pipefail
cd "$(dirname "$0")"

export SOURCE_ACCESS_KEY="${SOURCE_ACCESS_KEY:-minioadmin}"
export SOURCE_SECRET_KEY="${SOURCE_SECRET_KEY:-minioadmin}"
export TARGET_ACCESS_KEY="${TARGET_ACCESS_KEY:-minioadmin}"
export TARGET_SECRET_KEY="${TARGET_SECRET_KEY:-minioadmin}"

SOURCE="${SOURCE_ENDPOINT:-http://localhost:9000}"
TARGET="${TARGET_ENDPOINT:-http://localhost:9002}"
BUCKET="${BUCKET:-migration-test}"

[ -x bin/migration-verify ] || go build -o bin/migration-verify ./cmd/migration-verify
mkdir -p reports

exec ./bin/migration-verify \
  --source "$SOURCE" --target "$TARGET" \
  --source-name "${SOURCE_NAME:-MinIO A}" --target-name "${TARGET_NAME:-MinIO B}" \
  --bucket "$BUCKET" \
  --json reports/migration-report.json --html reports/migration-report.html \
  "$@"
