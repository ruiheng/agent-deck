package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func handleAgyHooks(args []string) {
	if len(args) == 0 {
		printAgyHooksUsage(os.Stderr)
		exitCLI(1)
	}

	// A help request anywhere in the argument list must print usage and exit
	// without side effects (#1993).
	if hooksHelpRequested(args) {
		printAgyHooksUsage(os.Stdout)
		return
	}

	jsonOut := slices.Contains(args[1:], "--json")

	switch args[0] {
	case "help", "--help", "-h":
		printAgyHooksUsage(os.Stdout)
	case "install":
		handleAgyHooksInstall(jsonOut)
	case "uninstall":
		handleAgyHooksUninstall(jsonOut)
	case "status":
		runAgyHooksStatus(hooksStatusWantsJSON(args[1:]))
	default:
		fmt.Fprintf(os.Stderr, "Unknown agy-hooks subcommand: %s\n", args[0])
		printAgyHooksUsage(os.Stderr)
		exitCLI(1)
	}
}

func printAgyHooksUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: agent-deck agy-hooks <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Manage Antigravity (agy) CLI hook integration.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  install      Install agent-deck Antigravity hooks")
	fmt.Fprintln(w, "  uninstall    Remove agent-deck Antigravity hooks")
	fmt.Fprintln(w, "  status       Show Antigravity hooks install status (--json for machine output)")
}

func handleAgyHooksInstall(jsonOut bool) {
	configDir := session.GetAgyConfigDir()
	configPath := filepath.Join(configDir, "hooks.json")
	installed, err := session.InjectAgyHooks(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error installing Antigravity hooks: %v\n", err)
		exitCLI(1)
	}
	// Clear the durable opt-out so TUI startup may auto-reinstall again.
	if err := session.SetAgyHooksEnabled(true); err != nil {
		fmt.Fprintf(os.Stderr, "Error clearing [antigravity] hooks_enabled opt-out in config.toml: %v\n", err)
		exitCLI(1)
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"installed": true,
			"changed":   installed,
			"config":    configPath,
		})
		return
	}
	if installed {
		fmt.Println("Antigravity hooks installed successfully.")
		fmt.Printf("Config: %s\n", configPath)
	} else {
		fmt.Println("Antigravity hooks are already installed.")
	}
}

func handleAgyHooksUninstall(jsonOut bool) {
	// Persist the opt-out FIRST so TUI startup cannot silently reinstall on next launch.
	if err := session.SetAgyHooksEnabled(false); err != nil {
		fmt.Fprintf(os.Stderr, "Error persisting opt-out to config.toml: %v\n", err)
		fmt.Fprintln(os.Stderr, "Hooks were NOT removed (TUI startup would reinstall them).")
		fmt.Fprintln(os.Stderr, "Set [antigravity] hooks_enabled = false manually, then rerun uninstall.")
		exitCLI(1)
	}
	configDir := session.GetAgyConfigDir()
	removed, err := session.RemoveAgyHooks(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error removing Antigravity hooks: %v\n", err)
		exitCLI(1)
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"installed": false,
			"removed":   removed,
			"config":    filepath.Join(configDir, "hooks.json"),
		})
		return
	}
	fmt.Println("Auto-install disabled ([antigravity] hooks_enabled = false in config.toml).")
	fmt.Println("Run 'agent-deck agy-hooks install' to re-enable.")
	if removed {
		fmt.Println("Antigravity hooks removed successfully.")
	} else {
		fmt.Println("No agent-deck Antigravity hooks found to remove.")
	}
}

func runAgyHooksStatus(asJSON bool) {
	configDir := session.GetAgyConfigDir()
	installed := session.CheckAgyHooksInstalled(configDir)
	configPath := filepath.Join(configDir, "hooks.json")

	cfg, err := session.LoadUserConfig()
	eff := cfg.EffectiveAntigravitySettings()
	if asJSON {
		st := hooksStatusJSON{Kind: "agy", State: simpleHooksState(installed), Config: configPath}
		switch {
		case err != nil:
			st.Details = append(st.Details, fmt.Sprintf("auto-install unknown: could not read config.toml: %v", err))
		case cfg != nil && !eff.GetHooksEnabled():
			st.Details = append(st.Details, "auto-install disabled ([antigravity] hooks_enabled = false in config.toml)")
		}
		printHooksStatusJSON(st)
		return
	}

	if installed {
		fmt.Println("Status: INSTALLED")
		fmt.Printf("Config: %s\n", configPath)
	} else {
		fmt.Println("Status: NOT INSTALLED")
		fmt.Println("Run 'agent-deck agy-hooks install' to install.")
	}
	switch {
	case err != nil:
		fmt.Printf("Auto-install: UNKNOWN (could not read config.toml: %v)\n", err)
	case cfg != nil && !eff.GetHooksEnabled():
		fmt.Println("Auto-install: DISABLED ([antigravity] hooks_enabled = false in config.toml)")
	}
}
