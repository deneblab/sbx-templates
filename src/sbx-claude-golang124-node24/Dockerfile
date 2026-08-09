# Stage 1: deps — install runtimes; rebuilt only when dependencies change
FROM docker/sandbox-templates:claude-code AS deps

USER root

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    zsh \
    jq \
    unzip \
    procps \
 && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
    nodejs \
 && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.24.2
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
    | tar -C /usr/local -xz

RUN mkdir -p \
    /workspace/.sbx-cache/go/pkg \
    /workspace/.sbx-cache/go/bin \
    /workspace/.sbx-cache/npm \
 && chown -R agent:agent /home/agent /workspace

USER agent

ENV PATH="/usr/local/go/bin:/workspace/.sbx-cache/go/bin:${PATH}"
ENV GOPATH=/workspace/.sbx-cache/go
ENV GOMODCACHE=/workspace/.sbx-cache/go/pkg/mod
ENV GOBIN=/workspace/.sbx-cache/go/bin
ENV npm_config_cache=/workspace/.sbx-cache/npm
ENV SBX_SANDBOX=true

RUN go version && node --version && npm --version

# Stage 2: claude — install/update Claude Code; rebuild this stage alone to update
FROM deps AS claude

USER root
RUN curl -fsSL https://claude.ai/install.sh | bash
USER agent

ARG VERSION=dev
ARG SHORT_SHA=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${SHORT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}"
