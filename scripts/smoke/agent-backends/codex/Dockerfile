# Codex CLI: static-PIE musl binary from GitHub releases. Sketch; not built by CI.
FROM --platform=linux/amd64 debian:bookworm-slim AS fetch
ARG CODEX_VERSION=0.170.0
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /tmp/codex.tgz \
      "https://github.com/openai/codex/releases/download/rust-v${CODEX_VERSION}/codex-x86_64-unknown-linux-musl.tar.gz" \
 && tar -xzf /tmp/codex.tgz -C /tmp && mv /tmp/codex-x86_64-unknown-linux-musl /codex

FROM --platform=linux/amd64 debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps \
 && rm -rf /var/lib/apt/lists/*
COPY --from=fetch /codex /usr/local/bin/codex
COPY smoke.sh /usr/local/bin/smoke.sh
# CODEX_HOME on the /work volume: auth.json must persist (ChatGPT login refreshes it in place).
ENV SMOKE_BACKEND=codex CODEX_HOME=/work/home/.codex
# Secrets at run time only: OPENAI_API_KEY (preferred). ChatGPT auth.json is Peter-only and lives on the volume.
