# Gemini CLI: Node.js package on Debian (glibc). Sketch; not built by CI.
FROM --platform=linux/amd64 node:22-bookworm-slim
ARG GEMINI_VERSION=latest
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps \
 && rm -rf /var/lib/apt/lists/*
RUN npm install -g "@google/gemini-cli@${GEMINI_VERSION}" && npm cache clean --force
COPY smoke.sh /usr/local/bin/smoke.sh
ENV SMOKE_BACKEND=gemini NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt
# Secrets at run time only: GEMINI_API_KEY (API key only; subscription OAuth is out of scope)
