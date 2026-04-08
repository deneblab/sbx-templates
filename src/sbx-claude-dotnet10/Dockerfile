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
    software-properties-common \
 && add-apt-repository ppa:dotnet/backports -y \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
    dotnet-sdk-10.0 \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p \
    /home/agent/.nuget/packages \
    /home/agent/.local/share/NuGet \
    /workspace/.sbx-cache/nuget/packages \
 && chown -R agent:agent /home/agent /workspace

USER agent

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1
ENV DOTNET_NOLOGO=1
ENV NUGET_PACKAGES=/workspace/.sbx-cache/nuget/packages

RUN dotnet --info

ARG VERSION=dev
ARG SHORT_SHA=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${SHORT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}"