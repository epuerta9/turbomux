package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const usage = `turbomux — tmux agent orchestrator

Usage:
  turbomux list                              List all tmux panes with status
  turbomux peek <pane> [lines]               Show last N lines (default 30)
  turbomux history <pane>                    Dump entire scrollback buffer
  turbomux send [-f] <pane> <message...>     Send input to a pane (-f to skip idle check)
  turbomux status                            Check all agent panes (idle/working)
  turbomux spawn <name> <dir> [prompt]       Create pane, cd to dir, launch agent, send prompt
  turbomux spawn --agent=codex <name> <dir>  Use a specific agent (overrides config)
  turbomux window <name> <count>             Create named window with N panes
  turbomux kill <pane>                       Kill a pane or window
  turbomux keys <pane> <key...>              Send special keys (Enter, Escape, Up, Down, Tab, C-c, etc.)
  turbomux config                            Show current configuration
  turbomux json                              Output all pane status as JSON

Agents:
  The default agent is set in config (~/.config/turbomux/config.yaml).
  Override per-spawn with --agent=<name>. Built-in agents:
    claude-yolo  claude --dangerously-skip-permissions (default)
    claude       claude (with permission prompts)
    codex        codex
    pi           pi
    aider        aider
  Or use any custom command: --agent="my-custom-agent --flag"

Pane targeting:
  Use tmux target syntax: "session:window.pane", "window.pane", or window name.
  Examples: "0:1.0", "agents.0", "agents.1"

Config:
  Place config at ~/.config/turbomux/config.yaml or ./turbomux.yaml
`

type Config struct {
	Agent         string `yaml:"agent"`
	Session       string `yaml:"session"`
	DefaultWindow string `yaml:"default_window"`
	Layout        string `yaml:"layout"`
}

var defaultConfig = Config{
	Agent:         "claude-yolo",
	Session:       "0",
	DefaultWindow: "agents",
	Layout:        "tiled",
}

