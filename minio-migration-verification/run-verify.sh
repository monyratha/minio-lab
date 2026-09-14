#!/usr/bin/env bash
# Runs migration-verify against the lab environment (MinIO A -> MinIO B).
# Credentials default to the lab's minioadmin/minioadmin but can be
# overridden through the environment. Any extra arguments are passed on,
# e.g.:  ./run-verify.sh --level 3 --smoke-test
#
# Reports go to out/ (git-ignored, overwritten on every run), or to
# REPORT_DIR. reports/ holds the committed evidence for TEST_RESULTS.md
# and is never written by this script.
set -euo pipefail
cd "$(dirname "$0")"

export SOURCE_ACCESS_KEY="${SOURCE_ACCESS_KEY:-minioadmin}"
export SOURCE_SECRET_KEY="${SOURCE_SECRET_KEY:-minioadmin}"
export TARGET_ACCESS_KEY="${TARGET_ACCESS_KEY:-minioadmin}"
export TARGET_SECRET_KEY="${TARGET_SECRET_KEY:-minioadmin}"

SOURCE="${SOURCE_ENDPOINT:-http://localhost:9000}"
TARGET="${TARGET_ENDPOINT:-http://localhost:9002}"
BUCKET="${BUCKET:-migration-test}"
REPORT_DIR="${REPORT_DIR:-out}"

[ -x bin/migration-verify ] || go build -o bin/migration-verify ./cmd/migration-verify
mkdir -p "$REPORT_DIR"

exec ./bin/migration-verify \
  --source "$SOURCE" --target "$TARGET" \
  --source-name "${SOURCE_NAME:-MinIO A}" --target-name "${TARGET_NAME:-MinIO B}" \
  --bucket "$BUCKET" \
  --json "$REPORT_DIR/migration-report.json" --html "$REPORT_DIR/migration-report.html" \
  "$@"
