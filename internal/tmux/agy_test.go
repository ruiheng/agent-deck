package tmux

import (
	"slices"
	"testing"
)

func TestDetectToolFromCommand_Agy(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"bare agy", "agy", "agy"},
		{"agy uppercase", "AGY", "agy"},
		{"agy full path", "/usr/local/bin/agy --conversation abc-123", "agy"},
		{"agy with exports", "export AGENTDECK_INSTANCE_ID=id1; agy --model gemini-2.5-pro", "agy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectToolFromCommand(tt.command); got != tt.want {
				t.Fatalf("detectToolFromCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestDetectToolFromCommand_Agy_Negative(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"substring autophagy", "autophagy"},
		{"username directory", "/Users/nagy/bin/zsh"},
		{"argument only", "echo agy --help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectToolFromCommand(tt.command); got == "agy" {
				t.Fatalf("detectToolFromCommand(%q) = %q, must not match agy", tt.command, got)
			}
		})
	}
}

func TestDefaultRawPatterns_Agy(t *testing.T) {
	raw := DefaultRawPatterns("agy")
	if raw == nil {
		t.Fatal("DefaultRawPatterns(\"agy\") = nil, want agy patterns")
	}
	if _, err := CompilePatterns(raw); err != nil {
		t.Fatalf("CompilePatterns(agy): %v", err)
	}
	for _, ch := range []string{"⣾", "⡿", "⠋"} {
		if !slices.Contains(raw.SpinnerChars, ch) {
			t.Errorf("agy SpinnerChars missing %q: %v", ch, raw.SpinnerChars)
		}
	}
}

func TestClassifyPaneFrame_Agy_RealCaptures(t *testing.T) {
	const busySpinnerFrame = `
      ▄▀▀▄        Antigravity CLI 1.5.10
     ▀▀▀▀▀▀       user@example.com
    ▀▀▀▀▀▀▀▀      Orchestrator (Carbon)
   ▄▀▀    ▀▀▄     ~/work/project
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────
○ Thought for 5m 5s (checking route evidence calculation)
● Ran (rg -n "required_checkpoint_keys") (ctrl+o to expand)
⡿  Running command...
└ Tip: Use /feedback to share your experience with the team.
────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────
esc to cancel                                  auto · Orchestrator (Carbon)
`

	const busyFooterOnlyFrame = `
────────────────────────────────────────────────────────────
● Explored 3 files
└ Tip: Use /feedback to share your experience with the team.
────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────
esc to cancel                                  auto · Orchestrator (Carbon)
`

	const waitingFrame = `
────────────────────────────────────────────────────────────
▸ Thought for 3s, 200 tokens

  All checks passed and the session is ready for your next prompt.

────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────
? for shortcuts                                auto · Orchestrator (Carbon)
`

	const quotedProseAboveIdleComposerFrame = `
────────────────────────────────────────────────────────────
▸ Thought for 2s, 110 tokens

  Previously the worker pane displayed:
⡿  Running command...
esc to cancel                                  auto · Orchestrator (Carbon)

────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────
? for shortcuts                                auto · Orchestrator (Carbon)
`

	d := NewPromptDetector("agy")

	for _, tc := range []struct {
		name       string
		pane       string
		wantFrame  FrameVerdict
		wantPrompt bool
	}{
		{"busy spinner + esc to cancel footer", busySpinnerFrame, FrameActive, false},
		{"busy footer between tool calls", busyFooterOnlyFrame, FrameActive, false},
		{"idle waiting ? for shortcuts footer", waitingFrame, FrameWaiting, true},
		{"quoted prose in reply above idle composer", quotedProseAboveIdleComposerFrame, FrameWaiting, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyPaneFrame("agy", tc.pane); got != tc.wantFrame {
				t.Errorf("ClassifyPaneFrame(%q) = %s, want %s", tc.name, got, tc.wantFrame)
			}
			if got := d.HasPrompt(tc.pane); got != tc.wantPrompt {
				t.Errorf("HasPrompt(%q) = %v, want %v", tc.name, got, tc.wantPrompt)
			}
		})
	}
}
