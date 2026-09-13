# minio-lab — one command per part of the lab.
# Every target is safe to re-run. See GUIDE.md for the manual steps.

NETWORK     := minio-migration
CHORUS_REF  := 8b68045
BUCKET      ?= migration-test
MC_IMAGE    ?= quay.io/minio/mc:latest
CHORUS_USER ?= user1

.DEFAULT_GOAL := help
.PHONY: help net minio-up minio-down seed chorus-clone chorus-up chorus-down \
        chorus-wait repl verify lab status down clean

help:
	@echo "Storage only (Path A)"
	@echo "  make minio-up      start MinIO A (:9000) and B (:9002) on the shared network"
	@echo "  make seed          create bucket '$(BUCKET)' with sample objects if it is missing"
	@echo "  make minio-down    stop both MinIO containers"
	@echo ""
	@echo "Chorus only (Path B)"
	@echo "  make chorus-clone  clone chorus at $(CHORUS_REF) and apply the lab patch"
	@echo "  make chorus-up     start redis, worker (:9671) and web-ui (:8080)"
	@echo "  make repl          add replication A -> B for bucket '$(BUCKET)'"
	@echo "  make chorus-down   stop the chorus stack"
	@echo ""
	@echo "Verification only (Path C)"
	@echo "  make verify        build migration-verify and check A against B"
	@echo ""
	@echo "Everything"
	@echo "  make lab           run the whole lab end to end"
	@echo "  make status        show which containers are up"
	@echo "  make down          stop everything"
	@echo "  make clean         stop everything and delete data, chorus clone, binaries"

net:
	docker network inspect $(NETWORK) >/dev/null 2>&1 || docker network create $(NETWORK)

minio-up: net
	cd minio-a && docker compose up -d
	cd minio-b && docker compose up -d
	docker network connect $(NETWORK) minio-a 2>/dev/null || true
	docker network connect $(NETWORK) minio-b 2>/dev/null || true

minio-down:
	cd minio-a && docker compose down
	cd minio-b && docker compose down

# Uses your local mc client when you have one, otherwise the mc container.
# minio/mc is no longer on Docker Hub, so the image comes from quay.io.
seed:
	@if command -v mc >/dev/null 2>&1; then \
	  echo "seeding with the local mc client"; \
	  ENDPOINT=http://localhost:9000 BUCKET=$(BUCKET) scripts/seed.sh; \
	else \
	  echo "no local mc found, seeding with $(MC_IMAGE)"; \
	  docker run --rm --network $(NETWORK) -v "$$PWD/scripts:/scripts:ro" \
	    --entrypoint sh $(MC_IMAGE) \
	    -c "ENDPOINT=http://minio-a:9000 BUCKET=$(BUCKET) /scripts/seed.sh"; \
	fi

chorus-clone:
	[ -d chorus ] || git clone https://github.com/clyso/chorus
	git -C chorus checkout $(CHORUS_REF)
	git -C chorus apply --reverse --check ../chorus-lab-config.patch 2>/dev/null \
	  || git -C chorus apply ../chorus-lab-config.patch

chorus-up: net chorus-clone
	cd chorus/docker-compose && docker compose up -d

chorus-wait:
	@echo "waiting for the chorus worker API on :9671 ..."
	@waited=0; while [ $$waited -lt 120 ]; do \
	  curl -sf http://localhost:9671/storage >/dev/null && { echo "worker is up"; exit 0; }; \
	  waited=$$((waited + 2)); sleep 2; \
	done; \
	echo "worker did not answer within 120s; see 'docker logs docker-compose-worker-1'" >&2; \
	exit 1

chorus-down:
	[ -d chorus/docker-compose ] || exit 0; cd chorus/docker-compose && docker compose down

# 'repl add' returns immediately; the copy runs in the background. Waiting
# here is what makes 'make lab' verify a finished migration instead of an
# empty target bucket.
repl: chorus-wait
	chorctl repl add -u $(CHORUS_USER) -f main -t follower -b $(BUCKET) || true
	@scripts/wait-replication.sh
	chorctl repl

verify:
	cd minio-migration-verification && $(MAKE) build && BUCKET=$(BUCKET) ./run-verify.sh --smoke-test

lab: minio-up seed chorus-up repl verify

status:
	docker ps --filter network=$(NETWORK) --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

down: chorus-down minio-down
	docker network rm $(NETWORK) 2>/dev/null || true

# Chorus keeps its replication policies in the redis volume. Without '-v' a
# fresh lab would still think the old replication is finished and copy nothing.
clean: down
	[ -d chorus/docker-compose ] && (cd chorus/docker-compose && docker compose down -v) || true
	rm -rf chorus minio-a/data minio-b/data minio-migration-verification/bin
