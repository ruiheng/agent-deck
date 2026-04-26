package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type config struct {
	Targets         [][]string `json:"targets"`
	ContinueOnError bool       `json:"continue_on_error,omitempty"`
}

func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex-notify-fanout.json"), nil
}

func loadConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("config missing targets")
	}
	for i, target := range cfg.Targets {
		if len(target) == 0 {
			return nil, fmt.Errorf("config target %d is empty", i)
		}
	}
	return &cfg, nil
}

func runTarget(argv []string, payloadArgs []string, stdinData []byte) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := exec.Command(argv[0], append(argv[1:], payloadArgs...)...)
	if len(stdinData) > 0 {
		cmd.Stdin = bytes.NewReader(stdinData)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("%s exited with code %d", argv[0], exitErr.ExitCode())
		}
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	return nil
}

func main() {
	var configPath string
	fs := flag.NewFlagSet("codex-notify-fanout", flag.ExitOnError)
	fs.StringVar(&configPath, "config", "", "Path to codex-notify-fanout JSON config")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "codex-notify-fanout: %v\n", err)
		os.Exit(2)
	}

	if strings.TrimSpace(configPath) == "" {
		var err error
		configPath, err = defaultConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "codex-notify-fanout: determine config path: %v\n", err)
			os.Exit(1)
		}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codex-notify-fanout: %v\n", err)
		os.Exit(1)
	}

	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codex-notify-fanout: read stdin: %v\n", err)
		os.Exit(1)
	}
	payloadArgs := fs.Args()

	var firstErr error
	for _, target := range cfg.Targets {
		err := runTarget(target, payloadArgs, stdinData)
		if err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
		if !cfg.ContinueOnError {
			fmt.Fprintf(os.Stderr, "codex-notify-fanout: target failed: %v\n", err)
			os.Exit(1)
		}
	}

	if firstErr != nil {
		fmt.Fprintf(os.Stderr, "codex-notify-fanout: target failed: %v\n", firstErr)
		os.Exit(1)
	}
}
