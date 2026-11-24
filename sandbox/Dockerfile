# Image for `smithy config set auto.sandbox container`: each unattended worker runs as
#   docker|podman run --rm -i --cap-drop ALL --security-opt no-new-privileges -w <worktree>
#     -v <worktree> -v <git dir> -v ~/.smithy/plans -v ~/.smithy/warden -e <allowed names> <image> <agent argv>
# Build: docker build -t smithy-sandbox:latest sandbox/   (or podman build)
# Status: the image and the run command are unit-tested only; no live run yet.
FROM golang:1.24-bookworm

RUN apt-get update \
 && apt-get install -y --no-install-recommends git gh ca-certificates curl nodejs npm \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g @anthropic-ai/claude-code \
 && npm cache clean --force

# smithy runs the worker as the host user (--user uid:gid on Linux) with HOME=/tmp.
ENV HOME=/tmp
