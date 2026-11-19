# Grok Build (grok CLI): static-PIE binary, rustls using the OS trust store. Sketch; not built by CI.
FROM --platform=linux/amd64 debian:bookworm-slim AS fetch
ARG GROK_VERSION=1.0.89
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates bash && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://x.ai/cli/install.sh | bash -s "${GROK_VERSION}" \
 && cp -L /root/.grok/bin/grok /grok

FROM --platform=linux/amd64 debian:bookworm-slim
# ca-certificates is required: rustls reads the OS trust store.
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash coreutils curl ca-certificates openssl git jq procps \
 && rm -rf /var/lib/apt/lists/*
COPY --from=fetch /grok /usr/local/bin/grok
COPY smoke.sh /usr/local/bin/smoke.sh
ENV SMOKE_BACKEND=grok
# Secrets at run time only: XAI_API_KEY
