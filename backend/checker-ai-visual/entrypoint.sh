#!/bin/sh
set -eu

# Credentials are never baked into the image. The host folders are read-only
# mounts; copy them into this disposable container profile so each CLI can keep
# its normal runtime state without modifying host authentication files.
if [ -d /auth/codex ]; then
  mkdir -p /root/.codex
  cp -a /auth/codex/. /root/.codex/
fi
if [ -d /auth/claude ]; then
  mkdir -p /root/.claude
  cp -a /auth/claude/. /root/.claude/
fi

exec /app/checker-ai-visual
