# Claude Code: native binary (glibc build) on Debian. Sketch; not built by CI.
FROM --platform=linux/amd64 debian:bookworm-slim AS fetch
ARG CLAUDE_VERSION=stable
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates bash && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://claude.ai/install.sh | bash -s "${CLAUDE_VERSION}" \
 && cp -L /root/.local/bin/claude /claude

FROM --platform=linux/amd64 debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps \
 && rm -rf /var/lib/apt/lists/*
COPY --from=fetch /claude /usr/local/bin/claude
COPY smoke.sh /usr/local/bin/smoke.sh
ENV SMOKE_BACKEND=claude DISABLE_AUTOUPDATER=1
# Secrets at run time only (names, never values): CLAUDE_CODE_OAUTH_TOKEN (Peter-only) or ANTHROPIC_API_KEY
