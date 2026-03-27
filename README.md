# turbomux

Dead simple coding agent orchestration. No framework, no server, no SDK — just tmux.

Spawn Claude Code, Codex, Pi, Aider, or any coding agent in tmux panes. Monitor them. Send them tasks. Read their output. Track tickets with [beads](https://github.com/steveyegge/beads). That's it.

```bash
turbomux init                    # init beads ticket tracker
turbomux spawn backend ~/app "Fix the auth bug"
turbomux spawn frontend ~/app "Add dark mode"
turbomux status                  # panes + ready tickets
turbomux ready                   # what's unblocked?
turbomux assign slate-mjm 0:1.0  # assign ticket to agent
turbomux peek 0:1.0              # what's the agent doing?
```

No YAML pipelines. No agent frameworks. No LangChain. Just terminals doing work.

Powered by [beads](https://github.com/steveyegge/beads) — the agent-native issue tracker built on [Dolt](https://github.com/dolthub/dolt) (git for data). Tickets track dependencies, agents claim work atomically, and all worktrees share one database. Or skip beads entirely and use your own system (Linear, ClickUp, nothing).

---

## Install

```bash
go install github.com/epuerta9/turbomux@latest
```

Or build from source:
```bash
git clone https://github.com/epuerta9/turbomux.git
cd turbomux
go build -o turbomux .
cp turbomux ~/.local/bin/
```

### Optional: Install beads + dolt for ticket tracking
```bash
# Dolt (git-for-data SQL database)
brew install dolt            # macOS
# or: https://docs.dolthub.com/introduction/installation

# Beads (agent-native issue tracker)
go install github.com/steveyegge/beads/cmd/bd@latest
```

Without beads, turbomux is pure tmux orchestration. With beads, you get dependency-aware ticket tracking that all your agents share.

---

## Quick Start

### Without beads (pure tmux)

```bash
# Spawn agents
turbomux spawn backend ~/projects/app "Fix the auth bug"
turbomux spawn frontend ~/projects/app "Add dark mode"

# Monitor
turbomux status
turbomux peek 0:1.0

# Send follow-up
turbomux send 0:1.0 "Now write tests"
```

### With beads (ticket tracking)

```bash
# Init beads in your project
cd ~/projects/my-app
turbomux init

# Create tickets
bd create "Auth middleware rewrite" --type task --priority 1
bd create "Add rate limiting" --type task --priority 2
bd create "Integration tests" --type task --priority 2
bd dep add <test-id> <auth-id>    # tests depend on auth

# See what's ready
turbomux ready
# → auth and rate-limiting are ready (no blockers)
# → tests are blocked (waiting on auth)

# Spawn agents — beads auto-creates worktrees with shared DB
turbomux spawn auth ~/projects/my-app "Run bd ready, claim a ticket with bd update <id> --claim, and start working"
turbomux spawn api ~/projects/my-app "Run bd ready, claim a ticket, start working"

# Both agents see the same tickets, claim atomically (no race conditions)

# Monitor everything
turbomux status
# 🔨 working  0:1.0  auth agent
# 🔨 working  0:1.1  api agent
# 📋 Ready tickets:
# ○ slate-q5y  P2  Integration tests (blocked by auth)

# When auth finishes, tests auto-unblock
turbomux ready
# → Integration tests is now ready!

# Assign it
turbomux assign <test-id> 0:1.0
```

---

## Commands

### Tmux Orchestration

| Command | Description |
|---------|-------------|
| `turbomux list` | List all tmux panes with idle/working status |
| `turbomux status` | Agent panes + beads ready/blocked summary |
| `turbomux peek <pane> [lines]` | Read last N lines of a pane (default 30) |
| `turbomux history <pane>` | Dump entire scrollback buffer |
| `turbomux send [-f] <pane> <msg>` | Send input to a pane (checks idle first, `-f` to force) |
| `turbomux spawn [--agent=X] <name> <dir> [prompt]` | Create pane, launch agent, optionally send prompt |
| `turbomux window <name> <count>` | Create named window with N panes |
| `turbomux kill <pane>` | Kill a pane or window |
| `turbomux keys <pane> <key...>` | Send special keys (Enter, Up, Down, C-c, etc.) |
| `turbomux config` | Show current configuration |
| `turbomux json` | Machine-readable JSON of all panes |

### Ticket Tracking (beads)

| Command | Description |
|---------|-------------|
| `turbomux init [dir]` | Initialize beads in a project (`bd init`) |
| `turbomux tickets` | List all open tickets (`bd list`) |
| `turbomux board` | Board overview with counts (`bd status`) |
| `turbomux ready` | Show unblocked tickets ready for work (`bd ready`) |
| `turbomux assign <ticket> <pane>` | Claim ticket + send details to an agent |

---

## Agent Configuration

```yaml
# ~/.config/turbomux/config.yaml

# Default coding agent
agent: claude-yolo    # claude --dangerously-skip-permissions

# Built-in agents:
#   claude-yolo  = claude --dangerously-skip-permissions
#   claude       = claude (with permission prompts)
#   codex        = codex
#   pi           = pi
#   aider        = aider
# Or any command: agent: "my-tool --flag"

# Ticket tracker: "beads" (default) or "none"
# beads: turbomux spawn auto-creates worktrees with shared ticket DB
# none: pure tmux, coordinate however you want (Linear, ClickUp, etc.)
tracker: beads

# Pane layout: tiled, even-horizontal, even-vertical
layout: tiled
```

Override per-spawn:
```bash
turbomux spawn --agent=codex backend ~/app "Fix the bug"
turbomux spawn --no-tracker worker ~/app "Just code, no tickets"
```

---

## How Beads Integration Works

[Beads](https://github.com/steveyegge/beads) is a Go CLI issue tracker by Steve Yegge, built on [Dolt](https://github.com/dolthub/dolt) (a MySQL database with git semantics). It's designed for AI agents — hash-based IDs, dependency graphs, atomic claim, memory decay.

When you `turbomux spawn` with `tracker: beads`, turbomux:

1. Checks if the target directory has a `.beads/` database
2. Creates a git worktree via `bd worktree create` (not `git worktree add`)
3. The worktree gets a `.beads/redirect` file pointing to the main project's database
4. **All worktrees share one ticket database** — agents in different worktrees see the same tickets
5. Agents use `bd ready` to find work, `bd update --claim` to take a ticket, `bd note` to log progress, `bd close` when done

No race conditions. Two agents can't claim the same ticket — `bd update --claim` is an atomic SQL operation.

### Don't want beads?

Set `tracker: none` in config. turbomux becomes pure tmux orchestration — no opinions on how you track work. Use Linear MCP, ClickUp, GitHub Issues, or nothing at all.

---

## Interactive Prompt Handling

When spawning agents, turbomux auto-handles common startup prompts:

- **"Do you trust this project directory?"** → auto-accepts
- **"Select account"** → selects the default option
- **Yes/No confirmations** → auto-confirms

For anything else:
```bash
turbomux keys 0:1.0 Down Down Enter    # navigate a selector
turbomux keys 0:1.0 Escape             # dismiss a dialog
turbomux keys 0:1.0 C-c                # ctrl+c to interrupt
```

---

## Pane Targeting

Use tmux target syntax:
- `0:1.0` — session 0, window 1, pane 0
- `agents.0` — window named "agents", pane 0
- `agents` — the window itself (for `kill`)

---

## Claude Code Skill

turbomux includes a Claude Code skill at `.claude/commands/turbomux.md`. Install globally:

```bash
cp .claude/commands/turbomux.md ~/.claude/commands/turbomux.md
```

Then any Claude Code session can use `/turbomux` or ask Claude to "check on my agents."

---

## Example: Full Multi-Agent Sprint

```bash
# 1. Init project with beads
cd ~/projects/my-app
turbomux init

# 2. Create tickets with dependencies
bd create "Database schema migration" --type task -p 1
bd create "API endpoints for users" --type task -p 1
bd create "Frontend user dashboard" --type task -p 2
bd create "E2E tests" --type task -p 2
bd dep add <api-id> <db-id>         # API depends on DB
bd dep add <frontend-id> <api-id>    # frontend depends on API
bd dep add <e2e-id> <frontend-id>    # E2E depends on frontend

# 3. Check the ready frontier
turbomux ready
# → Only "Database schema migration" is ready (everything else is blocked)

# 4. Spawn agents
turbomux spawn db-agent ~/projects/my-app "Run bd ready and start on the database migration"
turbomux spawn api-agent ~/projects/my-app "Run bd ready — if nothing is ready, wait. Check back with bd ready periodically."

# 5. As DB migration completes, API ticket auto-unblocks
# api-agent can now claim it

# 6. Monitor from your main session
turbomux status
turbomux board
turbomux peek 0:1.0 50

# 7. When everything's done
bd list --status closed    # see what was accomplished
bd status                  # sprint summary
```
