# minio-lab — one command per part of the lab.
# Every target is safe to re-run. See GUIDE.md for the manual steps.
#
# All settings live in one place: .env in this directory. Start from
# .env.example ("cp .env.example .env"). Everything below is only the
# fallback used when .env does not set a value, and any of them can also be
# overridden on the command line: make verify BUCKET=other-bucket

-include .env
export

NETWORK     := minio-migration
CHORUS_REF  := 8b68045
BUCKET      ?= all
MC_IMAGE    ?= quay.io/minio/mc:latest
CHORUS_USER ?= user1

# 'repl' and 'verify' cover every bucket on the source by default; set
# BUCKET to a name to handle only that one. 'seed' always creates the lab
# bucket, since 'all' is not a bucket.
SEED_BUCKET := $(if $(filter all,$(BUCKET)),migration-test,$(BUCKET))

# Verification depth: 1 listing/ETag, 2 adds metadata and tags, 3 hashes
# every object on both sides (downloads everything).
VERIFY_LEVEL ?= 2

# What the Chorus worker connects to (container view).
SOURCE_URL        ?= http://minio-a:9000
TARGET_URL        ?= http://minio-b:9000
SOURCE_ACCESS_KEY ?= minioadmin
SOURCE_SECRET_KEY ?= minioadmin
TARGET_ACCESS_KEY ?= minioadmin
TARGET_SECRET_KEY ?= minioadmin
SOURCE_PROVIDER   ?= Minio
TARGET_PROVIDER   ?= Minio

# What your laptop connects to (seed and verification).
SOURCE_URL_LOCAL  ?= http://localhost:9000
TARGET_URL_LOCAL  ?= http://localhost:9002

.DEFAULT_GOAL := help
.PHONY: help net minio-up minio-down seed chorus-clone chorus-config chorus-up \
        chorus-down chorus-reload chorus-wait repl verify lab status down clean config

help:
	@echo "Storage only (Path A)"
	@echo "  make minio-up      start MinIO A (:9000) and B (:9002) on the shared network"
	@echo "  make seed          create bucket '$(SEED_BUCKET)' with sample objects if it is missing"
	@echo "  make minio-down    stop both MinIO containers"
	@echo ""
	@echo "Chorus only (Path B)"
	@echo "  make chorus-clone  clone chorus at $(CHORUS_REF) and apply the lab patch"
	@echo "  make chorus-config write the chorus storage config from .env"
	@echo "  make chorus-reload apply .env changes to a running worker"
	@echo "  make chorus-up     start redis, worker (:9671) and web-ui (:8080)"
	@echo "  make repl          add replication A -> B for bucket '$(BUCKET)' ('all' = every bucket)"
	@echo "  make chorus-down   stop the chorus stack"
	@echo ""
	@echo "Verification only (Path C)"
	@echo "  make verify        build migration-verify and check A against B (VERIFY_LEVEL=$(VERIFY_LEVEL))"
	@echo ""
	@echo "Everything"
	@echo "  make lab           run the whole lab end to end"
	@echo "  make config        show the settings in use"
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
	  ENDPOINT=$(SOURCE_URL_LOCAL) BUCKET=$(SEED_BUCKET) scripts/seed.sh; \
	else \
	  echo "no local mc found, seeding with $(MC_IMAGE)"; \
	  docker run --rm --network $(NETWORK) -v "$$PWD/scripts:/scripts:ro" \
	    --entrypoint sh $(MC_IMAGE) \
	    -c "ENDPOINT=$(SOURCE_URL) BUCKET=$(SEED_BUCKET) /scripts/seed.sh"; \
	fi

chorus-clone:
	[ -d chorus ] || git clone https://github.com/clyso/chorus
	git -C chorus checkout $(CHORUS_REF)
	git -C chorus apply --reverse --check ../chorus-lab-config.patch 2>/dev/null \
	  || git -C chorus apply ../chorus-lab-config.patch

# Generates chorus/docker-compose/s3-credentials.yaml from .env, so the
# chorus/ clone itself never has to be edited by hand.
chorus-config: chorus-clone
	@scripts/render-s3-credentials.sh

# The worker reads the config only at startup, so .env changes need this.
chorus-reload: chorus-config
	cd chorus/docker-compose && docker compose up -d --force-recreate worker

chorus-up: net chorus-config
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
#
# BUCKET=all adds one bucket-level policy per source bucket instead of a
# user-level policy: Chorus refuses a user-level policy once any bucket-level
# one exists, and 'repl buckets' lists exactly the buckets that have none yet,
# so this is safe to re-run.
repl: chorus-wait
ifeq ($(BUCKET),all)
	@for b in $$(chorctl repl buckets -u $(CHORUS_USER) -f main -t follower); do \
	  echo "adding replication for bucket $$b"; \
	  chorctl repl add -u $(CHORUS_USER) -f main -t follower -b $$b; \
	done
else
	chorctl repl add -u $(CHORUS_USER) -f main -t follower -b $(BUCKET) || true
endif
	@scripts/wait-replication.sh
	chorctl repl

verify:
	cd minio-migration-verification && $(MAKE) build && \
	  SOURCE_ENDPOINT=$(SOURCE_URL_LOCAL) TARGET_ENDPOINT=$(TARGET_URL_LOCAL) \
	  BUCKET=$(BUCKET) ./run-verify.sh --smoke-test --level $(VERIFY_LEVEL)

config:
	@echo "chorus worker sees   main     $(SOURCE_URL)"
	@echo "                     follower $(TARGET_URL)"
	@echo "this machine sees    source   $(SOURCE_URL_LOCAL)"
	@echo "                     target   $(TARGET_URL_LOCAL)"
	@echo "bucket               $(BUCKET)"
	@echo "chorus user          $(CHORUS_USER)"
	@echo "verify level         $(VERIFY_LEVEL)"
	@if [ -f .env ]; then echo "settings from        .env"; \
	else echo "settings from        built-in lab defaults (no .env file)"; fi

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
