#!/bin/bash
set -e

# --- Claude Config Setup ---
# Individual auth files are mounted read-only to /tmp/ staging paths.
# Copy them to the writable home directory.

mkdir -p /home/agent/.claude

# Copy .claude.json (main config)
if [ -f /tmp/.claude.json.host ]; then
  cp /tmp/.claude.json.host /home/agent/.claude.json
fi

# Copy credentials
if [ -f /tmp/.claude-credentials.json ]; then
  cp /tmp/.claude-credentials.json /home/agent/.claude/.credentials.json
fi

# Copy settings
if [ -f /tmp/.claude-settings.json ]; then
  cp /tmp/.claude-settings.json /home/agent/.claude/settings.json
fi
if [ -f /tmp/.claude-settings-local.json ]; then
  cp /tmp/.claude-settings-local.json /home/agent/.claude/settings.local.json
fi

# --- Git Config ---
if [ -n "$GIT_TOKEN" ]; then
  git config --global credential.helper '!f() { echo "password=$GIT_TOKEN"; }; f'
  git config --global user.email "${GIT_EMAIL:-klaudio-agent@local}"
  git config --global user.name "${GIT_USER:-Klaudio Agent}"
fi

if ! git config --global user.email > /dev/null 2>&1; then
  git config --global user.email "klaudio-agent@local"
  git config --global user.name "Klaudio Agent"
fi

# --- Launch Claude Code ---
if [ -n "$CLAUDE_PROMPT" ]; then
  exec claude --dangerously-skip-permissions \
    --output-format stream-json \
    --verbose \
    -p "$CLAUDE_PROMPT"
else
  exec claude --dangerously-skip-permissions
fi
