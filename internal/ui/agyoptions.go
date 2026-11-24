package ui

import (
	"slices"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/shellwords"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AgyOptionsPanel exposes Antigravity CLI (`agy`) launch controls in NewDialog.
type AgyOptionsPanel struct {
	sessionMode      int // 0=new, 1=continue, 2=resume
	resumeIDInput    textinput.Model
	modeCursor       int // 0="", 1="plan", 2="accept-edits", 3="auto"
	dangerousMode    bool
	sandbox          bool
	agentInput       textinput.Model
	extraArgsInput   textinput.Model
	startPromptInput textinput.Model
	focusIndex       int // -1 when blurred, 0..N-1 when focused
}

var agySessionModes = []string{"new", "continue", "resume"}

var agyExecutionModes = []struct {
	value string
	label string
}{
	{value: "", label: "Default"},
	{value: "plan", label: "Plan"},
	{value: "accept-edits", label: "Accept edits"},
	{value: "auto", label: "Auto"},
}

// NewAgyOptionsPanel creates an Antigravity CLI options panel for NewDialog.
func NewAgyOptionsPanel() *AgyOptionsPanel {
	resumeInput := textinput.New()
	resumeInput.Placeholder = "conversation_id..."
	resumeInput.CharLimit = 128
	resumeInput.Width = 34

	agentInput := textinput.New()
	agentInput.Placeholder = "default (agy agents)"
	agentInput.CharLimit = 128
	agentInput.Width = 36

	extraArgsInput := textinput.New()
	extraArgsInput.Placeholder = "e.g. --remote-control"
	extraArgsInput.CharLimit = 512
	extraArgsInput.Width = 36

	startPromptInput := textinput.New()
	startPromptInput.Placeholder = "initial prompt (--prompt-interactive)"
	startPromptInput.CharLimit = 1024
	startPromptInput.Width = 36

	return &AgyOptionsPanel{
		resumeIDInput:    resumeInput,
		agentInput:       agentInput,
		extraArgsInput:   extraArgsInput,
		startPromptInput: startPromptInput,
		focusIndex:       -1,
	}
}

func (p *AgyOptionsPanel) focusOrder() []string {
	if p.sessionMode == 2 {
		return []string{"sessionMode", "resumeID", "mode", "dangerousMode", "sandbox", "agent", "extraArgs", "startPrompt"}
	}
	return []string{"sessionMode", "mode", "dangerousMode", "sandbox", "agent", "extraArgs", "startPrompt"}
}

func (p *AgyOptionsPanel) currentControl() string {
	order := p.focusOrder()
	if p.focusIndex < 0 || p.focusIndex >= len(order) {
		return ""
	}
	return order[p.focusIndex]
}

func (p *AgyOptionsPanel) clampFocus() {
	if p.focusIndex < 0 {
		return
	}
	if maxIdx := len(p.focusOrder()) - 1; p.focusIndex > maxIdx {
		p.focusIndex = maxIdx
	}
}

func (p *AgyOptionsPanel) updateInputFocus() {
	p.resumeIDInput.Blur()
	p.agentInput.Blur()
	p.extraArgsInput.Blur()
	p.startPromptInput.Blur()
	switch p.currentControl() {
	case "resumeID":
		p.resumeIDInput.Focus()
	case "agent":
		p.agentInput.Focus()
	case "extraArgs":
		p.extraArgsInput.Focus()
	case "startPrompt":
		p.startPromptInput.Focus()
	}
}

func (p *AgyOptionsPanel) activeTextInput() *textinput.Model {
	switch p.currentControl() {
	case "resumeID":
		return &p.resumeIDInput
	case "agent":
		return &p.agentInput
	case "extraArgs":
		return &p.extraArgsInput
	case "startPrompt":
		return &p.startPromptInput
	default:
		return nil
	}
}

// SetDefaults applies defaults from resolved session.AgyOptions (or resets to zero values when nil).
func (p *AgyOptionsPanel) SetDefaults(opts *session.AgyOptions) {
	p.SetFromOptions(opts)
}

// SetFromOptions populates panel controls from an existing AgyOptions struct.
func (p *AgyOptionsPanel) SetFromOptions(opts *session.AgyOptions) {
	p.sessionMode = 0
	p.modeCursor = 0
	p.dangerousMode = false
	p.sandbox = false
	p.resumeIDInput.SetValue("")
	p.agentInput.SetValue("")
	p.extraArgsInput.SetValue("")
	if opts == nil {
		p.clampFocus()
		p.updateInputFocus()
		return
	}
	if idx := slices.Index(agySessionModes, opts.SessionMode); idx >= 0 {
		p.sessionMode = idx
	}
	p.resumeIDInput.SetValue(strings.TrimSpace(opts.ResumeConversationID))
	for i, item := range agyExecutionModes {
		if item.value == opts.Mode {
			p.modeCursor = i
			break
		}
	}
	p.dangerousMode = opts.DangerousMode
	p.sandbox = opts.Sandbox
	p.agentInput.SetValue(strings.TrimSpace(opts.Agent))
	p.SetExtraArgs(opts.ExtraArgs)
	p.clampFocus()
	p.updateInputFocus()
}

// SetExtraArgs formats argument tokens into extraArgsInput while preserving quoted whitespace.
func (p *AgyOptionsPanel) SetExtraArgs(tokens []string) {
	formatted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.ContainsAny(tok, " \t\n\"'\\") {
			formatted = append(formatted, shellescape.Quote(tok))
		} else {
			formatted = append(formatted, tok)
		}
	}
	p.extraArgsInput.SetValue(strings.Join(formatted, " "))
}

