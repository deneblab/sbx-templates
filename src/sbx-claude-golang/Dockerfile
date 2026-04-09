FROM docker/sandbox-templates:claude-code

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

ARG GO_VERSION=1.24.2
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
    | tar -C /usr/local -xz

RUN mkdir -p \
    /workspace/.sbx-cache/go/pkg \
    /workspace/.sbx-cache/go/bin \
 && chown -R agent:agent /home/agent /workspace

USER agent

ENV PATH="/usr/local/go/bin:/workspace/.sbx-cache/go/bin:${PATH}"
ENV GOPATH=/workspace/.sbx-cache/go
ENV GOMODCACHE=/workspace/.sbx-cache/go/pkg/mod
ENV GOBIN=/workspace/.sbx-cache/go/bin

RUN go version

ARG VERSION=dev
ARG SHORT_SHA=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${SHORT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}"