func loadConfig() Config {
	cfg := defaultConfig

	// Try local first, then global
	paths := []string{
		"turbomux.yaml",
		filepath.Join(os.Getenv("HOME"), ".config", "turbomux", "config.yaml"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		yaml.Unmarshal(data, &cfg)
		break
	}

	return cfg
}

func resolveAgent(name string) string {
	switch name {
	case "claude-yolo":
		return "claude --dangerously-skip-permissions"
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	case "pi":
		return "pi"
	case "aider":
		return "aider"
	default:
		return name // custom command
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "list":
		cmdList()
	case "peek":
		cmdPeek(args)
	case "history":
		cmdHistory(args)
	case "send":
		cmdSend(args)
	case "status":
		cmdStatus()
	case "spawn":
		cmdSpawn(args)
	case "window":
		cmdWindow(args)
	case "kill":
		cmdKill(args)
	case "keys":
		cmdKeys(args)
	case "config":
		cmdConfig()
	case "json":
		cmdJSON()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

type PaneInfo struct {
	Target  string `json:"target"`
	Title   string `json:"title"`
	Command string `json:"command"`
	Width   string `json:"width"`
	Height  string `json:"height"`
	Idle    bool   `json:"idle"`
}

func listPanes() []PaneInfo {
	out, err := tmux("list-panes", "-a", "-F",
		"#{session_name}:#{window_index}.#{pane_index}\t#{pane_title}\t#{pane_current_command}\t#{pane_width}\t#{pane_height}")
	if err != nil {
		return nil
	}

	var panes []PaneInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		p := PaneInfo{
			Target:  parts[0],
			Title:   parts[1],
			Command: parts[2],
			Width:   parts[3],
			Height:  parts[4],
		}
		p.Idle = isIdle(p.Target)
		panes = append(panes, p)
	}
	return panes
}

// isAgentLoaded checks if a coding agent (not just a shell) is running in the pane
func isAgentLoaded(target string) bool {
	out, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-30")
	// Look for Claude Code's banner or status bar indicators
	return strings.Contains(out, "Claude Code") ||
		strings.Contains(out, "bypass permissions") ||
		strings.Contains(out, "shift+tab to cycle") ||
		strings.Contains(out, "esc to interrupt") ||
		strings.Contains(out, "⏺") ||
		strings.Contains(out, "✻") ||
		strings.Contains(out, "codex>") ||
		strings.Contains(out, "aider>")
}

func isIdle(target string) bool {
	out, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-10")
	lines := strings.Split(out, "\n")

	// First check: is there an interactive prompt blocking? (trust dialog, selector)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Yes, I trust") ||
			strings.Contains(trimmed, "Enter to confirm") ||
			strings.Contains(trimmed, "Select") {
			return false // blocked on interactive prompt, not idle
		}
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Claude Code idle prompt — must also verify agent is actually loaded
		// (zsh also shows ❯ via starship, so we need to disambiguate)
		if strings.Contains(line, "❯") && !strings.Contains(line, "tokens") {
			// Check the surrounding context for Claude Code indicators
			if strings.Contains(out, "bypass permissions") ||
				strings.Contains(out, "shift+tab") ||
				strings.Contains(out, "esc to interrupt") {
				return true
			}
			// If no Claude indicators, this is likely a zsh prompt — not idle
			return false
		}
		// Codex idle prompt
		if strings.Contains(line, "codex>") {
			return true
		}
		// Active work indicators
		if strings.Contains(line, "⏺") || strings.Contains(line, "✻") ||
			strings.Contains(line, "◼") || strings.Contains(line, "⎿") ||
			strings.Contains(line, "Thinking") {
			return false
		}
		break
	}
	return false
}

func cmdList() {
	panes := listPanes()
	if len(panes) == 0 {
		fmt.Println("No tmux panes found")
		return
	}

	fmt.Printf("%-15s %-50s %-12s %s\n", "PANE", "TITLE", "CMD", "STATUS")
	fmt.Println(strings.Repeat("─", 90))
	for _, p := range panes {
		status := "working"
		if p.Idle {
			status = "idle ❯"
		}
		title := p.Title
		if len(title) > 48 {
			title = title[:48] + "…"
		}
		fmt.Printf("%-15s %-50s %-12s %s\n", p.Target, title, p.Command, status)
	}
}

func cmdPeek(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: turbomux peek <pane> [lines]")
		os.Exit(1)
	}
	target := args[0]
	lines := "30"
	if len(args) > 1 {
		lines = args[1]
	}

	out, err := tmux("capture-pane", "-t", target, "-p", "-S", "-"+lines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdHistory(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: turbomux history <pane>")
		os.Exit(1)
	}
	target := args[0]
	out, err := tmux("capture-pane", "-t", target, "-p", "-S", "-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdSend(args []string) {
	force := false
	filtered := []string{}
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux send [-f] <pane> <message...>")
		os.Exit(1)
	}
	target := args[0]
	message := strings.Join(args[1:], " ")

	if !force && !isIdle(target) {
		fmt.Fprintf(os.Stderr, "warning: pane %s appears to be busy (not at prompt)\n", target)
		fmt.Fprintf(os.Stderr, "send anyway? [y/N] ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("aborted")
			return
		}
	}

	// Use -l for literal text to avoid shell interpretation of special chars (? * etc.)
	_, err := tmux("send-keys", "-t", target, "-l", message)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error sending to %s\n", target)
		os.Exit(1)
	}
	tmux("send-keys", "-t", target, "Enter")
	fmt.Printf("sent to %s: %s\n", target, message)
}

