package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	deckdocker "github.com/asheshgoplani/agent-deck/internal/docker"
)

const (
	openCodePhase0PluginFileName = "agentdeck-phase0-proof.js"
	openCodePhase0ReportFileName = "plugin-events.jsonl"
)

type openCodePluginPhase0Options struct {
	projectPath  string
	prompt       string
	timeout      time.Duration
	mode         string
	model        string
	agent        string
	sandboxImage string
	reportFile   string
	jsonOutput   bool
}

type openCodePluginPhase0Report struct {
	Task        string                    `json:"task"`
	GeneratedAt time.Time                 `json:"generated_at"`
	ProjectPath string                    `json:"project_path"`
	Prompt      string                    `json:"prompt"`
	Mode        string                    `json:"mode"`
	OverallPass bool                      `json:"overall_pass"`
	Runs        []openCodePluginPhase0Run `json:"runs"`
}

type openCodePluginPhase0Run struct {
	Mode                 string                        `json:"mode"`
	InstanceID           string                        `json:"instance_id"`
	Pass                 bool                          `json:"pass"`
	FailureReasons       []string                      `json:"failure_reasons,omitempty"`
	Command              []string                      `json:"command"`
	ExitCode             int                           `json:"exit_code"`
	TimedOut             bool                          `json:"timed_out"`
	PluginInitObserved   bool                          `json:"plugin_init_observed"`
	InstanceIDVisible    bool                          `json:"instance_id_visible"`
	SessionEventObserved bool                          `json:"session_event_observed"`
	ObservedEvents       []string                      `json:"observed_events,omitempty"`
	ObservedSessionID    string                        `json:"observed_session_id,omitempty"`
	ObservedSessionField string                        `json:"observed_session_field,omitempty"`
	HookJSONPath         string                        `json:"hook_json_path"`
	HookSIDPath          string                        `json:"hook_sid_path"`
	HookJSONPresent      bool                          `json:"hook_json_present"`
	HookSIDPresent       bool                          `json:"hook_sid_present"`
	HookStatus           *openCodePluginHookStatusFile `json:"hook_status,omitempty"`
	Records              []openCodePluginPhase0Record  `json:"records,omitempty"`
	StdoutTail           string                        `json:"stdout_tail,omitempty"`
	StderrTail           string                        `json:"stderr_tail,omitempty"`
	WorkDir              string                        `json:"work_dir,omitempty"`
	TempDir              string                        `json:"temp_dir,omitempty"`
}

type openCodePluginPhase0Record struct {
	Kind         string          `json:"kind"`
	Mode         string          `json:"mode,omitempty"`
	InstanceID   string          `json:"instance_id,omitempty"`
	EventType    string          `json:"event_type,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	SessionField string          `json:"session_field,omitempty"`
	HooksDir     string          `json:"hooks_dir,omitempty"`
	ReportPath   string          `json:"report_path,omitempty"`
	Error        string          `json:"error,omitempty"`
	HookPath     string          `json:"hook_path,omitempty"`
	SIDPath      string          `json:"sid_path,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

type openCodePluginHookStatusFile struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Event     string `json:"event"`
	Timestamp int64  `json:"ts"`
}

type openCodePluginPhase0PreparedEnv struct {
	tempRoot      string
	tempConfigDir string
	tempDataDir   string
	tempCacheDir  string
	tempStateDir  string
}

func handleOpenCodePlugin(args []string) {
	if len(args) == 0 {
		printOpenCodePluginUsage(os.Stderr)
		os.Exit(1)
	}

	switch args[0] {
	case "help", "--help", "-h":
		printOpenCodePluginUsage(os.Stdout)
	case "phase0-proof":
		handleOpenCodePluginPhase0(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown opencode-plugin subcommand: %s\n", args[0])
		printOpenCodePluginUsage(os.Stderr)
		os.Exit(1)
	}
}

func printOpenCodePluginUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: agent-deck opencode-plugin <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "OpenCode plugin proof tooling.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  phase0-proof   Run the Phase 0 proof spike for OpenCode session binding")
}

func handleOpenCodePluginPhase0(args []string) {
	opts, err := parseOpenCodePluginPhase0Options(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	report, runErr := runOpenCodePluginPhase0Proof(opts)
	if writeErr := writeOpenCodePluginPhase0Report(opts.reportFile, report); writeErr != nil {
		fmt.Fprintf(os.Stderr, "Error writing report file: %v\n", writeErr)
		os.Exit(1)
	}

	if opts.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printOpenCodePluginPhase0Summary(os.Stdout, report)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "Phase 0 proof failed: %v\n", runErr)
		os.Exit(1)
	}
}

