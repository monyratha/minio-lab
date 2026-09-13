BIN := bin/migration-verify

.PHONY: build test vet fmt clean verify

build:
	go build -o $(BIN) ./cmd/migration-verify

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin

# Convenience target for the lab environment (MinIO A -> MinIO B).
verify: build
	./run-verify.sh
