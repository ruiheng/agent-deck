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

func agyCommandHasConversationFlag(command string) bool {
	words, ok := shellwords.Split(command)
	if !ok {
		words = strings.Fields(command)
	}
	for _, w := range words {
		if w == "--conversation" || strings.HasPrefix(w, "--conversation=") {
			return true
		}
	}
	return false
}

func (i *Instance) agyOptionFlags(opts *AgyOptions) string {
	if opts == nil {
		return ""
	}
	var b strings.Builder
	if m := strings.TrimSpace(opts.Model); m != "" {
		b.WriteString(" --model ")
		b.WriteString(shellescape.Quote(m))
	}
	if e := strings.TrimSpace(opts.Effort); e != "" {
		b.WriteString(" --effort ")
		b.WriteString(shellescape.Quote(e))
	}
	if mode := strings.TrimSpace(opts.Mode); mode != "" {
		b.WriteString(" --mode ")
		b.WriteString(shellescape.Quote(mode))
	}
	if opts.DangerousMode {
		b.WriteString(" --dangerously-skip-permissions")
	}
	return b.String()
}

// buildAgyCommand builds the launch command for Antigravity CLI (`agy`).
func (i *Instance) buildAgyCommand(baseCommand string) string {
	return i.buildAgyCommandWithMessage(baseCommand, "")
}

// buildAgyCommandWithMessage builds the launch command for Antigravity CLI
// (`agy`), optionally appending `-i <message>` for initial interactive prompt.
// Resume strictly requires an exact bound conversation ID (`--conversation <id>`);
// `agy` never falls back to workspace/mtime lookup or `-c`/`--continue`.
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

	var cmd strings.Builder
	if passthrough {
		cmd.WriteString(trimmed)
	} else {
		cmd.WriteString(GetAgyCommand())
		opts := i.GetAgyOptions()
		if opts == nil {
			userConfig, _ := LoadUserConfig()
			opts = NewAgyOptions(userConfig)
		}
		cmd.WriteString(i.agyOptionFlags(opts))
	}

	if !agyCommandHasConversationFlag(trimmed) {
		if sessionID := i.EffectiveAgySessionID(); sessionID != "" {
			cmd.WriteString(" --conversation ")
			cmd.WriteString(shellescape.Quote(sessionID))
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
