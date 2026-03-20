package main

import (
	"errors"
	"flag"
	"reflect"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *flag.FlagSet // create FlagSet with flags
		args     []string
		expected []string
	}{
		{
			name: "flags already before positional args",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{"--json", "my-title"},
			expected: []string{"--json", "my-title"},
		},
		{
			name: "bool flag after positional arg",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{"my-title", "--json"},
			expected: []string{"--json", "my-title"},
		},
		{
			name: "multiple bool flags after positional arg",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				fs.Bool("q", false, "")
				return fs
			},
			args:     []string{"my-title", "--json", "-q"},
			expected: []string{"--json", "-q", "my-title"},
		},
		{
			name: "string flag after positional arg",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.String("message", "", "")
				return fs
			},
			args:     []string{"my-title", "--message", "hello world"},
			expected: []string{"--message", "hello world", "my-title"},
		},
		{
			name: "flag with equals syntax",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.String("message", "", "")
				return fs
			},
			args:     []string{"my-title", "--message=hello"},
			expected: []string{"--message=hello", "my-title"},
		},
		{
			name: "mixed flags and positional args",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				fs.Bool("no-wait", false, "")
				return fs
			},
			args:     []string{"my-session", "hello message", "--json", "--no-wait"},
			expected: []string{"--json", "--no-wait", "my-session", "hello message"},
		},
		{
			name: "no flags at all",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{"my-title"},
			expected: []string{"my-title"},
		},
		{
			name: "empty args",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{},
			expected: nil,
		},
		{
			name: "double dash terminator",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{"--", "--json", "title"},
			expected: []string{"--json", "title"},
		},
		{
			name: "session show with title containing special chars",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("json", false, "")
				return fs
			},
			args:     []string{"Fix #147: Shift+R Restart Race", "--json"},
			expected: []string{"--json", "Fix #147: Shift+R Restart Race"},
		},
		{
			name: "short flag after positional",
			setup: func() *flag.FlagSet {
				fs := flag.NewFlagSet("test", flag.ContinueOnError)
				fs.Bool("q", false, "")
				return fs
			},
			args:     []string{"my-session", "-q"},
			expected: []string{"-q", "my-session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.setup()
			result := normalizeArgs(fs, tt.args)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("normalizeArgs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestNormalizeArgsIntegration verifies that after normalizeArgs + fs.Parse,
// flags are correctly parsed regardless of their position in args.
func TestNormalizeArgsIntegration(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		expectJSON       bool
		expectQuiet      bool
		expectIdentifier string
	}{
		{
			name:             "flags before identifier",
			args:             []string{"--json", "-q", "my-title"},
			expectJSON:       true,
			expectQuiet:      true,
			expectIdentifier: "my-title",
		},
		{
			name:             "flags after identifier",
			args:             []string{"my-title", "--json", "-q"},
			expectJSON:       true,
			expectQuiet:      true,
			expectIdentifier: "my-title",
		},
		{
			name:             "flags mixed around identifier",
			args:             []string{"--json", "my-title", "-q"},
			expectJSON:       true,
			expectQuiet:      true,
			expectIdentifier: "my-title",
		},
		{
			name:             "only identifier no flags",
			args:             []string{"my-title"},
			expectJSON:       false,
			expectQuiet:      false,
			expectIdentifier: "my-title",
		},
		{
			name:             "title with spaces and special chars",
			args:             []string{"Fix #147: Shift+R Restart Race", "--json"},
			expectJSON:       true,
			expectQuiet:      false,
			expectIdentifier: "Fix #147: Shift+R Restart Race",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			jsonOutput := fs.Bool("json", false, "Output as JSON")
			quiet := fs.Bool("q", false, "Quiet mode")

			normalized := normalizeArgs(fs, tt.args)
			if err := fs.Parse(normalized); err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			identifier := fs.Arg(0)

			if *jsonOutput != tt.expectJSON {
				t.Errorf("json = %v, want %v", *jsonOutput, tt.expectJSON)
			}
			if *quiet != tt.expectQuiet {
				t.Errorf("quiet = %v, want %v", *quiet, tt.expectQuiet)
			}
			if identifier != tt.expectIdentifier {
				t.Errorf("identifier = %q, want %q", identifier, tt.expectIdentifier)
			}
		})
	}
}

func TestReorderArgsForFlagParsing_CmdAndGroup(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "flags already before positional",
			args:     []string{"-c", "claude", "-g", "mygroup", "."},
			expected: []string{"-c", "claude", "-g", "mygroup", "."},
		},
		{
			name:     "path before flags gets moved to end",
			args:     []string{".", "-c", "claude", "-g", "mygroup"},
			expected: []string{"-c", "claude", "-g", "mygroup", "."},
		},
		{
			name:     "mixed flags with --no-parent",
			args:     []string{"-g", "mygroup", "-c", "claude", "--no-parent", "."},
			expected: []string{"-g", "mygroup", "-c", "claude", "--no-parent", "."},
		},
		{
			name:     "equals syntax for -c flag",
			args:     []string{"-c=claude", "-g", "work", "."},
			expected: []string{"-c=claude", "-g", "work", "."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reorderArgsForFlagParsing(tt.args)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("reorderArgsForFlagParsing(%v) = %v, want %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestResolveSessionCommand(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		explicitWrapper string
		wantTool        string
		wantWrapper     string
		wantNote        bool
		wantRawCommand  bool
	}{
		{
			name:           "plain tool uses tool command",
			raw:            "codex",
			wantTool:       "codex",
			wantWrapper:    "",
			wantNote:       false,
			wantRawCommand: false,
		},
		{
			name:           "tool with args auto-wrapper",
			raw:            "codex --dangerously-bypass-approvals-and-sandbox",
			wantTool:       "codex",
			wantWrapper:    "{command} --dangerously-bypass-approvals-and-sandbox",
			wantNote:       true,
			wantRawCommand: false,
		},
		{
			name:           "generic shell command kept raw",
			raw:            "bash -lc 'echo hi'",
			wantTool:       "shell",
			wantWrapper:    "",
			wantNote:       false,
			wantRawCommand: true,
		},
		{
			name:            "explicit wrapper wins",
			raw:             "codex --dangerously-bypass-approvals-and-sandbox",
			explicitWrapper: "{command} --my-wrapper-flag",
			wantTool:        "codex",
			wantWrapper:     "{command} --my-wrapper-flag",
			wantNote:        false,
			wantRawCommand:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, command, wrapper, note := resolveSessionCommand(tt.raw, tt.explicitWrapper)

			if tool != tt.wantTool {
				t.Fatalf("tool = %q, want %q", tool, tt.wantTool)
			}
			if wrapper != tt.wantWrapper {
				t.Fatalf("wrapper = %q, want %q", wrapper, tt.wantWrapper)
			}
			if (note != "") != tt.wantNote {
				t.Fatalf("note present = %v, want %v (note=%q)", note != "", tt.wantNote, note)
			}
			if command == "" {
				t.Fatal("command should not be empty")
			}
			if tt.wantRawCommand && command != tt.raw {
				t.Fatalf("command = %q, want raw %q", command, tt.raw)
			}
		})
	}
}

func TestResolveGroupSelection(t *testing.T) {
	tests := []struct {
		name                  string
		currentGroup          string
		parentGroup           string
		explicitGroupProvided bool
		want                  string
	}{
		{
			name:                  "explicit group wins over parent",
			currentGroup:          "ard",
			parentGroup:           "conductor",
			explicitGroupProvided: true,
			want:                  "ard",
		},
		{
			name:                  "inherit parent when no explicit group",
			currentGroup:          "",
			parentGroup:           "conductor",
			explicitGroupProvided: false,
			want:                  "conductor",
		},
		{
			name:                  "no explicit group and empty parent",
			currentGroup:          "",
			parentGroup:           "",
			explicitGroupProvided: false,
			want:                  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveGroupSelection(tt.currentGroup, tt.parentGroup, tt.explicitGroupProvided)
			if got != tt.want {
				t.Fatalf("resolveGroupSelection(%q, %q, %v) = %q, want %q",
					tt.currentGroup, tt.parentGroup, tt.explicitGroupProvided, got, tt.want)
			}
		})
	}
}

