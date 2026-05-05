# Agent and Worker binary for Unikraft Cloud (HTTP on $PORT, default 8080).
# Build from repo root:
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ukc-agent ./cmd/ukc-agent
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o drover-code ./cmd/drover-code
#   docker build -f cmd/ukc-agent/Dockerfile -t your-registry/ukc-agent:latest .
FROM alpine:latest
RUN apk add --no-cache ca-certificates git openssh tzdata bash
COPY ukc-agent /usr/local/bin/ukc-agent
COPY drover-code /usr/local/bin/drover-code
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/usr/local/bin/ukc-agent"]
