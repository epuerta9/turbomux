You have access to `turbomux`, a tmux agent orchestrator CLI. Use it to spawn, monitor, and command AI coding agents running in tmux panes.

## Commands

```bash
# See all panes and their status
turbomux list

# Check which agents are idle vs working
turbomux status

# Read the last N lines of a pane's output (default 30)
turbomux peek <pane> [lines]

# Read the entire scrollback history of a pane
turbomux history <pane>

# Send a message/instruction to a pane (-f to force even if busy)
turbomux send [-f] <pane> "your message here"

# Spawn a new agent: creates pane, cd to dir, launches the configured agent, sends prompt
turbomux spawn <name> <dir> "opening prompt"

# Spawn with a specific agent (overrides config default)
turbomux spawn --agent=codex <name> <dir> "prompt"
# Built-in agents: cc (claude skip-perms), claude, codex, pi, aider
# Or any custom command: --agent="my-tool --flag"

# Create a named window with N panes
turbomux window <name> <count>

# Kill a pane or window
turbomux kill <pane>

# Show config (default agent, layout, etc.)
turbomux config

# Machine-readable JSON output of all panes
turbomux json
```

## Pane Targeting
- `0:1.0` — session 0, window 1, pane 0
- `agents.0` — window named "agents", pane 0

## Idle Detection
turbomux detects whether an agent is idle (at a prompt) or working by scanning the last few lines of pane output. It looks for prompt characters like `❯` (Claude Code), `codex>` (Codex), or `$` (shell). When you use `turbomux send`, it warns you if the agent appears busy and asks for confirmation (use `-f` to skip).

## Typical Orchestration Workflow

1. **Check status**: `turbomux status` to see which agents are idle/working
2. **Peek at progress**: `turbomux peek 0:1.0 50` to see what an agent has been doing
3. **Send next task**: When an agent is idle, `turbomux send 0:1.0 "Next: implement the review endpoint"`
4. **Spawn new agent**: `turbomux spawn fix-bug ~/projects/app "Fix the null pointer in auth.ts"`
5. **Monitor all**: `turbomux json` for structured status of everything

## Important Notes
- Always check `turbomux status` before sending to avoid interrupting a working agent
- Use `turbomux peek` to understand what an agent just completed before giving the next task
- Use `-f` flag on send only when you need to redirect an agent that's gone off track
- The `spawn` command waits up to 60 seconds for the agent to start before sending the prompt
