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

  turbomux init [dir]                        Init beads in a project (bd init)
  turbomux tickets                           Show beads tickets (bd list + bd ready)
  turbomux board                             Show beads board overview (bd status)
  turbomux assign <ticket> <pane>            Assign a beads ticket to a pane's agent
  turbomux ready                             Show unblocked tickets ready for work (bd ready)

Agents:
  The default agent is set in config (~/.config/turbomux/config.yaml).
  Override per-spawn with --agent=<name>. Built-in agents:
    claude-yolo  claude --dangerously-skip-permissions (default)
    claude       claude (with permission prompts)
    codex        codex
    pi           pi
    aider        aider
  Or use any custom command: --agent="my-custom-agent --flag"

Tracker:
  turbomux uses beads (bd) by default for local ticket tracking.
  All worktrees share one beads DB — agents see the same tickets.
  Set tracker: none in config to disable (use your own Linear/ClickUp/etc).

Pane targeting:
  Use tmux target syntax: "session:window.pane", "window.pane", or window name.
  Examples: "0:1.0", "agents.0", "agents.1"

Config:
  Place config at ~/.config/turbomux/config.yaml or ./turbomux.yaml
`

type Config struct {
	Agent         string `yaml:"agent"`
	Tracker       string `yaml:"tracker"`
	Session       string `yaml:"session"`
	DefaultWindow string `yaml:"default_window"`
	Layout        string `yaml:"layout"`
}

var defaultConfig = Config{
	Agent:         "claude-yolo",
	Tracker:       "beads",
	Session:       "0",
	DefaultWindow: "agents",
	Layout:        "tiled",
}

func loadConfig() Config {
	cfg := defaultConfig

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
		return name
	}
}

// hasBeads checks if bd is installed
func hasBeads() bool {
	_, err := exec.LookPath("bd")
	return err == nil
}

// isBeadsProject checks if dir (or parents) has .beads/
func isBeadsProject(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".beads")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// bd runs a beads command and returns output
func bd(dir string, args ...string) (string, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
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
	case "init":
		cmdInit(args)
	case "tickets":
		cmdTickets(args)
	case "board":
		cmdBoard(args)
	case "ready":
		cmdReady(args)
	case "assign":
		cmdAssign(args)
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

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Yes, I trust") ||
			strings.Contains(trimmed, "Enter to confirm") ||
			strings.Contains(trimmed, "Select") {
			return false
		}
	}

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.Contains(line, "❯") && !strings.Contains(line, "tokens") {
			if strings.Contains(out, "bypass permissions") ||
				strings.Contains(out, "shift+tab") ||
				strings.Contains(out, "esc to interrupt") {
				return true
			}
			return false
		}
		if strings.Contains(line, "codex>") {
			return true
		}
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

	// Use -l for literal text to avoid shell interpretation of special chars
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
		isAgent := false
		agentCmds := []string{"claude", "codex", "pi", "aider", "2.1"}
		for _, ac := range agentCmds {
			if strings.Contains(strings.ToLower(p.Command), ac) {
				isAgent = true
				break
			}
		}
		agentTitles := []string{"claude", "Claude", "agent", "Agent", "Implement", "Build", "Fix", "Create", "Add", "Update", "Refactor", "Research", "Evaluate"}
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

	// Show beads summary if available
	cfg := loadConfig()
	if cfg.Tracker == "beads" && hasBeads() {
		cwd, _ := os.Getwd()
		if isBeadsProject(cwd) {
			fmt.Println()
			out, err := bd(cwd, "ready", "--plain", "--limit", "5")
			if err == nil && out != "" {
				fmt.Println("📋 Ready tickets:")
				fmt.Println(out)
			}
			blocked, err := bd(cwd, "blocked", "--plain")
			if err == nil && blocked != "" {
				fmt.Println("🚫 Blocked:")
				fmt.Println(blocked)
			}
		}
	}
}

func cmdSpawn(args []string) {
	cfg := loadConfig()
	agentOverride := ""
	noTracker := false

	// Parse flags
	filtered := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "--agent=") {
			agentOverride = strings.TrimPrefix(a, "--agent=")
		} else if a == "--no-tracker" {
			noTracker = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux spawn [--agent=<name>] [--no-tracker] <name> <dir> [prompt]")
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

	// If beads tracker is enabled and the source dir is a beads project,
	// use bd worktree create for the spawn directory
	useBeads := cfg.Tracker == "beads" && !noTracker && hasBeads() && isBeadsProject(dir)
	worktreeDir := ""

	if useBeads {
		// Check if this is spawning into an existing worktree or needs a new one
		absDir, _ := filepath.Abs(dir)
		parentDir := filepath.Dir(absDir)
		worktreePath := filepath.Join(parentDir, name)

		// Only create worktree if the target doesn't already exist
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			fmt.Printf("creating beads worktree: %s\n", worktreePath)
			out, err := bd(dir, "worktree", "create", worktreePath, "--branch", name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bd worktree create failed: %s\n", out)
				fmt.Println("falling back to plain tmux pane")
			} else {
				worktreeDir = worktreePath
				dir = worktreeDir
				fmt.Printf("worktree created at %s (shared beads DB)\n", worktreeDir)
			}
		} else {
			// Worktree already exists, just use it
			dir = worktreePath
			fmt.Printf("using existing worktree: %s\n", dir)
		}
	}

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

	// Build the prompt — prepend beads context if available
	if len(args) > 2 {
		prompt := strings.Join(args[2:], " ")

		// If beads is active, prepend bd prime output for agent context
		if useBeads {
			prime, err := bd(dir, "prime")
			if err == nil && prime != "" {
				prompt = prime + "\n\n" + prompt
			}
		}

		fmt.Printf("waiting for agent to load...")

		// Phase 1: Wait for agent to actually load
		agentLoaded := false
		for i := 0; i < 45; i++ {
			time.Sleep(2 * time.Second)

			paneOut, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-20")
			handled := handleInteractivePrompts(target, paneOut)
			if handled {
				fmt.Print("(handled prompt)")
				time.Sleep(1 * time.Second)
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

			paneOut, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-20")
			handled := handleInteractivePrompts(target, paneOut)
			if handled {
				fmt.Print("(handled prompt)")
				time.Sleep(1 * time.Second)
				continue
			}

			if isIdle(target) {
				fmt.Println(" ready!")
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

// handleInteractivePrompts detects and auto-responds to common agent startup prompts.
func handleInteractivePrompts(target, paneOutput string) bool {
	lower := strings.ToLower(paneOutput)

	if strings.Contains(lower, "yes, i trust") && strings.Contains(lower, "enter to confirm") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	if strings.Contains(lower, "trust") && strings.Contains(lower, "folder") && strings.Contains(lower, "enter to confirm") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	if strings.Contains(lower, "do you trust") {
		tmux("send-keys", "-t", target, "-l", "yes")
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	if strings.Contains(lower, "select") && (strings.Contains(lower, "account") || strings.Contains(lower, "profile")) {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	if strings.Contains(lower, "(y/n)") || strings.Contains(lower, "[y/n]") {
		tmux("send-keys", "-t", target, "-l", "y")
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	if strings.Contains(lower, "press enter") {
		tmux("send-keys", "-t", target, "Enter")
		return true
	}

	return false
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
  turbomux keys 0:1.0 Enter
  turbomux keys 0:1.0 Down Down Enter
  turbomux keys 0:1.0 C-c`)
		os.Exit(1)
	}
	target := args[0]
	keys := args[1:]

	for _, k := range keys {
		tmux("send-keys", "-t", target, k)
	}
	fmt.Printf("sent keys to %s: %s\n", target, strings.Join(keys, " "))
}

