package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"al.essio.dev/pkg/shellescape"

	"github.com/asheshgoplani/agent-deck/internal/shellwords"
)

const defaultAgyCommand = "agy"

// GetAgyCommand returns the configured Antigravity CLI command, falling back
// to the default `agy` binary.
func GetAgyCommand() string {
	userConfig, _ := LoadUserConfig()
	if userConfig != nil && strings.TrimSpace(userConfig.Agy.Command) != "" {
		return strings.TrimSpace(userConfig.Agy.Command)
	}
	return defaultAgyCommand
}

// GetAgyConfigDir returns the Antigravity CLI configuration directory
// (hosting hooks.json). Respects AGY_CONFIG_DIR and defaults to
// ~/.gemini/config. Panics in tests if it resolves inside the real home.
func GetAgyConfigDir() string {
	return sandboxedToolDir("agy config", getAgyConfigDirUnguarded())
}

func getAgyConfigDirUnguarded() string {
	if envDir := strings.TrimSpace(os.Getenv("AGY_CONFIG_DIR")); envDir != "" {
		return ExpandPath(envDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".gemini", "config")
	}
	return filepath.Join(home, ".gemini", "config")
}

// GetAgyDataDir returns the Antigravity CLI data root (hosting brain/<cid>
// transcripts and artifacts). Respects AGY_DATA_DIR and defaults to
// ~/.gemini/antigravity-cli. Panics in tests if it resolves inside real home.
func GetAgyDataDir() string {
	return sandboxedToolDir("agy data", getAgyDataDirUnguarded())
}