func cmdStatus() {
	panes := listPanes()
	agents := 0
	idle := 0
	working := 0

	for _, p := range panes {
		// Heuristic: detect agent panes by command or title
		isAgent := false
		agentCmds := []string{"claude", "codex", "pi", "aider", "2.1"}
		for _, ac := range agentCmds {
			if strings.Contains(strings.ToLower(p.Command), ac) {
				isAgent = true
				break
			}
		}
		agentTitles := []string{"claude", "Claude", "agent", "Agent", "Implement", "Build", "Fix", "Create", "Add", "Update", "Refactor"}
		for _, at := range agentTitles {
			if strings.Contains(p.Title, at) {
				isAgent = true
				break
			}
		}
		if !isAgent {
			continue
		}

		agents++
		status := "🔨 working"
		if p.Idle {
			status = "⏸  idle"
			idle++
		} else {
			working++
		}
		fmt.Printf("%s  %-15s  %s\n", status, p.Target, p.Title)
	}

	if agents == 0 {
		fmt.Println("No agent panes found")
		return
	}
	fmt.Printf("\n%d agents: %d working, %d idle\n", agents, working, idle)
}

func cmdSpawn(args []string) {
	cfg := loadConfig()
	agentOverride := ""

	// Parse --agent flag
	filtered := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "--agent=") {
			agentOverride = strings.TrimPrefix(a, "--agent=")
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux spawn [--agent=<name>] <name> <dir> [prompt]")
		os.Exit(1)
	}
	name := args[0]
	dir := args[1]

	// Expand ~ in dir
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(os.Getenv("HOME"), dir[2:])
	}

	// Resolve which agent to use
	agent := cfg.Agent
	if agentOverride != "" {
		agent = agentOverride
	}
	agentCmd := resolveAgent(agent)

	// Create a new pane by splitting, or a new window if split fails
	_, err := tmux("split-window", "-h", "-c", dir)
	if err != nil {
		_, err = tmux("new-window", "-n", name, "-c", dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating pane: %v\n", err)
			os.Exit(1)
		}
	}

	// Get the newly created pane
	target, _ := tmux("display-message", "-p", "#{session_name}:#{window_index}.#{pane_index}")

	// Launch the agent
	tmux("send-keys", "-t", target, agentCmd, "Enter")
	fmt.Printf("spawned %s in %s (dir: %s, agent: %s)\n", name, target, dir, agent)

	// Wait for agent to be ready, handling interactive prompts along the way
	if len(args) > 2 {
		prompt := strings.Join(args[2:], " ")
		fmt.Printf("waiting for agent to load...")

		// Phase 1: Wait for agent to actually load (not just zsh)
		agentLoaded := false
		for i := 0; i < 45; i++ {
			time.Sleep(2 * time.Second)

			// Check for and handle interactive prompts (trust dialog, etc.)
			paneOut, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-20")
			handled := handleInteractivePrompts(target, paneOut)
			if handled {
				fmt.Print("(handled prompt)")
				time.Sleep(1 * time.Second) // give agent time to process
				continue
			}

			if isAgentLoaded(target) {
				agentLoaded = true
				fmt.Print(" loaded!")
				break
			}
			fmt.Print(".")
		}

		if !agentLoaded {
			fmt.Println(" timeout waiting for agent to load")
			fmt.Printf("  send prompt manually: turbomux send %s \"...\"\n", target)
			return
		}

		// Phase 2: Wait for agent's idle prompt
		fmt.Print(" waiting for prompt...")
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)

			// Handle any late interactive prompts
			paneOut, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-20")
			handled := handleInteractivePrompts(target, paneOut)
			if handled {
				fmt.Print("(handled prompt)")
				time.Sleep(1 * time.Second)
				continue
			}

			if isIdle(target) {
				fmt.Println(" ready!")
				// Use send-keys -l for literal text (avoids shell glob interpretation of ? * etc.)
				tmux("send-keys", "-t", target, "-l", prompt)
				tmux("send-keys", "-t", target, "Enter")
				fmt.Printf("sent prompt to %s\n", target)
				return
			}
			fmt.Print(".")
		}
		fmt.Println(" timeout — send prompt manually:")
		fmt.Printf("  turbomux send %s \"%s\"\n", target, prompt)
	}
}