func TestParseTmuxEnvironmentValue(t *testing.T) {
	tests := []struct {
		name   string
		output string
		key    string
		want   string
	}{
		{
			name:   "matches requested key",
			output: "AGENTDECK_INSTANCE_ID=inst-123\n",
			key:    "AGENTDECK_INSTANCE_ID",
			want:   "inst-123",
		},
		{
			name:   "unset value returns empty",
			output: "-AGENTDECK_INSTANCE_ID\n",
			key:    "AGENTDECK_INSTANCE_ID",
			want:   "",
		},
		{
			name:   "different key returns empty",
			output: "OTHER_KEY=value\n",
			key:    "AGENTDECK_INSTANCE_ID",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTmuxEnvironmentValue(tt.output, tt.key); got != tt.want {
				t.Fatalf("parseTmuxEnvironmentValue(%q, %q) = %q, want %q", tt.output, tt.key, got, tt.want)
			}
		})
	}
}

func TestFindInstanceByTmuxSessionName(t *testing.T) {
	match := session.NewInstance("match", "/tmp")
	other := session.NewInstance("other", "/tmp")

	got := findInstanceByTmuxSessionName([]*session.Instance{other, match}, match.GetTmuxSession().Name)
	if got != match {
		t.Fatalf("findInstanceByTmuxSessionName() = %v, want match instance", got)
	}
}