func getAgyDataDirUnguarded() string {
	if envDir := strings.TrimSpace(os.Getenv("AGY_DATA_DIR")); envDir != "" {
		return ExpandPath(envDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// GetAgyHooksPath returns the path to Antigravity CLI's global hooks.json.
func GetAgyHooksPath() string {
	return filepath.Join(GetAgyConfigDir(), "hooks.json")
}

// GetAgyOptions returns Antigravity-specific options from ToolOptionsJSON, or nil if unset.
func (i *Instance) GetAgyOptions() *AgyOptions {
	if len(i.ToolOptionsJSON) == 0 {
		return nil
	}
	opts, err := UnmarshalAgyOptions(i.ToolOptionsJSON)
	if err != nil {
		return nil
	}
	return opts
}

// SetAgyOptions stores Antigravity-specific options into ToolOptionsJSON.
func (i *Instance) SetAgyOptions(opts *AgyOptions) error {
	if opts == nil {
		i.ToolOptionsJSON = nil
		return nil
	}
	data, err := MarshalToolOptions(opts)
	if err != nil {
		return err
	}
	i.ToolOptionsJSON = data
	return nil
}

// EffectiveAgySessionID returns the bound Antigravity conversation ID for this
// instance: AgySessionID takes precedence, falling back to legacy custom-tool
// GenericSessionID so existing state.db bindings migrate transparently.
func (i *Instance) EffectiveAgySessionID() string {
	if i == nil {
		return ""
	}
	if id := strings.TrimSpace(i.AgySessionID); id != "" {
		return id
	}
	return strings.TrimSpace(i.GenericSessionID)
}

// IsValidAgyConversationID reports whether id is a well-formed Antigravity
// conversation identifier safe to pass directly to `--conversation <id>`.
// Accepts 1..128 characters from `[a-zA-Z0-9._-]`.
func IsValidAgyConversationID(id string) bool {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || len(trimmed) > 128 {
		return false
	}
	for _, r := range trimmed {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlphaNum && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func agyCommandHasConversationFlag(command string) bool {
	words, ok := shellwords.Split(command)
	if !ok {
		words = strings.Fields(command)
	}
	for _, w := range words {
		if w == "--conversation" || strings.HasPrefix(w, "--conversation=") || w == "-c" || w == "--continue" {
			return true
		}
	}
	return false
}

func (i *Instance) agyOptionFlags(opts *AgyOptions) string {
	var b strings.Builder
	var model, effort, mode, agent string
	var dangerous, sandbox bool
	var extraArgs []string
	if opts != nil {
		model = strings.TrimSpace(opts.Model)
		effort = strings.TrimSpace(opts.Effort)
		mode = strings.TrimSpace(opts.Mode)
		agent = strings.TrimSpace(opts.Agent)
		dangerous = opts.DangerousMode
		sandbox = opts.Sandbox
		extraArgs = opts.ExtraArgs
	}
	if model == "" || effort == "" {
		if userConfig, err := LoadUserConfig(); err == nil && userConfig != nil {
			if model == "" {
				model = strings.TrimSpace(userConfig.Agy.DefaultModel)
			}
			if effort == "" {
				effort = strings.TrimSpace(userConfig.Agy.Effort)
			}
		}
	}
	if model != "" {
		b.WriteString(" --model ")
		b.WriteString(shellescape.Quote(model))
	}
	if effort != "" {
		b.WriteString(" --effort ")
		b.WriteString(shellescape.Quote(effort))
	}
	if mode != "" {
		b.WriteString(" --mode ")
		b.WriteString(shellescape.Quote(mode))
	}
	if agent != "" {
		b.WriteString(" --agent ")
		b.WriteString(shellescape.Quote(agent))
	}
	if dangerous {
		b.WriteString(" --dangerously-skip-permissions")
	}
	if sandbox {
		b.WriteString(" --sandbox")
	}
	if i != nil {
		if parentDir := strings.TrimSpace(i.ParentProjectPath); parentDir != "" && parentDir != i.ProjectPath {
			b.WriteString(" --add-dir ")
			b.WriteString(shellescape.Quote(parentDir))
		}
		for _, addPath := range i.AdditionalPaths {
			if trimmedPath := strings.TrimSpace(addPath); trimmedPath != "" {
				b.WriteString(" --add-dir ")
				b.WriteString(shellescape.Quote(trimmedPath))
			}
		}
	}
	for _, arg := range extraArgs {
		if trimmedArg := strings.TrimSpace(arg); trimmedArg != "" {
			b.WriteString(" ")
			b.WriteString(shellescape.Quote(trimmedArg))
		}
	}
	return b.String()
}

// buildAgyCommand builds the launch command for Antigravity CLI (`agy`).
func (i *Instance) buildAgyCommand(baseCommand string) string {
	return i.buildAgyCommandWithMessage(baseCommand, "")
}

// buildAgyCommandWithMessage builds the launch command for Antigravity CLI
// (`agy`), optionally appending `-i <message>` for initial interactive prompt.
// Automatic resume uses an exact bound conversation ID (`--conversation <id>`);
// `--continue` is emitted only when opts.SessionMode == "continue" and no ID is
// bound yet, switching to `--conversation <id>` once bound.
func (i *Instance) buildAgyCommandWithMessage(baseCommand, message string) string {
	if i.Tool != "agy" {
		return baseCommand
	}

	i.launchesFresh.Store(false)

	envPrefix := i.buildEnvSourceCommand()
	exportPrefix := fmt.Sprintf(
		"export AGENTDECK_INSTANCE_ID=%s; export AGENTDECK_PROFILE=%s; ",
		shellescape.Quote(i.ID),
		shellescape.Quote(sessionProfileEnvValue()),
	)

	trimmed := strings.TrimSpace(baseCommand)
	passthrough := trimmed != "" && trimmed != "agy"

	opts := i.GetAgyOptions()
	if opts == nil {
		userConfig, _ := LoadUserConfig()
		opts = NewAgyOptions(userConfig)
	}

	var cmd strings.Builder
	if passthrough {
		cmd.WriteString(trimmed)
	} else {
		cmd.WriteString(GetAgyCommand())
		cmd.WriteString(i.agyOptionFlags(opts))
	}

	if !agyCommandHasConversationFlag(trimmed) {
		if sessionID := i.EffectiveAgySessionID(); sessionID != "" {
			cmd.WriteString(" --conversation ")
			cmd.WriteString(shellescape.Quote(sessionID))
		} else if opts != nil && strings.TrimSpace(opts.SessionMode) == "continue" {
			cmd.WriteString(" --continue")
		} else {
			i.launchesFresh.Store(true)
		}
	}

	if msg := strings.TrimSpace(message); msg != "" {
		cmd.WriteString(" -i ")
		cmd.WriteString(shellescape.Quote(msg))
	}

	return envPrefix + exportPrefix + cmd.String()
}
