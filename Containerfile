FROM golang:1.24.0 AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/artifact-handoff-resolver ./cmd/artifact-handoff-resolver

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/artifact-handoff-resolver /usr/local/bin/artifact-handoff-resolver

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/artifact-handoff-resolver"]