func TestGetCurrentSessionIDPrefersTmuxEnvironment(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")

	origEnv := getCurrentTmuxEnvironmentFn
	origName := getCurrentTmuxSessionNameFn
	origFind := findInstanceDataByTmuxFastFn
	t.Cleanup(func() {
		getCurrentTmuxEnvironmentFn = origEnv
		getCurrentTmuxSessionNameFn = origName
		findInstanceDataByTmuxFastFn = origFind
	})

	getCurrentTmuxEnvironmentFn = func(key string) string {
		if key != "AGENTDECK_INSTANCE_ID" {
			t.Fatalf("unexpected key lookup: %s", key)
		}
		return "inst-env"
	}
	getCurrentTmuxSessionNameFn = func() (string, error) {
		t.Fatal("tmux session name lookup should not run when env is present")
		return "", nil
	}
	findInstanceDataByTmuxFastFn = func(tmuxSessionName, preferredProfile string) (*session.InstanceData, string) {
		t.Fatal("storage fallback should not run when env is present")
		return nil, ""
	}

	if got := GetCurrentSessionID(); got != "inst-env" {
		t.Fatalf("GetCurrentSessionID() = %q, want %q", got, "inst-env")
	}
}

func TestGetCurrentSessionIDFallsBackToTmuxSessionLookup(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")

	origEnv := getCurrentTmuxEnvironmentFn
	origName := getCurrentTmuxSessionNameFn
	origFind := findInstanceDataByTmuxFastFn
	t.Cleanup(func() {
		getCurrentTmuxEnvironmentFn = origEnv
		getCurrentTmuxSessionNameFn = origName
		findInstanceDataByTmuxFastFn = origFind
	})

	getCurrentTmuxEnvironmentFn = func(key string) string { return "" }
	getCurrentTmuxSessionNameFn = func() (string, error) {
		return "agentdeck_child_deadbeef", nil
	}
	findInstanceDataByTmuxFastFn = func(tmuxSessionName, preferredProfile string) (*session.InstanceData, string) {
		if tmuxSessionName != "agentdeck_child_deadbeef" {
			t.Fatalf("unexpected tmux session name: %s", tmuxSessionName)
		}
		return &session.InstanceData{ID: "inst-storage"}, "_test"
	}

	if got := GetCurrentSessionID(); got != "inst-storage" {
		t.Fatalf("GetCurrentSessionID() = %q, want %q", got, "inst-storage")
	}
}

func TestResolveSessionOrCurrentFallsBackToExactTmuxName(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")

	origEnv := getCurrentTmuxEnvironmentFn
	origName := getCurrentTmuxSessionNameFn
	origFind := findInstanceDataByTmuxFastFn
	t.Cleanup(func() {
		getCurrentTmuxEnvironmentFn = origEnv
		getCurrentTmuxSessionNameFn = origName
		findInstanceDataByTmuxFastFn = origFind
	})

	inst := session.NewInstance("child", "/tmp")
	getCurrentTmuxEnvironmentFn = func(key string) string { return "" }
	getCurrentTmuxSessionNameFn = func() (string, error) {
		return inst.GetTmuxSession().Name, nil
	}
	findInstanceDataByTmuxFastFn = func(tmuxSessionName, preferredProfile string) (*session.InstanceData, string) {
		t.Fatal("loaded instance match should win before storage fallback")
		return nil, ""
	}

	got, msg, code := ResolveSessionOrCurrent("", []*session.Instance{inst})
	if got != inst {
		t.Fatalf("ResolveSessionOrCurrent() = %v, want %v (msg=%q code=%q)", got, inst, msg, code)
	}
}

func TestResolveSessionOrCurrentReturnsNotFoundWithoutTmuxSession(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")

	origEnv := getCurrentTmuxEnvironmentFn
	origName := getCurrentTmuxSessionNameFn
	origFind := findInstanceDataByTmuxFastFn
	t.Cleanup(func() {
		getCurrentTmuxEnvironmentFn = origEnv
		getCurrentTmuxSessionNameFn = origName
		findInstanceDataByTmuxFastFn = origFind
	})

	getCurrentTmuxEnvironmentFn = func(key string) string { return "" }
	getCurrentTmuxSessionNameFn = func() (string, error) {
		return "", errors.New("no client")
	}
	findInstanceDataByTmuxFastFn = func(tmuxSessionName, preferredProfile string) (*session.InstanceData, string) {
		return nil, ""
	}

	got, msg, code := ResolveSessionOrCurrent("", nil)
	if got != nil {
		t.Fatalf("ResolveSessionOrCurrent() = %v, want nil", got)
	}
	if code != ErrCodeNotFound {
		t.Fatalf("ResolveSessionOrCurrent() code = %q, want %q", code, ErrCodeNotFound)
	}
	if msg == "" {
		t.Fatal("ResolveSessionOrCurrent() should return an explanatory message")
	}
}
