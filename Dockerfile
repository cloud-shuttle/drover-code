# Agent and Worker binary for Unikraft Cloud (HTTP on $PORT, default 8080).
# Build from repo root:
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ukc-agent ./cmd/ukc-agent
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o drover-code ./cmd/drover-code
#   docker build -f cmd/ukc-agent/Dockerfile -t your-registry/ukc-agent:latest .
FROM --platform=linux/amd64 golang:1.26-alpine AS builder
WORKDIR /src
COPY drover-libs/ ./drover-libs/
COPY drover-warden/ ./drover-warden/
COPY drover-code/ ./drover-code/
WORKDIR /src/drover-code
RUN apk add --no-cache gcc musl-dev
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /ukc-agent ./cmd/ukc-agent
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /drover-code ./cmd/drover-code

FROM --platform=linux/amd64 alpine:3.18
RUN apk add --no-cache ca-certificates git openssh tzdata bash
COPY --from=builder /ukc-agent /usr/local/bin/ukc-agent
COPY --from=builder /drover-code /usr/local/bin/drover-code
# Ship default safe Warden Beads for unikernel preset (Option B governance).
RUN mkdir -p /usr/local/share/drover-code/beads
COPY --from=builder /src/drover-code/beads/ /usr/local/share/drover-code/beads/
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/usr/local/bin/ukc-agent"]