func cmdConfig() {
	cfg := loadConfig()
	fmt.Printf("agent:          %s → %s\n", cfg.Agent, resolveAgent(cfg.Agent))
	fmt.Printf("tracker:        %s\n", cfg.Tracker)
	fmt.Printf("session:        %s\n", cfg.Session)
	fmt.Printf("default_window: %s\n", cfg.DefaultWindow)
	fmt.Printf("layout:         %s\n", cfg.Layout)

	if cfg.Tracker == "beads" {
		if hasBeads() {
			fmt.Println("beads:          installed ✓")
			cwd, _ := os.Getwd()
			if isBeadsProject(cwd) {
				fmt.Println("beads project:  yes (found .beads/)")
			} else {
				fmt.Println("beads project:  no (run turbomux init)")
			}
		} else {
			fmt.Println("beads:          not installed (go install github.com/steveyegge/beads/cmd/bd@latest)")
		}
	}
}

func cmdJSON() {
	panes := listPanes()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(panes)
}

// --- Beads integration ---

func cmdInit(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	if !hasBeads() {
		fmt.Fprintln(os.Stderr, "beads (bd) not installed. Install with:")
		fmt.Fprintln(os.Stderr, "  go install github.com/steveyegge/beads/cmd/bd@latest")
		os.Exit(1)
	}

	if isBeadsProject(dir) {
		fmt.Println("beads already initialized in this project")
		out, _ := bd(dir, "status")
		fmt.Println(out)
		return
	}

	out, err := bd(dir, "init")
	if err != nil {
		fmt.Fprintf(os.Stderr, "bd init failed: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
	fmt.Println("\nbeads initialized. Create tickets with:")
	fmt.Println("  bd create \"ticket title\" --type task --priority 1")
	fmt.Println("  bd dep add <child> <parent>")
	fmt.Println("  turbomux ready")
}

func cmdTickets(args []string) {
	if !hasBeads() {
		fmt.Fprintln(os.Stderr, "beads not installed")
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	if !isBeadsProject(cwd) {
		fmt.Fprintln(os.Stderr, "not a beads project (run turbomux init)")
		os.Exit(1)
	}

	// Show all open tickets
	out, err := bd(cwd, "list", "--pretty")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdBoard(args []string) {
	if !hasBeads() {
		fmt.Fprintln(os.Stderr, "beads not installed")
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	if !isBeadsProject(cwd) {
		fmt.Fprintln(os.Stderr, "not a beads project (run turbomux init)")
		os.Exit(1)
	}

	out, err := bd(cwd, "status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdReady(args []string) {
	if !hasBeads() {
		fmt.Fprintln(os.Stderr, "beads not installed")
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	if !isBeadsProject(cwd) {
		fmt.Fprintln(os.Stderr, "not a beads project (run turbomux init)")
		os.Exit(1)
	}

	out, err := bd(cwd, "ready", "--pretty")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	if out == "" {
		fmt.Println("No ready tickets (all blocked or assigned)")
	} else {
		fmt.Println(out)
	}
}

func cmdAssign(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux assign <ticket-id> <pane>")
		os.Exit(1)
	}

	ticketID := args[0]
	target := args[1]

	if !hasBeads() {
		fmt.Fprintln(os.Stderr, "beads not installed")
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	if !isBeadsProject(cwd) {
		fmt.Fprintln(os.Stderr, "not a beads project")
		os.Exit(1)
	}

	// Get ticket details
	details, err := bd(cwd, "show", ticketID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ticket not found: %s\n", ticketID)
		os.Exit(1)
	}

	// Claim the ticket
	_, err = bd(cwd, "update", ticketID, "--status", "in_progress")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to claim ticket: %s\n", ticketID)
	} else {
		fmt.Printf("claimed %s\n", ticketID)
	}

	// Send ticket details to the agent pane
	// Build a concise prompt with the ticket info
	prompt := fmt.Sprintf("You have been assigned ticket %s. Here are the details:\n\n%s\n\nStart working on this ticket. Use bd note %s to log progress. When done, run bd close %s.", ticketID, details, ticketID, ticketID)

	if !isIdle(target) {
		fmt.Fprintf(os.Stderr, "warning: pane %s is busy — queuing assignment\n", target)
	}

	tmux("send-keys", "-t", target, "-l", prompt)
	tmux("send-keys", "-t", target, "Enter")
	fmt.Printf("assigned %s to %s\n", ticketID, target)
}
