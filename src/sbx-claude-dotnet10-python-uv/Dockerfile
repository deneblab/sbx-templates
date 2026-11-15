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
# The SDK goes to /usr/share/dotnet, NOT /usr/lib/dotnet: dotnet-runtime-10.0 puts Ubuntu's
# source-built runtime and hostfxr there, and in a shared directory the highest version wins,
# so the SDK ran on the distro runtime and reported RID ubuntu.26.04-x64. NuGet does not know
# that RID, and every `dotnet tool install`/`restore` then failed with "DotnetToolSettings.xml
# was not found" (SBXT-024). /usr/bin/dotnet, DOTNET_ROOT and /etc/dotnet/install_location
# (read by apphost-launched global tools when DOTNET_ROOT is unset) all point here instead.
ARG DOTNET_SDK_VERSION=10.0.403
RUN curl -fsSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh \
 && bash /tmp/dotnet-install.sh --version "${DOTNET_SDK_VERSION}" --install-dir /usr/share/dotnet \
 && rm /tmp/dotnet-install.sh \
 && ln -sf /usr/share/dotnet/dotnet /usr/bin/dotnet \
 && case "$(dpkg --print-architecture)" in amd64) arch=x64 ;; *) arch="$(dpkg --print-architecture)" ;; esac \
 && mkdir -p /etc/dotnet \
 && echo /usr/share/dotnet > /etc/dotnet/install_location \
 && echo /usr/share/dotnet > "/etc/dotnet/install_location_${arch}"
ENV DOTNET_ROOT=/usr/share/dotnet

# uv — Astral's Python package/project manager (https://docs.astral.sh/uv/)
# Copy the standalone uv/uvx binaries from the official distroless image.
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

RUN mkdir -p \
    /home/agent/.nuget/packages \
    /home/agent/.local/share/NuGet \
    /workspace/.sbx-cache/nuget/packages \
    /workspace/.sbx-cache/uv \
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

# uv cache lives under the persisted sandbox cache dir; copy link mode is safe
# when that dir is a mount on a different filesystem than the image layers.
ENV UV_CACHE_DIR=/workspace/.sbx-cache/uv
ENV UV_LINK_MODE=copy
ENV PATH="/home/agent/.local/bin:${PATH}"
ENV SBX_SANDBOX=true

# Install the latest CPython and expose it as `python`/`python3` on PATH, then prove
# both runtimes work — a broken merge should fail here, not in a user's sandbox. The SDK
# must come from /usr/share/dotnet and report a portable RID, not the distro's (SBXT-024).
RUN uv python install --default \
 && dotnet --info \
 && test "$(dotnet --version)" = "${DOTNET_SDK_VERSION}" \
 && dotnet --list-sdks | grep -Fqx "${DOTNET_SDK_VERSION} [/usr/share/dotnet/sdk]" \
 && test "$(readlink -f /usr/bin/dotnet)" = /usr/share/dotnet/dotnet \
 && rids="$(dotnet --info | sed -n 's/^ *RID: *//p' | sort -u)" \
 && case "$rids" in linux-x64|linux-arm64) ;; *) echo "non-portable RID: $rids" >&2; exit 1 ;; esac \
 && uv --version && python --version && python3 --version

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