// GetExtraArgs parses extraArgsInput using POSIX shell word splitting.
func (p *AgyOptionsPanel) GetExtraArgs() []string {
	raw := strings.TrimSpace(p.extraArgsInput.Value())
	if raw == "" {
		return nil
	}
	words, ok := shellwords.Split(raw)
	if !ok {
		words = strings.Fields(raw)
	}
	var out []string
	for _, w := range words {
		if trimmed := strings.TrimSpace(w); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetYoloMode reports whether DangerousMode (--dangerously-skip-permissions) is checked.
func (p *AgyOptionsPanel) GetYoloMode() bool {
	return p.dangerousMode
}

// GetStartPrompt returns the trimmed ephemeral initial prompt (--prompt-interactive) string.
func (p *AgyOptionsPanel) GetStartPrompt() string {
	return strings.TrimSpace(p.startPromptInput.Value())
}

// SetStartPrompt sets the initial prompt text input (used for snapshots and tests).
func (p *AgyOptionsPanel) SetStartPrompt(prompt string) {
	p.startPromptInput.SetValue(prompt)
}

// ResetStartPrompt clears the per-session initial prompt between dialog openings.
func (p *AgyOptionsPanel) ResetStartPrompt() {
	p.startPromptInput.SetValue("")
}

// GetOptions returns the serialized AgyOptions representing the panel plus shared model/effort.
func (p *AgyOptionsPanel) GetOptions(model, effort string) *session.AgyOptions {
	mode := agySessionModes[p.sessionMode]
	resumeID := ""
	if p.sessionMode == 2 {
		resumeID = strings.TrimSpace(p.resumeIDInput.Value())
	}
	return &session.AgyOptions{
		SessionMode:          mode,
		ResumeConversationID: resumeID,
		Model:                strings.TrimSpace(model),
		Effort:               strings.TrimSpace(effort),
		Mode:                 agyExecutionModes[p.modeCursor].value,
		Agent:                strings.TrimSpace(p.agentInput.Value()),
		DangerousMode:        p.dangerousMode,
		Sandbox:              p.sandbox,
		ExtraArgs:            p.GetExtraArgs(),
	}
}

// Focus activates focus on the first control in the panel.
func (p *AgyOptionsPanel) Focus() {
	p.focusIndex = 0
	p.updateInputFocus()
}

// Blur removes focus from the panel and its text inputs.
func (p *AgyOptionsPanel) Blur() {
	p.focusIndex = -1
	p.updateInputFocus()
}

// IsFocused reports whether any control inside the panel holds focus.
func (p *AgyOptionsPanel) IsFocused() bool {
	return p.focusIndex >= 0
}

// AtTop reports whether focus is on the first control.
func (p *AgyOptionsPanel) AtTop() bool {
	return p.focusIndex <= 0
}

// AtBottom reports whether focus is on the last control.
func (p *AgyOptionsPanel) AtBottom() bool {
	return p.focusIndex >= len(p.focusOrder())-1
}

// FocusLast moves focus to the last control when entering backwards (Shift+Tab / Up).
func (p *AgyOptionsPanel) FocusLast() {
	p.focusIndex = len(p.focusOrder()) - 1
	p.updateInputFocus()
}

// FocusedLine returns the zero-based logical line in View occupying focus, or -1 if blurred.
func (p *AgyOptionsPanel) FocusedLine() int {
	if p.focusIndex < 0 {
		return -1
	}
	return 1 + p.focusIndex
}

// Update handles keyboard navigation, toggles, and text input inside the panel.
func (p *AgyOptionsPanel) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	orderLen := len(p.focusOrder())
	switch key.String() {
	case "up", "shift+tab":
		if p.focusIndex < 0 {
			return nil
		}
		p.focusIndex--
		if p.focusIndex < 0 {
			p.focusIndex = orderLen - 1
		}
		p.updateInputFocus()
		return nil
	case "down", "tab":
		if p.focusIndex < 0 {
			return nil
		}
		p.focusIndex = (p.focusIndex + 1) % orderLen
		p.updateInputFocus()
		return nil
	}

	if activeInput := p.activeTextInput(); activeInput != nil {
		if key.Type == tea.KeySpace && len(key.Runes) == 0 && !key.Alt {
			// Ensure spacebars without explicit runes (e.g. CSI u compat) insert a space.
			key.Runes = []rune{' '}
			msg = key
		}
		var cmd tea.Cmd
		*activeInput, cmd = activeInput.Update(msg)
		return cmd
	}

	switch key.String() {
	case "y":
		p.dangerousMode = !p.dangerousMode
		return nil
	case " ":
		p.handleSpaceKey()
		return nil
	case "left":
		p.cycleFocusedRadio(-1)
		return nil
	case "right":
		p.cycleFocusedRadio(1)
		return nil
	}
	return nil
}

func (p *AgyOptionsPanel) handleSpaceKey() {
	switch p.currentControl() {
	case "sessionMode":
		p.sessionMode = (p.sessionMode + 1) % len(agySessionModes)
	case "mode":
		p.modeCursor = (p.modeCursor + 1) % len(agyExecutionModes)
	case "dangerousMode":
		p.dangerousMode = !p.dangerousMode
	case "sandbox":
		p.sandbox = !p.sandbox
	}
}

func (p *AgyOptionsPanel) cycleFocusedRadio(delta int) {
	switch p.currentControl() {
	case "sessionMode":
		n := len(agySessionModes)
		p.sessionMode = (p.sessionMode + delta + n) % n
	case "mode":
		n := len(agyExecutionModes)
		p.modeCursor = (p.modeCursor + delta + n) % n
	}
}

// View renders the Antigravity CLI launch options panel.
func (p *AgyOptionsPanel) View() string {
	activeStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	headerStyle := lipgloss.NewStyle().Foreground(ColorComment)

	var b strings.Builder
	b.WriteString(headerStyle.Render("─ Antigravity Options ─"))
	b.WriteString("\n")

	cur := p.currentControl()

	// Session mode radio row
	sessionFocused := cur == "sessionMode"
	if sessionFocused {
		b.WriteString(activeStyle.Render("▶ Session: "))
	} else {
		b.WriteString("  Session: ")
	}
	b.WriteString(renderRadioOption("New", p.sessionMode == 0, sessionFocused) + "  ")
	b.WriteString(renderRadioOption("Continue", p.sessionMode == 1, sessionFocused) + "  ")
	b.WriteString(renderRadioOption("Resume", p.sessionMode == 2, sessionFocused) + "\n")

	// Conditional conversation ID input when Resume is selected
	if p.sessionMode == 2 {
		if cur == "resumeID" {
			b.WriteString(activeStyle.Render("    ▶ ID: "))
		} else {
			b.WriteString("      ID: ")
		}
		b.WriteString(p.resumeIDInput.View() + "\n")
	}

	// Execution mode radio row
	modeFocused := cur == "mode"
	if modeFocused {
		b.WriteString(activeStyle.Render("▶ Mode: "))
	} else {
		b.WriteString("  Mode: ")
	}
	for i, item := range agyExecutionModes {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(renderRadioOption(item.label, p.modeCursor == i, modeFocused))
	}
	b.WriteString("\n")

	// Boolean flags
	b.WriteString(renderCheckboxLine("YOLO mode (--dangerously-skip-permissions)", p.dangerousMode, cur == "dangerousMode"))
	b.WriteString(renderCheckboxLine("Terminal sandbox (--sandbox)", p.sandbox, cur == "sandbox"))

	// Text fields
	writeInputRow := func(control, label string, input textinput.Model) {
		if cur == control {
			b.WriteString(activeStyle.Render("▶ " + label + ": "))
		} else {
			b.WriteString("  " + label + ": ")
		}
		b.WriteString(input.View() + "\n")
	}
	writeInputRow("agent", "CLI agent", p.agentInput)
	writeInputRow("extraArgs", "Extra args", p.extraArgsInput)
	writeInputRow("startPrompt", "Start prompt", p.startPromptInput)

	return strings.TrimSuffix(b.String(), "\n")
}
