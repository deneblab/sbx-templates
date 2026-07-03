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
 && rm -rf /var/lib/apt/lists/*

# uv — Astral's Python package/project manager (https://docs.astral.sh/uv/)
# Copy the standalone uv/uvx binaries from the official distroless image.
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

RUN mkdir -p \
    /workspace/.sbx-cache/uv \
 && chown -R agent:agent /home/agent /workspace

USER agent

# uv cache lives under the persisted sandbox cache dir; copy link mode is safe
# when that dir is a mount on a different filesystem than the image layers.
ENV UV_CACHE_DIR=/workspace/.sbx-cache/uv
ENV UV_LINK_MODE=copy
ENV PATH="/home/agent/.local/bin:${PATH}"
ENV SBX_SANDBOX=true

# Install the latest CPython and expose it as `python`/`python3` on PATH.
RUN uv python install --default \
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
