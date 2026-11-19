# opencode CLI client pointed at OpenCode Go (https://opencode.ai/inference/go). Sketch; not built by CI.
FROM --platform=linux/amd64 node:22-bookworm-slim
ARG OPENCODE_VERSION=latest
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps \
 && rm -rf /var/lib/apt/lists/*
RUN npm install -g "opencode-ai@${OPENCODE_VERSION}" && npm cache clean --force
COPY smoke.sh /usr/local/bin/smoke.sh
COPY opencode.json /etc/opencode/opencode.json
ENV SMOKE_BACKEND=opencode OPENCODE_CONFIG=/etc/opencode/opencode.json \
    NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt
# Secrets at run time only: OPENCODE_GO_API_KEY
