# Image for `smithy config set auto.sandbox container`: each unattended worker runs as
#   docker|podman run --rm -i --cap-drop ALL --security-opt no-new-privileges --read-only --tmpfs /tmp
#     --pids-limit 512 --memory 4g --network none (auto.sandbox_network) -w <worktree>
#     -v <worktree> -v <git dir> (hooks and config :ro) -v ~/.smithy/plans -v ~/.smithy/warden
#     -e <allowed names> <image> <agent argv>
# Build: docker build -t smithy-sandbox:latest sandbox/   (or podman build)
# Bump: take the latest golang:<x.y.z>-bookworm tag and its index digest from Docker Hub, `npm view @anthropic-ai/claude-code version`,
# and the latest stable sst/opencode release (its linux tarballs' sha256 digests from the release page).
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea

RUN apt-get update \
 && apt-get install -y --no-install-recommends git gh ca-certificates nodejs npm \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code@2.1.331 \
 && npm cache clean --force

# smithy runs the worker as the host user (--user uid:gid, or podman --userns=keep-id) with
# HOME=/tmp on a tmpfs; the root filesystem is read-only.
ENV HOME=/tmp

# OpenCode from its GitHub release (not npm opencode-ai, an older line), checked against the release digests.
ARG OPENCODE_VERSION=2.4.0
ARG TARGETARCH
RUN set -e; case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
      arm64) a=arm64; sum=fed468566215fd38b26abab99018240c0d30772d3f45aee25621664ceff7e360 ;; \
      amd64) a=x64; sum=66cf6adeb02fbfe9ccbb1f6497fcf46cecf2d299a1dc3a7a32e01ea30ff20201 ;; \
      *) echo "unsupported arch" >&2; exit 1 ;; esac; \
    curl -fsSL -o /tmp/oc.tgz "https://github.com/sst/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${a}.tar.gz"; \
    echo "${sum}  /tmp/oc.tgz" | sha256sum -c -; \
    tar -xzf /tmp/oc.tgz -C /usr/local/bin opencode; rm /tmp/oc.tgz; \
    opencode --version
