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
 && curl -fsSL https://deb.nodesource.com/setup_26.x | bash - \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
    nodejs \
 && rm -rf /var/lib/apt/lists/*

# TARGETARCH is set by BuildKit (amd64, arm64), so the tarball matches the platform the
# image is built for instead of always being amd64 (which broke Apple Silicon hosts).
ARG GO_VERSION=1.27.3
ARG TARGETARCH
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz

RUN mkdir -p \
    /workspace/.sbx-cache/go/pkg \
    /workspace/.sbx-cache/go/bin \
    /workspace/.sbx-cache/npm \
 && chown -R agent:agent /home/agent /workspace

# Claude Code managed settings: no Claude attribution (Co-Authored-By trailer, "Generated with"
# line) in commits or PR descriptions. Managed settings sit above user and project settings, so
# this holds even though sbx recreates ~/.claude at sandbox start. Lives in the deps stage so
# --update-claude (which re-runs only the claude stage) keeps it. Rebuild without this block to
# get attribution back.
RUN mkdir -p /etc/claude-code \
 && printf '%s\n' '{"attribution":{"commit":"","pr":""}}' > /etc/claude-code/managed-settings.json \
 && chmod 0644 /etc/claude-code/managed-settings.json

USER agent

ENV PATH="/usr/local/go/bin:/workspace/.sbx-cache/go/bin:${PATH}"
ENV GOPATH=/workspace/.sbx-cache/go
ENV GOMODCACHE=/workspace/.sbx-cache/go/pkg/mod
ENV GOBIN=/workspace/.sbx-cache/go/bin
ENV npm_config_cache=/workspace/.sbx-cache/npm
ENV SBX_SANDBOX=true

# Prove the pinned Go and the Node major are what actually resolves — a wrong tarball or
# a NodeSource fall back must fail here, not inside a user's sandbox.
RUN go version \
 && test "$(go env GOVERSION)" = "go${GO_VERSION}" \
 && node --version | grep -q '^v26\.' \
 && node --version && npm --version

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
