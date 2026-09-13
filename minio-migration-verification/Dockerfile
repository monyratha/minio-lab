FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /migration-verify ./cmd/migration-verify

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /migration-verify /usr/local/bin/migration-verify
WORKDIR /reports
ENTRYPOINT ["migration-verify"]
