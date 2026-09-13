#!/usr/bin/env sh
# Waits until Chorus reports the initial copy as finished.
#
# 'chorctl repl add' returns at once; the worker then lists and copies the
# objects in the background. Verifying before that finishes reports a target
# bucket that does not exist yet.
set -eu

API="${CHORUS_API:-http://localhost:9671}"
TIMEOUT="${TIMEOUT:-180}"

echo "waiting for the initial copy to finish (timeout ${TIMEOUT}s) ..."

waited=0
while [ "$waited" -lt "$TIMEOUT" ]; do
  if curl -s -X POST "$API/replication" -d '{}' | grep -Eq '"isInitDone" *: *true'; then
    echo "initial copy finished after ${waited}s"
    exit 0
  fi
  waited=$((waited + 2))
  sleep 2
done

echo "timed out after ${TIMEOUT}s waiting for the initial copy" >&2
echo "check 'chorctl repl' and 'docker logs docker-compose-worker-1'" >&2
exit 1