func parseOpenCodePluginPhase0Options(args []string) (*openCodePluginPhase0Options, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	fs := flag.NewFlagSet("phase0-proof", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := &openCodePluginPhase0Options{}
	fs.StringVar(&opts.projectPath, "project", cwd, "Project directory to run the proof in")
	fs.StringVar(&opts.prompt, "prompt", "Reply with the single word proof.", "Prompt to send to OpenCode during the proof run")
	fs.DurationVar(&opts.timeout, "timeout", 45*time.Second, "Per-mode timeout")
	fs.StringVar(&opts.mode, "mode", "both", "Proof mode: host, sandbox, or both")
	fs.StringVar(&opts.model, "model", "", "Optional OpenCode model override")
	fs.StringVar(&opts.agent, "agent", "", "Optional OpenCode agent override")
	fs.StringVar(&opts.sandboxImage, "sandbox-image", deckdocker.DefaultImage(), "Sandbox image for sandbox mode")
	fs.StringVar(&opts.reportFile, "report-file", "", "Optional path to save the full proof report JSON")
	fs.BoolVar(&opts.jsonOutput, "json", false, "Print the proof report as JSON")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	opts.projectPath = strings.TrimSpace(opts.projectPath)
	opts.mode = strings.ToLower(strings.TrimSpace(opts.mode))
	if opts.projectPath == "" {
		return nil, errors.New("project path is required")
	}
	if !filepath.IsAbs(opts.projectPath) {
		opts.projectPath, err = filepath.Abs(opts.projectPath)
		if err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(opts.projectPath)
	if err != nil {
		return nil, fmt.Errorf("stat project path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path is not a directory: %s", opts.projectPath)
	}
	switch opts.mode {
	case "host", "sandbox", "both":
	default:
		return nil, fmt.Errorf("unsupported mode %q (expected host, sandbox, or both)", opts.mode)
	}
	if opts.timeout <= 0 {
		return nil, errors.New("timeout must be greater than zero")
	}
	return opts, nil
}

func runOpenCodePluginPhase0Proof(opts *openCodePluginPhase0Options) (*openCodePluginPhase0Report, error) {
	prepared, err := prepareOpenCodePluginPhase0Env()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(prepared.tempRoot)

	report := &openCodePluginPhase0Report{
		Task:        "opencode-phase0-proof",
		GeneratedAt: time.Now(),
		ProjectPath: opts.projectPath,
		Prompt:      opts.prompt,
		Mode:        opts.mode,
	}

	modes := selectedOpenCodePluginPhase0Modes(opts.mode)
	var runErrs []string
	for _, mode := range modes {
		run, err := runOpenCodePluginPhase0Mode(prepared, opts, mode)
		report.Runs = append(report.Runs, run)
		if err != nil {
			runErrs = append(runErrs, fmt.Sprintf("%s: %v", mode, err))
		}
	}

	report.OverallPass = len(runErrs) == 0
	if len(runErrs) > 0 {
		return report, errors.New(strings.Join(runErrs, "; "))
	}
	return report, nil
}

func selectedOpenCodePluginPhase0Modes(mode string) []string {
	switch mode {
	case "host":
		return []string{"host"}
	case "sandbox":
		return []string{"sandbox"}
	default:
		return []string{"host", "sandbox"}
	}
}

func prepareOpenCodePluginPhase0Env() (*openCodePluginPhase0PreparedEnv, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	tempRoot, err := os.MkdirTemp("", "agentdeck-opencode-phase0-*")
	if err != nil {
		return nil, err
	}

	tempConfigDir := filepath.Join(tempRoot, ".config", "opencode")
	if err := os.MkdirAll(tempConfigDir, 0o755); err != nil {
		return nil, err
	}

	realConfigDir := filepath.Join(homeDir, ".config", "opencode")
	if err := copyDirTree(realConfigDir, tempConfigDir, map[string]bool{"plugins": true}); err != nil {
		return nil, err
	}

	pluginDir := filepath.Join(tempConfigDir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return nil, err
	}
	pluginPath := filepath.Join(pluginDir, openCodePhase0PluginFileName)
	if err := os.WriteFile(pluginPath, []byte(renderOpenCodePluginPhase0Plugin()), 0o644); err != nil {
		return nil, err
	}

	realDataDir := filepath.Join(homeDir, ".local", "share", "opencode")
	tempDataDir := filepath.Join(tempRoot, ".local", "share", "opencode")
	if err := os.MkdirAll(tempDataDir, 0o755); err != nil {
		return nil, err
	}
	if err := copyDirTree(realDataDir, tempDataDir, nil); err != nil {
		return nil, err
	}

	realCacheDir := filepath.Join(homeDir, ".cache", "opencode")
	tempCacheDir := filepath.Join(tempRoot, ".cache", "opencode")
	if err := os.MkdirAll(tempCacheDir, 0o755); err != nil {
		return nil, err
	}
	if err := copyDirTree(realCacheDir, tempCacheDir, nil); err != nil {
		return nil, err
	}
	tempStateDir := filepath.Join(tempRoot, ".local", "state", "opencode")
	if err := os.MkdirAll(tempStateDir, 0o755); err != nil {
		return nil, err
	}

	return &openCodePluginPhase0PreparedEnv{
		tempRoot:      tempRoot,
		tempConfigDir: tempConfigDir,
		tempDataDir:   tempDataDir,
		tempCacheDir:  tempCacheDir,
		tempStateDir:  tempStateDir,
	}, nil
}

func runOpenCodePluginPhase0Mode(prepared *openCodePluginPhase0PreparedEnv, opts *openCodePluginPhase0Options, mode string) (openCodePluginPhase0Run, error) {
	instanceID := fmt.Sprintf("opencode-phase0-%s-%d", mode, time.Now().UnixNano())
	runDir := filepath.Join(prepared.tempRoot, mode)
	reportDir := filepath.Join(runDir, "report")
	hooksDir := filepath.Join(runDir, "hooks")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return openCodePluginPhase0Run{}, err
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return openCodePluginPhase0Run{}, err
	}

	reportPath := filepath.Join(reportDir, openCodePhase0ReportFileName)
	hookJSONPath := filepath.Join(hooksDir, instanceID+".json")
	hookSIDPath := filepath.Join(hooksDir, instanceID+".sid")
	commandArgs := buildOpenCodePluginPhase0CommandArgs(opts)

	var cmd *exec.Cmd
	switch mode {
	case "host":
		cmd = buildOpenCodePluginPhase0HostCommand(prepared, opts, instanceID, reportPath, hooksDir, commandArgs)
	case "sandbox":
		cmd = buildOpenCodePluginPhase0SandboxCommand(prepared, opts, instanceID, reportDir, hooksDir, commandArgs)
	default:
		return openCodePluginPhase0Run{}, fmt.Errorf("unsupported mode %q", mode)
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	cmd = exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Dir = opts.projectPath
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = filterEnv(cmd.Env, "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "AGENTDECK_INSTANCE_ID", "AGENTDECK_HOOKS_DIR", "AGENTDECK_PROOF_REPORT_PATH", "AGENTDECK_PROOF_MODE", "OPENCODE_DISABLE_AUTOUPDATE", "OPENCODE_DISABLE_MODELS_FETCH", "OPENCODE_DISABLE_LSP_DOWNLOAD")

	switch mode {
	case "host":
		cmd.Env = append(cmd.Env, buildOpenCodePluginPhase0HostEnv(prepared, instanceID, reportPath, hooksDir)...)
	case "sandbox":
		cmd.Env = append(cmd.Env, buildOpenCodePluginPhase0SandboxEnv()...)
	}

	err := cmd.Run()
	exitCode := exitCodeFromError(err)
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)

	run := openCodePluginPhase0Run{
		Mode:         mode,
		InstanceID:   instanceID,
		Command:      append([]string{}, cmd.Args...),
		ExitCode:     exitCode,
		TimedOut:     timedOut,
		HookJSONPath: hookJSONPath,
		HookSIDPath:  hookSIDPath,
		StdoutTail:   tailString(stdoutBuf.String(), 2000),
		StderrTail:   tailString(stderrBuf.String(), 2000),
		WorkDir:      opts.projectPath,
		TempDir:      runDir,
	}

	if hookStatus, readErr := readOpenCodePluginPhase0HookStatus(hookJSONPath); readErr == nil {
		run.HookStatus = hookStatus
		run.HookJSONPresent = true
	}
	if sidData, sidErr := os.ReadFile(hookSIDPath); sidErr == nil && strings.TrimSpace(string(sidData)) != "" {
		run.HookSIDPresent = true
	}

	records, _ := readOpenCodePluginPhase0Records(reportPath)
	run.Records = records
	summarizeOpenCodePluginPhase0Run(&run)

	if len(run.FailureReasons) > 0 {
		return run, errors.New(strings.Join(run.FailureReasons, "; "))
	}
	return run, nil
}

func buildOpenCodePluginPhase0CommandArgs(opts *openCodePluginPhase0Options) []string {
	args := []string{"opencode", "run", "--format", "json"}
	if opts.model != "" {
		args = append(args, "--model", opts.model)
	}
	if opts.agent != "" {
		args = append(args, "--agent", opts.agent)
	}
	args = append(args, opts.prompt)
	return args
}

func buildOpenCodePluginPhase0HostCommand(prepared *openCodePluginPhase0PreparedEnv, opts *openCodePluginPhase0Options, instanceID, reportPath, hooksDir string, commandArgs []string) *exec.Cmd {
	cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
	cmd.Args = commandArgs
	cmd.Path = commandArgs[0]
	return cmd
}

func buildOpenCodePluginPhase0SandboxCommand(prepared *openCodePluginPhase0PreparedEnv, opts *openCodePluginPhase0Options, instanceID, reportDir, hooksDir string, commandArgs []string) *exec.Cmd {
	containerReportDir := "/tmp/agentdeck-phase0"
	containerHooksDir := "/root/.agent-deck/hooks"

	args := []string{
		"docker", "run", "--rm",
		"--workdir", "/workspace",
		"-e", "HOME=/root",
		"-e", "XDG_CONFIG_HOME=/root/.config",
		"-e", "XDG_DATA_HOME=/root/.local/share",
		"-e", "XDG_CACHE_HOME=/root/.cache",
		"-e", "XDG_STATE_HOME=/root/.local/state",
		"-e", "OPENCODE_DISABLE_AUTOUPDATE=1",
		"-e", "OPENCODE_DISABLE_MODELS_FETCH=1",
		"-e", "OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		"-e", "AGENTDECK_INSTANCE_ID=" + instanceID,
		"-e", "AGENTDECK_PROOF_MODE=sandbox",
		"-e", "AGENTDECK_HOOKS_DIR=" + containerHooksDir,
		"-e", "AGENTDECK_PROOF_REPORT_PATH=" + filepath.ToSlash(filepath.Join(containerReportDir, openCodePhase0ReportFileName)),
		"-v", opts.projectPath + ":/workspace",
		"-v", prepared.tempConfigDir + ":/root/.config/opencode",
		"-v", prepared.tempDataDir + ":/root/.local/share/opencode",
		"-v", prepared.tempCacheDir + ":/root/.cache/opencode",
		"-v", prepared.tempStateDir + ":/root/.local/state/opencode",
		"-v", hooksDir + ":" + containerHooksDir,
		"-v", reportDir + ":" + containerReportDir,
	}
	if uidgid := currentUserUIDGID(); uidgid != "" {
		args = append(args, "--user", uidgid)
	}
	args = append(args, opts.sandboxImage)
	args = append(args, commandArgs...)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Args = args
	cmd.Path = args[0]
	return cmd
}

func buildOpenCodePluginPhase0HostEnv(prepared *openCodePluginPhase0PreparedEnv, instanceID, reportPath, hooksDir string) []string {
	tempHome := filepath.Dir(filepath.Dir(prepared.tempConfigDir))
	return []string{
		"HOME=" + tempHome,
		"XDG_CONFIG_HOME=" + filepath.Join(tempHome, ".config"),
		"XDG_DATA_HOME=" + filepath.Dir(prepared.tempDataDir),
		"XDG_CACHE_HOME=" + filepath.Dir(prepared.tempCacheDir),
		"XDG_STATE_HOME=" + filepath.Dir(prepared.tempStateDir),
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_MODELS_FETCH=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
		"AGENTDECK_INSTANCE_ID=" + instanceID,
		"AGENTDECK_PROOF_MODE=host",
		"AGENTDECK_HOOKS_DIR=" + hooksDir,
		"AGENTDECK_PROOF_REPORT_PATH=" + reportPath,
	}
}

func buildOpenCodePluginPhase0SandboxEnv() []string {
	return nil
}

func summarizeOpenCodePluginPhase0Run(run *openCodePluginPhase0Run) {
	eventSet := make(map[string]bool)
	for _, record := range run.Records {
		switch record.Kind {
		case "plugin_init":
			run.PluginInitObserved = true
			if record.InstanceID == run.InstanceID {
				run.InstanceIDVisible = true
			}
		case "event":
			if record.InstanceID == run.InstanceID {
				run.InstanceIDVisible = true
			}
			if record.EventType != "" {
				eventSet[record.EventType] = true
				if record.EventType == "session.created" || record.EventType == "session.idle" {
					run.SessionEventObserved = true
				}
			}
			if record.SessionID != "" && run.ObservedSessionID == "" {
				run.ObservedSessionID = record.SessionID
				run.ObservedSessionField = record.SessionField
			}
		}
	}

	for event := range eventSet {
		run.ObservedEvents = append(run.ObservedEvents, event)
	}
	sort.Strings(run.ObservedEvents)

	var failures []string
	if !run.PluginInitObserved {
		failures = append(failures, "plugin did not initialize")
	}
	if !run.InstanceIDVisible {
		failures = append(failures, "plugin did not observe AGENTDECK_INSTANCE_ID")
	}
	if !run.SessionEventObserved {
		failures = append(failures, "plugin did not observe session.created or session.idle")
	}
	if run.ObservedSessionID == "" {
		failures = append(failures, "plugin did not extract a usable sessionID")
	}
	if !run.HookJSONPresent {
		failures = append(failures, "hook JSON was not written to the host-visible path")
	}
	if !run.HookSIDPresent {
		failures = append(failures, ".sid anchor was not written to the host-visible path")
	}
	if run.ExitCode != 0 && (!run.SessionEventObserved || run.ObservedSessionID == "" || !run.HookJSONPresent || !run.HookSIDPresent) {
		failures = append(failures, fmt.Sprintf("opencode exited with code %d", run.ExitCode))
	}
	if run.TimedOut {
		failures = append(failures, "command timed out before proof completed")
	}

	run.Pass = len(failures) == 0
	run.FailureReasons = failures
}

func readOpenCodePluginPhase0HookStatus(path string) (*openCodePluginHookStatusFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var status openCodePluginHookStatusFile
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func readOpenCodePluginPhase0Records(path string) ([]openCodePluginPhase0Record, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var records []openCodePluginPhase0Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record openCodePluginPhase0Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func writeOpenCodePluginPhase0Report(path string, report *openCodePluginPhase0Report) error {
	if strings.TrimSpace(path) == "" || report == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func printOpenCodePluginPhase0Summary(w io.Writer, report *openCodePluginPhase0Report) {
	fmt.Fprintf(w, "OpenCode Phase 0 Proof: %v\n", passLabel(report.OverallPass))
	fmt.Fprintf(w, "Project: %s\n", report.ProjectPath)
	fmt.Fprintf(w, "Prompt: %s\n", report.Prompt)
	for _, run := range report.Runs {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "[%s] %v\n", run.Mode, passLabel(run.Pass))
		fmt.Fprintf(w, "  instance: %s\n", run.InstanceID)
		fmt.Fprintf(w, "  exit: %d\n", run.ExitCode)
		if len(run.ObservedEvents) > 0 {
			fmt.Fprintf(w, "  events: %s\n", strings.Join(run.ObservedEvents, ", "))
		}
		if run.ObservedSessionID != "" {
			fmt.Fprintf(w, "  session_id: %s (%s)\n", run.ObservedSessionID, run.ObservedSessionField)
		}
		fmt.Fprintf(w, "  hook_json: %t  hook_sid: %t\n", run.HookJSONPresent, run.HookSIDPresent)
		if len(run.FailureReasons) > 0 {
			fmt.Fprintf(w, "  failures: %s\n", strings.Join(run.FailureReasons, "; "))
		}
		if run.StderrTail != "" {
			fmt.Fprintf(w, "  stderr tail: %s\n", strings.ReplaceAll(run.StderrTail, "\n", " | "))
		}
	}
}

func passLabel(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

func filterEnv(env []string, keys ...string) []string {
	if len(keys) == 0 {
		return append([]string{}, env...)
	}
	blocked := make(map[string]bool, len(keys))
	for _, key := range keys {
		blocked[key] = true
	}
	var out []string
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if blocked[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func tailString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[len(s)-max:])
}

func currentUserUIDGID() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	if u.Uid == "" || u.Gid == "" {
		return ""
	}
	return u.Uid + ":" + u.Gid
}

func copyDirTree(src, dst string, skipTopLevel map[string]bool) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 0 && skipTopLevel[parts[0]] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func renderOpenCodePluginPhase0Plugin() string {
	return `import { appendFile, mkdir, rename, writeFile } from "node:fs/promises";
import path from "node:path";

function clean(value) {
  return typeof value === "string" ? value.trim() : "";
}

function deepLookup(obj, segments) {
  let current = obj;
  for (const segment of segments) {
    if (!current || typeof current !== "object") {
      return "";
    }
    current = current[segment];
  }
  return clean(current);
}

function extractSession(event) {
  const candidates = [
    [["sessionID"], "sessionID"],
    [["session_id"], "session_id"],
    [["session", "id"], "session.id"],
    [["session", "sessionID"], "session.sessionID"],
    [["properties", "sessionID"], "properties.sessionID"],
    [["properties", "session_id"], "properties.session_id"],
  ];
  for (const [segments, field] of candidates) {
    const value = deepLookup(event, segments);
    if (value) {
      return { value, field };
    }
  }
  return { value: "", field: "" };
}

async function appendRecord(record) {
  const reportPath = clean(process.env.AGENTDECK_PROOF_REPORT_PATH);
  if (!reportPath) {
    return;
  }
  await mkdir(path.dirname(reportPath), { recursive: true });
  await appendFile(reportPath, JSON.stringify(record) + "\n");
}

async function writeHookFiles(instanceID, eventType, sessionID) {
  const hooksDir = clean(process.env.AGENTDECK_HOOKS_DIR);
  if (!hooksDir || !instanceID) {
    return;
  }
  const statusPath = path.join(hooksDir, instanceID + ".json");
  const sidPath = path.join(hooksDir, instanceID + ".sid");
  await appendRecord({
    kind: "hook_write_attempt",
    mode: clean(process.env.AGENTDECK_PROOF_MODE),
    instance_id: instanceID,
    event_type: eventType,
    session_id: sessionID,
    hook_path: statusPath,
    sid_path: sidPath,
  });
  try {
    await mkdir(hooksDir, { recursive: true });
    const payload = {
      status: "waiting",
      session_id: sessionID,
      event: eventType,
      ts: Math.floor(Date.now() / 1000),
    };
    const nonce = String(Date.now()) + "-" + Math.random().toString(16).slice(2);
    const tmpStatusPath = statusPath + "." + nonce + ".tmp";
    await writeFile(tmpStatusPath, JSON.stringify(payload));
    await rename(tmpStatusPath, statusPath);
    if (sessionID) {
      const tmpSIDPath = sidPath + "." + nonce + ".tmp";
      await writeFile(tmpSIDPath, sessionID);
      await rename(tmpSIDPath, sidPath);
    }
    await appendRecord({
      kind: "hook_write",
      mode: clean(process.env.AGENTDECK_PROOF_MODE),
      instance_id: instanceID,
      event_type: eventType,
      session_id: sessionID,
      hook_path: statusPath,
      sid_path: sidPath,
    });
  } catch (error) {
    await appendRecord({
      kind: "hook_write_error",
      mode: clean(process.env.AGENTDECK_PROOF_MODE),
      instance_id: instanceID,
      event_type: eventType,
      session_id: sessionID,
      hook_path: statusPath,
      sid_path: sidPath,
      error: error instanceof Error ? error.message : String(error),
    });
  }
}

export const AgentDeckPhase0Proof = async () => {
  const instanceID = clean(process.env.AGENTDECK_INSTANCE_ID);
  const mode = clean(process.env.AGENTDECK_PROOF_MODE);
  await appendRecord({
    kind: "plugin_init",
    mode,
    instance_id: instanceID,
    hooks_dir: clean(process.env.AGENTDECK_HOOKS_DIR),
    report_path: clean(process.env.AGENTDECK_PROOF_REPORT_PATH),
  });

  return {
    event: async ({ event }) => {
      const session = extractSession(event);
      const eventType = clean(event?.type);
      await appendRecord({
        kind: "event",
        mode,
        instance_id: instanceID,
        event_type: eventType,
        session_id: session.value,
        session_field: session.field,
        payload: event,
      });
      if (session.value) {
        await writeHookFiles(instanceID, eventType, session.value);
      }
    },
  };
};
`
}
