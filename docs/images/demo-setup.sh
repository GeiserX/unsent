# Sourced by demo.tape before it records. It builds a throwaway HOME, so no
# real draft, account or path shows, and points Claude Code at a dead address
# with a made-up key, so nothing leaves the machine. Needs unsent and claude
# on PATH.

export HOME="$(cd "$(mktemp -d)" && pwd -P)"
export ANTHROPIC_API_KEY=sk-ant-api03-demo-key-not-real-0000000000000000000000
export ANTHROPIC_BASE_URL=http://127.0.0.1:9
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
export BASH_SILENCE_DEPRECATION_WARNING=1
mkdir -p "$HOME/shop-api" "$HOME/bin"

# Skip Claude Code's onboarding, key prompt, trust dialog and what's-new notes.
claude_version="$(claude --version | cut -d' ' -f1)"
cat > "$HOME/.claude.json" <<EOF
{
  "hasCompletedOnboarding": true,
  "hasSeenAutoDefaultNotice": true,
  "hasResetAutoModeOptInForDefaultOffer": true,
  "lastReleaseNotesSeen": "$claude_version",
  "theme": "dark",
  "numStartups": 5,
  "customApiKeyResponses": {"approved": ["${ANTHROPIC_API_KEY: -20}"], "rejected": []},
  "projects": {"$HOME/shop-api": {"hasTrustDialogAccepted": true, "hasCompletedProjectOnboarding": true}}
}
EOF

# A recorder has no clipboard; this stand-in keeps what unsent copies.
printf '#!/bin/sh\ncat > "$HOME/clipboard"\n' > "$HOME/bin/pbcopy"
chmod +x "$HOME/bin/pbcopy"
export PATH="$HOME/bin:$PATH"

# Kills the agent the way a crash would, once the draft holding $1 is on disk.
crash_once_saved() {
  (
    until grep -qs "$1" "$HOME"/.local/state/unsent/drafts/*.json; do sleep 0.2; done
    sleep 2.5
    kill -9 $(pgrep -P "$(pgrep -P $$ -x unsent)")
  ) >/dev/null 2>&1 &
}

set +m
PS1='$ '
cd "$HOME/shop-api"
