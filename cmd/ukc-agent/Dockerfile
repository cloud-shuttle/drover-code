# Static agent binary for Unikraft Cloud (HTTP on $PORT, default 8080).
# Build from repo root:
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ukc-agent ./cmd/ukc-agent
#   docker build -f cmd/ukc-agent/Dockerfile -t your-registry/ukc-agent:latest .
FROM scratch
COPY ukc-agent /ukc-agent
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/ukc-agent"]
