# turbomux

tmux agent orchestrator — spawn, monitor, and command AI coding agents across tmux panes.

## Install

```bash
go install github.com/epuerta9/turbomux@latest
```

Or build from source:
```bash
git clone https://github.com/epuerta9/turbomux.git
cd turbomux
go build -o turbomux .
cp turbomux ~/.local/bin/  # or /usr/local/bin/
```

## Quick Start

```bash
# See what's running
turbomux list

# Check agent status (idle vs working)
turbomux status

# Spawn a new agent in a directory
turbomux spawn backend ~/projects/my-app "Fix the auth bug in src/auth.ts"

# Read what an agent is doing
turbomux peek 0:1.0
turbomux peek 0:1.0 100     # last 100 lines
turbomux history 0:1.0       # full scrollback

# Send a follow-up instruction to an idle agent
turbomux send 0:1.0 "Now write tests for the auth fix"

# Force-send even if agent looks busy
turbomux send -f 0:1.0 "Stop and focus on the login bug instead"

# Create a window with 3 agent panes
turbomux window agents 3

# Kill a pane
turbomux kill 0:2.1
```

## Agent Configuration

By default, turbomux launches `claude --dangerously-skip-permissions` (`cc`). Change this in your config:

```bash
mkdir -p ~/.config/turbomux
cat > ~/.config/turbomux/config.yaml << 'EOF'
# Default coding agent
agent: cc

# Options: cc, claude, codex, pi, aider, or any custom command
# agent: codex
# agent: "my-agent --custom-flag"

# tmux session name
session: "0"

# Default window name for spawned agents
default_window: agents

# Pane layout: tiled, even-horizontal, even-vertical, main-horizontal, main-vertical
layout: tiled
EOF
```

Override per-spawn:
```bash
turbomux spawn --agent=codex backend ~/projects/app "Fix the bug"
turbomux spawn --agent=pi frontend ~/projects/web "Add dark mode"
turbomux spawn --agent="aider --model gpt-4" refactor ~/projects/lib
```

## Commands

| Command | Description |
|---------|-------------|
| `turbomux list` | List all tmux panes with idle/working status |
| `turbomux status` | Show only agent panes with status summary |
| `turbomux peek <pane> [lines]` | Read last N lines of a pane (default 30) |
| `turbomux history <pane>` | Dump entire scrollback buffer |
| `turbomux send [-f] <pane> <msg>` | Send input to a pane (checks idle first) |
| `turbomux spawn [--agent=X] <name> <dir> [prompt]` | Create pane, launch agent, send prompt |
| `turbomux window <name> <count>` | Create named window with N panes |
| `turbomux kill <pane>` | Kill a pane or window |
| `turbomux config` | Show current configuration |
| `turbomux json` | Machine-readable JSON of all panes |

## Pane Targeting

Use tmux target syntax:
- `0:1.0` — session 0, window 1, pane 0
- `agents.0` — window named "agents", pane 0
- `agents` — the window itself (for `kill`)

## How It Works

turbomux wraps tmux commands to manage AI coding agents:

- **`list`/`status`** — Uses `tmux list-panes` + `tmux capture-pane` to detect idle agents (looks for `❯` prompt for Claude Code, `codex>` for Codex, `$` for shell)
- **`peek`/`history`** — Uses `tmux capture-pane -p -S -N` to read scrollback
- **`send`** — Uses `tmux send-keys` to type into panes. Checks idle state first to avoid interrupting working agents
- **`spawn`** — Uses `tmux split-window` + `send-keys` to create a pane, cd to a directory, launch the configured agent, and optionally send an opening prompt
- **`window`** — Uses `tmux new-window` + `split-window` + `select-layout` to create multi-pane layouts

## Use with Claude Code (as a skill)

turbomux includes a Claude Code skill so Claude can orchestrate agents:

```bash
# Copy the skill to your Claude Code config
cp skill-turbomux.md ~/.claude/commands/turbomux.md
```

Then in Claude Code: `/turbomux status` or ask Claude to "check on my agents" and it will use turbomux.

## Example: Multi-Agent Workflow

```bash
# 1. Create worktrees for parallel work
cd ~/projects/my-app
git worktree add ../my-app-agent-a -b feature/auth
git worktree add ../my-app-agent-b -b feature/api
git worktree add ../my-app-agent-c -b feature/ui

# 2. Create a 3-pane window
turbomux window agents 3

# 3. Spawn agents with their briefs
turbomux spawn auth ~/projects/my-app-agent-a "Read AGENTS.md and start on the auth ticket"
turbomux spawn api ~/projects/my-app-agent-b "Read AGENTS.md and start on the API ticket"
turbomux spawn ui ~/projects/my-app-agent-c "Read AGENTS.md and start on the UI ticket"

# 4. Monitor
turbomux status

# 5. Send follow-ups as they finish
turbomux send agents.0 "Auth looks good, now write integration tests"
```
