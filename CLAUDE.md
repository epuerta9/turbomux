# turbomux

tmux agent orchestrator — spawn, monitor, and command AI coding agents across tmux panes.

## Setup

```bash
go build -o turbomux . && cp turbomux ~/.local/bin/
mkdir -p ~/.config/turbomux && cp turbomux.yaml ~/.config/turbomux/config.yaml
```

## Skills

The `/turbomux` skill is available in `.claude/commands/turbomux.md`. It lets any Claude Code instance orchestrate agents via tmux.

## Development

- Single-file Go CLI: `main.go`
- Config: `turbomux.yaml` (copied to `~/.config/turbomux/config.yaml`)
- Build: `go build -o turbomux .`
- No tests yet — this is a thin tmux wrapper

## Global Install

To make the skill available in ALL projects (not just this repo):
```bash
cp .claude/commands/turbomux.md ~/.claude/commands/turbomux.md
```
