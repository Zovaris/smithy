# Image for `smithy config set auto.sandbox container`: each unattended worker runs as
#   docker|podman run --rm -i --cap-drop ALL --security-opt no-new-privileges --read-only --tmpfs /tmp
#     --pids-limit 512 --memory 4g --network none (auto.sandbox_network) -w <worktree>
#     -v <worktree> -v <git dir> (hooks and config :ro) -v ~/.smithy/plans -v ~/.smithy/warden
#     -e <allowed names> <image> <agent argv>
# Build: docker build -t smithy-sandbox:latest sandbox/   (or podman build)
# Status: the image and the run command are unit-tested only; no live run yet.
# golang:1.24-bookworm, pinned by digest (multi-arch index).
FROM golang:1.24-bookworm@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac

RUN apt-get update \
 && apt-get install -y --no-install-recommends git gh ca-certificates nodejs npm \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code@2.1.331 \
 && npm cache clean --force

# smithy runs the worker as the host user (--user uid:gid, or podman --userns=keep-id) with
# HOME=/tmp on a tmpfs; the root filesystem is read-only.
ENV HOME=/tmp
