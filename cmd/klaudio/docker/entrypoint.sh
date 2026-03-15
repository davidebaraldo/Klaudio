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

# --- Rate Limit Retry Configuration ---
MAX_RETRIES=${KLAUDIO_MAX_RETRIES:-10}
RETRY_DELAY=${KLAUDIO_RETRY_DELAY:-30}
MAX_RETRY_DELAY=${KLAUDIO_MAX_RETRY_DELAY:-300}

# --- Launch Claude Code with Rate Limit Retry ---
if [ -n "$CLAUDE_PROMPT" ]; then
  RETRY_COUNT=0
  CURRENT_DELAY=$RETRY_DELAY
  STDERR_LOG=$(mktemp)

  while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    set +e
    claude --dangerously-skip-permissions \
      --output-format stream-json \
      --verbose \
      -p "$CLAUDE_PROMPT" 2>"$STDERR_LOG"
    EXIT_CODE=$?
    set -e

    # Success — exit cleanly
    if [ $EXIT_CODE -eq 0 ]; then
      rm -f "$STDERR_LOG"
      exit 0
    fi

    # Check if this is a rate limit error (check stderr and recent output)
    IS_RATE_LIMIT=false
    if [ -f "$STDERR_LOG" ]; then
      if grep -qiE "rate.limit|overloaded|too.many.requests|429|ResourceExhausted" "$STDERR_LOG" 2>/dev/null; then
        IS_RATE_LIMIT=true
      fi
    fi

    if [ "$IS_RATE_LIMIT" = "true" ]; then
      RETRY_COUNT=$((RETRY_COUNT + 1))

      # Emit a stream-json event so the Go backend can detect and broadcast it
      echo "{\"type\":\"system\",\"subtype\":\"rate_limit\",\"data\":{\"retry_in_seconds\":$CURRENT_DELAY,\"attempt\":$RETRY_COUNT,\"max_retries\":$MAX_RETRIES,\"message\":\"Rate limited by API. Waiting ${CURRENT_DELAY}s before retry ${RETRY_COUNT}/${MAX_RETRIES}.\"}}"

      sleep $CURRENT_DELAY

      # Exponential backoff
      CURRENT_DELAY=$((CURRENT_DELAY * 2))
      if [ $CURRENT_DELAY -gt $MAX_RETRY_DELAY ]; then
        CURRENT_DELAY=$MAX_RETRY_DELAY
      fi

      # Emit retry start event
      echo "{\"type\":\"system\",\"subtype\":\"rate_limit_retry\",\"data\":{\"attempt\":$RETRY_COUNT,\"max_retries\":$MAX_RETRIES,\"message\":\"Retrying after rate limit (attempt ${RETRY_COUNT}/${MAX_RETRIES})...\"}}"

      continue
    fi

    # Not a rate limit error — exit with original code
    rm -f "$STDERR_LOG"
    exit $EXIT_CODE
  done

  # Exhausted all retries
  rm -f "$STDERR_LOG"
  echo "{\"type\":\"system\",\"subtype\":\"rate_limit_exhausted\",\"data\":{\"attempts\":$RETRY_COUNT,\"message\":\"Rate limit retries exhausted after ${RETRY_COUNT} attempts.\"}}"
  exit 1
else
  exec claude --dangerously-skip-permissions
fi
