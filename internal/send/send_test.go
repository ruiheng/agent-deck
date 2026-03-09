package send

import (
	"strings"
	"testing"
)

func TestHasUnsentPastedPrompt(t *testing.T) {
	if !HasUnsentPastedPrompt("❯ [Pasted text #1 +89 lines]") {
		t.Fatal("expected pasted prompt marker to be detected")
	}
	if HasUnsentPastedPrompt("normal terminal output") {
		t.Fatal("did not expect normal output to be detected as pasted prompt")
	}
	// Case-insensitive detection
	if !HasUnsentPastedPrompt("[PASTED TEXT #2 +10 lines]") {
		t.Fatal("expected case-insensitive pasted prompt detection")
	}
}

func TestHasUnsentComposerPrompt(t *testing.T) {
	content := "────────────────\n❯\u00a0Write one line: LAUNCH_OK\n[Opus 4.6] Context: 0%"
	if !HasUnsentComposerPrompt(content, "Write one line: LAUNCH_OK") {
		t.Fatal("expected unsent composer prompt to be detected")
	}
	if HasUnsentComposerPrompt(content, "Different text") {
		t.Fatal("did not expect mismatched composer text to be detected")
	}

	// Wrapped current composer lines only expose a prefix of the message.
	wrappedContent := "────────────────\n❯\u00a0Read these 3 files and produce a summary for DIAGTOKEN_123. Keep\n  under 80 lines and include one verdict line.\n────────────────\n[Opus 4.6] Context: 0%"
	wrappedMessage := "Read these 3 files and produce a summary for DIAGTOKEN_123. Keep under 80 lines and include one verdict line."
	if !HasUnsentComposerPrompt(wrappedContent, wrappedMessage) {
		t.Fatal("expected wrapped unsent composer prompt to be detected")
	}

	// Claude hint suggestions should not be treated as unsent input for a
	// different message.
	suggestion := "────────────────\n❯\u00a0Try \"write a test for <filepath>\"\n────────────────\n[Opus 4.6] Context: 0%"
	if HasUnsentComposerPrompt(suggestion, wrappedMessage) {
		t.Fatal("did not expect suggestion placeholder to be treated as unsent composer input")
	}
}

func TestHasUnsentComposerPrompt_SubmittedHistory(t *testing.T) {
	// Submitted messages can appear in history; only current composer should count.
	submitted := "❯ Write one line: LAUNCH_OK\n✳ Tempering…\n────────────────\n❯\n────────────────\n[Opus 4.6] Context: 0%"
	if HasUnsentComposerPrompt(submitted, "Write one line: LAUNCH_OK") {
		t.Fatal("did not expect submitted history line to be treated as unsent composer input")
	}
}

func TestCurrentComposerPrompt_UsesBottomComposerBlock(t *testing.T) {
	content := strings.Join([]string{
		"> quoted output line from earlier response",
		"Some other output",
		"────────────────",
		"❯\u00a0Read these 3 files and produce a summary for DIAGTOKEN_123. Keep",
		"  under 80 lines and include one verdict line.",
		"────────────────",
		"[Opus 4.6] Context: 0%",
	}, "\n")

	got, ok := CurrentComposerPrompt(content)
	if !ok {
		t.Fatal("expected current composer prompt to be found")
	}
	want := "Read these 3 files and produce a summary for DIAGTOKEN_123. Keep under 80 lines and include one verdict line."
	if got != want {
		t.Fatalf("unexpected composer prompt.\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCurrentComposerPrompt_FindsTallFallbackPromptOutsideBottomWindow(t *testing.T) {
	contentLines := []string{
		"bash -c 'codex --model gpt-5.4 -a on-request'",
		"",
		"╭────────────────────────────────────────────╮",
		"│ >_ OpenAI Codex (v0.111.0)                 │",
		"╰────────────────────────────────────────────╯",
		"",
		"Tip: welcome banner",
		"",
		`› {`,
		`    "preconditions": {`,
		`      "must_fully_load_skills": [`,
		`        "agent-deck-workflow"`,
		`      ]`,
		`    },`,
		`    "execution": {`,
		`      "action": "execute_delegate_task",`,
		`      "artifact_path": ".agent-artifacts/example/delegate-task.md",`,
		`      "note": "Read and follow the delegate task file before any code change."`,
		`    },`,
		`    "context": {`,
		`      "task_id": "example-task"`,
		`    }`,
		`}`,
	}
	for range 60 {
		contentLines = append(contentLines, "")
	}
	content := strings.Join(contentLines, "\n")

	got, ok := CurrentComposerPrompt(content)
	if !ok {
		t.Fatal("expected tall fallback composer prompt to be found")
	}
	if !strings.HasPrefix(got, `{ "preconditions": {`) {
		t.Fatalf("expected JSON prompt body, got %q", got)
	}
	if !strings.Contains(got, `"task_id": "example-task"`) {
		t.Fatalf("expected task_id in prompt body, got %q", got)
	}
}

func TestNormalizePromptText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   \t  ", ""},
		{"NBSP replaced", "hello\u00a0world", "hello world"},
		{"multi whitespace collapsed", "hello   world  foo", "hello world foo"},
		{"leading/trailing trimmed", "  hello world  ", "hello world"},
		{"mixed NBSP and spaces", "\u00a0hello\u00a0\u00a0world\u00a0", "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePromptText(tt.input)
			if got != tt.want {
				t.Errorf("NormalizePromptText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsComposerDividerLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"long dash line", "────────────────", true},
		{"long hyphen line", "----------------", true},
		{"long bold dash line", "━━━━━━━━━━━━━━━━", true},
		{"short dash line (9)", "---------", false},
		{"exactly 10 dashes", "----------", true},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"mixed chars", "────abc────", false},
		{"with surrounding whitespace", "  ────────────────  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsComposerDividerLine(tt.line)
			if got != tt.want {
				t.Errorf("IsComposerDividerLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestHasCurrentComposerPrompt(t *testing.T) {
	content := "────────────────\n❯ hello world\n────────────────\n[Opus 4.6]"
	if !HasCurrentComposerPrompt(content) {
		t.Fatal("expected HasCurrentComposerPrompt to return true")
	}
	if HasCurrentComposerPrompt("no prompt here at all") {
		t.Fatal("expected HasCurrentComposerPrompt to return false for no prompt content")
	}
}

func TestParsePromptFromComposerBlock(t *testing.T) {
	lines := []string{
		"❯ hello world",
	}
	got, ok := ParsePromptFromComposerBlock(lines)
	if !ok {
		t.Fatal("expected ParsePromptFromComposerBlock to find prompt")
	}
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}

	got, ok = ParsePromptFromLineWithContinuations([]string{
		"prefix output",
		"› branch/example-feature-rename). If branch setup fails, stop and report.",
		"  After first implementation pass, commit and prepare review request.",
		"  gpt-5.4 high · 100% left · ~/workspace",
	}, 1)
	if !ok {
		t.Fatal("expected ParsePromptFromLineWithContinuations to find wrapped fallback prompt")
	}
	if !strings.Contains(got, "After first implementation pass") {
		t.Fatalf("expected wrapped fallback prompt body, got %q", got)
	}

	// No marker present
	_, ok = ParsePromptFromComposerBlock([]string{"no marker here"})
	if ok {
		t.Fatal("expected ParsePromptFromComposerBlock to return false for no marker")
	}
}
