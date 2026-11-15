package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAgyOptionsPanel_ExposesControlsAndKeyboardNavigation(t *testing.T) {
	p := NewAgyOptionsPanel()
	view := p.View()
	for _, label := range []string{
		"Antigravity Options",
		"Session:",
		"Mode:",
		"YOLO mode (--dangerously-skip-permissions)",
		"Terminal sandbox (--sandbox)",
		"CLI agent:",
		"Extra args:",
		"Start prompt:",
	} {
		if !strings.Contains(view, label) {
			t.Errorf("Agy panel missing %q in view:\n%s", label, view)
		}
	}

	p.Focus()
	if !p.IsFocused() || !p.AtTop() {
		t.Fatalf("initial focus: IsFocused=%v AtTop=%v", p.IsFocused(), p.AtTop())
	}
	if got := p.FocusedLine(); got != 1 {
		t.Fatalf("FocusedLine() at Session = %d, want 1", got)
	}

	// Session cycles new -> continue -> resume.
	p.Update(tea.KeyMsg{Type: tea.KeyRight})
	if opts := p.GetOptions("", ""); opts.SessionMode != "continue" {
		t.Fatalf("after 1 right: SessionMode = %q, want continue", opts.SessionMode)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRight})
	if opts := p.GetOptions("", ""); opts.SessionMode != "resume" {
		t.Fatalf("after 2 right: SessionMode = %q, want resume", opts.SessionMode)
	}

	// Resume exposes ID input right below Session (focusIndex 1).
	if !strings.Contains(p.View(), "ID:") {
		t.Fatalf("expected resume ID input to appear when sessionMode=resume")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := p.FocusedLine(); got != 2 {
		t.Fatalf("FocusedLine() at resume ID = %d, want 2", got)
	}
	// Typing 'y' and ' ' inside ID must go into the text box, NOT toggle YOLO/radio.
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	p.Update(tea.KeyMsg{Type: tea.KeySpace})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if opts := p.GetOptions("", ""); opts.ResumeConversationID != "y 1" || opts.DangerousMode {
		t.Fatalf("resume ID typing triggered toggle or lost chars: opts=%+v", opts)
	}

	// Move down to Mode (focusIndex 2 -> line 3) and cycle default -> plan -> accept-edits -> auto.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.FocusedLine(); got != 3 {
		t.Fatalf("FocusedLine() at Mode = %d, want 3", got)
	}
	wantModes := []string{"plan", "accept-edits", "auto", ""}
	for _, want := range wantModes {
		p.Update(tea.KeyMsg{Type: tea.KeyRight})
		if got := p.GetOptions("", "").Mode; got != want {
			t.Fatalf("Mode after right = %q, want %q", got, want)
		}
	}

	// Move down to YOLO (line 4) and toggle with 'y'.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.FocusedLine(); got != 4 {
		t.Fatalf("FocusedLine() at YOLO = %d, want 4", got)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !p.GetYoloMode() {
		t.Fatal("'y' on YOLO row should toggle DangerousMode=true")
	}

	// Move down to Terminal sandbox (line 5) and toggle with Space.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.FocusedLine(); got != 5 {
		t.Fatalf("FocusedLine() at Sandbox = %d, want 5", got)
	}
	p.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !p.GetOptions("", "").Sandbox {
		t.Fatal("Space on Sandbox row should toggle Sandbox=true")
	}

	// Move to CLI agent (line 6) and type "my agent" — 'y' and ' ' must insert characters.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.FocusedLine(); got != 6 {
		t.Fatalf("FocusedLine() at CLI agent = %d, want 6", got)
	}
	for _, r := range "my" {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !p.GetYoloMode() {
		t.Fatal("typing 'y' inside CLI agent input must not toggle YOLO")
	}
	if got := p.GetOptions("", "").Agent; got != "my" {
		t.Fatalf("Agent = %q, want %q", got, "my")
	}

	// Move to Extra args (line 7) and enter flags with quoted token.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := p.FocusedLine(); got != 7 {
		t.Fatalf("FocusedLine() at Extra args = %d, want 7", got)
	}
	for _, r := range `--remote-control --project "two words"` {
		if r == ' ' {
			p.Update(tea.KeyMsg{Type: tea.KeySpace})
		} else {
			p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	args := p.GetExtraArgs()
	if len(args) != 3 || args[0] != "--remote-control" || args[1] != "--project" || args[2] != "two words" {
		t.Fatalf("GetExtraArgs() = %#v", args)
	}

	// Move to Start prompt (line 8) — should be AtBottom and preserve spaces.
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !p.AtBottom() {
		t.Fatal("Start prompt row should be AtBottom()")
	}
	if got := p.FocusedLine(); got != 8 {
		t.Fatalf("FocusedLine() at Start prompt = %d, want 8", got)
	}
	for _, r := range "check y" {
		if r == ' ' {
			p.Update(tea.KeyMsg{Type: tea.KeySpace})
		} else {
			p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	if got := p.GetStartPrompt(); got != "check y" {
		t.Fatalf("GetStartPrompt() = %q, want %q", got, "check y")
	}

	// Shift+Tab from bottom returns smoothly through FocusLast / Blur.
	p.Blur()
	p.FocusLast()
	if !p.AtBottom() {
		t.Fatal("FocusLast should position cursor at bottom")
	}
}

func TestNewDialog_AgyOptionsWiringAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	session.ClearUserConfigCache()
	t.Cleanup(session.ClearUserConfigCache)

	cfgDir := filepath.Join(home, ".agent-deck")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgTOML := `
[agy]
default_model = "gemini-2.5-pro"
effort = "high"
mode = "plan"
dangerous_mode = true
sandbox = true
agent = "architect"
extra_args = ["--remote-control"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfgTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	session.ClearUserConfigCache()

	d := NewNewDialog()
	d.SetDefaultTool("agy")
	d.SetSize(100, 50)
	d.Show()

	if d.toolOptions != d.agyOptions {
		t.Fatal("selecting agy did not expose AgyOptionsPanel")
	}
	if d.GetSelectedCommand() != "agy" {
		t.Fatalf("GetSelectedCommand() = %q, want agy", d.GetSelectedCommand())
	}
	if !d.GetAgyYoloMode() {
		t.Fatal("expected [agy].dangerous_mode=true to prefill YOLO mode")
	}
	if hint := d.modelInputHint(); hint == "" {
		t.Fatal("expected non-empty modelInputHint for agy")
	}

	opts := d.GetAgyOptions()
	if opts == nil {
		t.Fatal("GetAgyOptions() returned nil")
	}
	if opts.Mode != "plan" || !opts.DangerousMode || !opts.Sandbox || opts.Agent != "architect" {
		t.Fatalf("config defaults did not populate AgyOptions: %+v", opts)
	}
	if len(opts.ExtraArgs) != 1 || opts.ExtraArgs[0] != "--remote-control" {
		t.Fatalf("ExtraArgs = %v, want [--remote-control]", opts.ExtraArgs)
	}

	// Press 'y' while focusCommand is active -> toggles agy YOLO.
	d.focusIndex = d.indexOf(focusCommand)
	d.updateFocus()
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if d.GetAgyYoloMode() {
		t.Fatal("pressing 'y' on focusCommand should toggle agy YOLO off")
	}
	// Command hint advertises 'y yolo'.
	if view := stripAnsi(d.View()); !strings.Contains(view, "y yolo") {
		t.Fatalf("focusCommand footer should advertise 'y yolo' for agy:\n%s", view)
	}

	// Capture ReasoningEffort and ModelID into GetAgyOptions().
	d.modelInput.SetValue("claude-sonnet-4-5")
	d.reasoningEffort = "xhigh"
	captured := d.GetAgyOptions()
	if captured.Model != "claude-sonnet-4-5" || captured.Effort != "xhigh" {
		t.Fatalf("GetAgyOptions() did not capture model/effort: %+v", captured)
	}

	// Resume ID validation in NewDialog.Validate().
	d.agyOptions.SetFromOptions(&session.AgyOptions{SessionMode: "resume", ResumeConversationID: ""})
	if errMsg := d.Validate(); !strings.Contains(errMsg, "Conversation ID required") {
		t.Fatalf("empty resume ID Validate() = %q, want Conversation ID required", errMsg)
	}
	d.agyOptions.SetFromOptions(&session.AgyOptions{SessionMode: "resume", ResumeConversationID: "bad id; rm"})
	if errMsg := d.Validate(); !strings.Contains(errMsg, "Invalid conversation ID") {
		t.Fatalf("bad resume ID Validate() = %q, want Invalid conversation ID", errMsg)
	}
	d.agyOptions.SetFromOptions(&session.AgyOptions{SessionMode: "resume", ResumeConversationID: "aafbb74d-9089-4066-9102-f9df37ff2809"})
	if errMsg := d.Validate(); errMsg != "" {
		t.Fatalf("valid resume ID Validate() = %q, want empty", errMsg)
	}

	// Start prompt resets across ShowInGroup openings.
	d.agyOptions.SetStartPrompt("fix failing tests")
	if got := d.GetAgyStartPrompt(); got != "fix failing tests" {
		t.Fatalf("GetAgyStartPrompt() = %q", got)
	}
	d.Show()
	if got := d.GetAgyStartPrompt(); got != "" {
		t.Fatalf("Show() must reset Start prompt across openings, got %q", got)
	}
}
