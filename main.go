package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const usage = `turbomux — tmux agent orchestrator

Usage:
  turbomux list                          List all tmux panes with status
  turbomux peek <pane>                   Show last N lines of a pane (default 30)
  turbomux peek <pane> <lines>           Show last N lines of a pane
  turbomux history <pane>                Dump entire scrollback buffer
  turbomux send <pane> <message...>      Send input to a pane
  conductor status                        Check all agent panes (idle/working)
  turbomux spawn <name> <dir> [prompt]   Create a pane, cd to dir, launch cc, optionally send prompt
  turbomux window <name> <count>         Create a named window with N panes
  turbomux kill <pane>                   Kill a pane
  turbomux json                          Output all pane status as JSON

Pane targeting:
  Use tmux target syntax: "session:window.pane" or just "window.pane" or window name.
  Examples: "0:1.0", "agents.0", "agents.1"
`

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

func tmuxRun(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

func isIdle(target string) bool {
	out, _ := tmux("capture-pane", "-t", target, "-p", "-S", "-5")
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Claude Code shows ❯ when idle at prompt
		if strings.Contains(line, "❯") && !strings.Contains(line, "tokens") {
			return true
		}
		// If we see activity indicators, it's working
		if strings.Contains(line, "⏺") || strings.Contains(line, "✻") ||
			strings.Contains(line, "◼") || strings.Contains(line, "⎿") {
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

	// -S - means start of history, -E means end
	out, err := tmux("capture-pane", "-t", target, "-p", "-S", "-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", out)
		os.Exit(1)
	}
	fmt.Println(out)
}

func cmdSend(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux send <pane> <message...>")
		os.Exit(1)
	}
	target := args[0]
	message := strings.Join(args[1:], " ")

	// Check if pane is idle before sending
	if !isIdle(target) {
		fmt.Fprintf(os.Stderr, "warning: pane %s appears to be busy (not at ❯ prompt)\n", target)
		fmt.Fprintf(os.Stderr, "send anyway? [y/N] ")
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" {
			fmt.Println("aborted")
			return
		}
	}

	_, err := tmux("send-keys", "-t", target, message, "Enter")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error sending to %s\n", target)
		os.Exit(1)
	}
	fmt.Printf("sent to %s: %s\n", target, message)
}

func cmdStatus() {
	panes := listPanes()
	agents := 0
	idle := 0
	working := 0

	for _, p := range panes {
		// Only show claude-related panes
		if p.Command != "2.1.85" && !strings.Contains(p.Title, "claude") &&
			!strings.Contains(p.Title, "Claude") && !strings.Contains(p.Title, "agent") &&
			!strings.Contains(p.Title, "Implement") && !strings.Contains(p.Title, "Build") {
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
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux spawn <name> <dir> [prompt]")
		os.Exit(1)
	}
	name := args[0]
	dir := args[1]

	// Create a new pane in the current window by splitting
	_, err := tmux("split-window", "-h", "-c", dir)
	if err != nil {
		// Try creating a new window instead
		_, err = tmux("new-window", "-n", name, "-c", dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating pane: %v\n", err)
			os.Exit(1)
		}
	}

	// Get the newly created pane target
	target, _ := tmux("display-message", "-p", "#{session_name}:#{window_index}.#{pane_index}")

	// Launch claude
	tmux("send-keys", "-t", target, "cc", "Enter")
	fmt.Printf("spawned agent in %s (dir: %s)\n", target, dir)

	// If a prompt was provided, wait a moment then send it
	if len(args) > 2 {
		prompt := strings.Join(args[2:], " ")
		fmt.Printf("prompt queued — will need to be sent after cc starts: turbomux send %s \"%s\"\n", target, prompt)
	}
}

func cmdWindow(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: turbomux window <name> <count>")
		os.Exit(1)
	}
	name := args[0]
	count := 1
	fmt.Sscanf(args[1], "%d", &count)

	// Create the window
	_, err := tmux("new-window", "-n", name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating window: %v\n", err)
		os.Exit(1)
	}

	// Split into the requested number of panes
	for i := 1; i < count; i++ {
		if i%2 == 1 {
			tmux("split-window", "-t", name, "-h")
		} else {
			tmux("split-window", "-t", name, "-v")
		}
	}

	// Even out the layout
	tmux("select-layout", "-t", name, "tiled")

	fmt.Printf("created window '%s' with %d panes\n", name, count)

	// List the new panes
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
		// Try as window
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

func cmdJSON() {
	panes := listPanes()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(panes)
}
