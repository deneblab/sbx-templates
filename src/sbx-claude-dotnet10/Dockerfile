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
    dotnet-runtime-10.0 \
 && rm -rf /var/lib/apt/lists/*

# .NET SDK — both apt sources (Ubuntu's own archive and ppa:dotnet/backports) cap at the
# 10.0.1xx feature band, so a project whose global.json pins a 4xx SDK cannot be satisfied
# from apt at all. Install the SDK from the official script instead; dotnet-runtime-10.0
# above is present for its native dependencies (libicu and friends), which
# dotnet-install.sh does not install.
# DOTNET_SDK_VERSION is part of this layer's cache key, so bumping it rebuilds the SDK —
# an unpinned apt install could never invalidate itself.
ARG DOTNET_SDK_VERSION=10.0.401
RUN curl -fsSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh \
 && bash /tmp/dotnet-install.sh --version "${DOTNET_SDK_VERSION}" --install-dir /usr/lib/dotnet \
 && rm /tmp/dotnet-install.sh \
 && ln -sf /usr/lib/dotnet/dotnet /usr/bin/dotnet
ENV DOTNET_ROOT=/usr/lib/dotnet

RUN mkdir -p \
    /home/agent/.nuget/packages \
    /home/agent/.local/share/NuGet \
    /workspace/.sbx-cache/nuget/packages \
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

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1
ENV DOTNET_NOLOGO=1
ENV NUGET_PACKAGES=/workspace/.sbx-cache/nuget/packages
ENV SBX_SANDBOX=true

# Prove the pinned SDK is the one that actually resolves — a silent fall back to
# another feature band must fail here, not inside a user's sandbox.
RUN dotnet --info \
 && test "$(dotnet --version)" = "${DOTNET_SDK_VERSION}"

# Stage 2: claude — install/update Claude Code; rebuild this stage alone to update all
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