func cmdWindow(args []string) {
	cfg := loadConfig()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux window <name> <count>")
		os.Exit(1)
	}
	name := args[0]
	count := 1
	fmt.Sscanf(args[1], "%d", &count)

	_, err := tmux("new-window", "-n", name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating window: %v\n", err)
		os.Exit(1)
	}

	for i := 1; i < count; i++ {
		if i%2 == 1 {
			tmux("split-window", "-t", name, "-h")
		} else {
			tmux("split-window", "-t", name, "-v")
		}
	}

	layout := cfg.Layout
	tmux("select-layout", "-t", name, layout)

	fmt.Printf("created window '%s' with %d panes (layout: %s)\n", name, count, layout)
	out, _ := tmux("list-panes", "-t", name, "-F", "  #{session_name}:#{window_index}.#{pane_index}")
	fmt.Println(out)
}

func cmdKill(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: turbomux kill <pane>")
		os.Exit(1)
	}
	target := args[0]
	_, err := tmux("kill-pane", "-t", target)
	if err != nil {
		_, err = tmux("kill-window", "-t", target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error killing %s\n", target)
			os.Exit(1)
		}
		fmt.Printf("killed window %s\n", target)
		return
	}
	fmt.Printf("killed pane %s\n", target)
}

func cmdKeys(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: turbomux keys <pane> <key...>

Special keys: Enter, Escape, Up, Down, Left, Right, Tab, Space, BSpace
Ctrl combos:  C-c, C-d, C-l, C-a, C-e, C-k
Examples:
  turbomux keys 0:1.0 Enter              # press enter
  turbomux keys 0:1.0 Down Down Enter    # navigate menu down twice, select
  turbomux keys 0:1.0 C-c                # ctrl+c to interrupt
  turbomux keys 0:1.0 y Enter            # type 'y' then enter (confirm prompt)`)
		os.Exit(1)
	}
	target := args[0]
	keys := args[1:]

	for _, k := range keys {
		_, err := tmux("send-keys", "-t", target, k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error sending key %q to %s\n", k, target)
			os.Exit(1)
		}
	}
	fmt.Printf("sent keys to %s: %s\n", target, strings.Join(keys, " "))
}

// handleInteractivePrompts detects and auto-responds to common agent startup prompts.
// Scans the FULL pane output (not just last line) to catch dialogs anywhere on screen.
// Returns true if it handled something.
func handleInteractivePrompts(target, paneOutput string) bool {
	lower := strings.ToLower(paneOutput)

	// Claude Code trust dialog: "Yes, I trust this folder" with "Enter to confirm"
	// This is a selection menu — just press Enter to confirm the default (option 1 = trust)
	if strings.Contains(lower, "yes, i trust") && strings.Contains(lower, "enter to confirm") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	// Claude Code trust dialog alternative wording
	if strings.Contains(lower, "trust") && strings.Contains(lower, "folder") && strings.Contains(lower, "enter to confirm") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	// Generic "Do you trust this project directory?" → type "yes"
	if strings.Contains(lower, "do you trust") {
		tmux("send-keys", "-t", target, "-l", "yes")
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	// Account selector: numbered list with "select"
	if strings.Contains(lower, "select") && (strings.Contains(lower, "account") || strings.Contains(lower, "profile")) {
		tmux("send-keys", "-t", target, "Enter") // select default/first
		return true
	}

	// Yes/No confirmation prompts
	if strings.Contains(lower, "(y/n)") || strings.Contains(lower, "[y/n]") {
		tmux("send-keys", "-t", target, "-l", "y")
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	// "Press enter to continue" style prompts
	if strings.Contains(lower, "press enter") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	return false
}

func cmdConfig() {
	cfg := loadConfig()
	fmt.Printf("agent:          %s → %s\n", cfg.Agent, resolveAgent(cfg.Agent))
	fmt.Printf("session:        %s\n", cfg.Session)
	fmt.Printf("default_window: %s\n", cfg.DefaultWindow)
	fmt.Printf("layout:         %s\n", cfg.Layout)
}

func cmdJSON() {
	panes := listPanes()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(panes)
}
