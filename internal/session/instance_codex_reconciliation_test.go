package session

import (
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/tmux"
)

type codexOuterStatusRecorder struct {
	sampleCalls  int
	confirmCalls int
	confirmWant  string
}

func newCodexOuterStatusTestInstance(
	name string,
	sample tmux.CodexStatusSample,
	confirmed bool,
	confirmedAt time.Time,
	recorder *codexOuterStatusRecorder,
) *Instance {
	return &Instance{
		ID:                  name,
		Title:               name,
		Tool:                "codex",
		Status:              StatusRunning,
		CreatedAt:           time.Now().Add(-time.Minute),
		lastSessionMetaSync: time.Now(),
		tmuxSession:         &tmux.Session{Name: name, Command: "codex"},
		tmuxStatusExistsForTest: func() bool {
			return true
		},
		tmuxStatusSampleForTest: func() (string, tmux.CodexStatusSample, error) {
			recorder.sampleCalls++
			return "waiting", sample, nil
		},
		tmuxCodexDemotionConfirmForTest: func(expected string) (bool, time.Time) {
			recorder.confirmCalls++
			recorder.confirmWant = expected
			return confirmed, confirmedAt
		},
	}
}

func decisiveCodexWaitingSample(at time.Time, readyAgreement bool) tmux.CodexStatusSample {
	return tmux.CodexStatusSample{
		PaneRead:             true,
		ReadyPromptAgreement: readyAgreement,
		EvidenceStatus:       "waiting",
		EvidenceAt:           at,
		Observation:          tmux.CodexStatusObservationPrompt,
	}
}

func TestInstanceUpdateStatus_CodexOneCallDemotionConfirmation(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name            string
		readyAgreement  bool
		confirmed       bool
		confirmedAt     time.Time
		wantStatus      Status
		wantConfirmCall int
		wantEvidenceAt  int64
	}{
		{
			name:            "agreement accepts waiting in one call",
			confirmed:       true,
			confirmedAt:     now.Add(time.Second),
			wantStatus:      StatusWaiting,
			wantConfirmCall: 1,
			wantEvidenceAt:  now.Add(time.Second).Unix(),
		},
		{
			name:            "disagreement holds running",
			confirmed:       false,
			wantStatus:      StatusRunning,
			wantConfirmCall: 1,
		},
		{
			name:            "failed confirmation holds running",
			confirmed:       false,
			confirmedAt:     time.Time{},
			wantStatus:      StatusRunning,
			wantConfirmCall: 1,
		},
		{
			name:            "Ready prompt agreement needs no second capture",
			readyAgreement:  true,
			confirmed:       false,
			wantStatus:      StatusWaiting,
			wantConfirmCall: 0,
			wantEvidenceAt:  now.Unix(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &codexOuterStatusRecorder{}
			inst := newCodexOuterStatusTestInstance(
				"outer-"+tc.name,
				decisiveCodexWaitingSample(now, tc.readyAgreement),
				tc.confirmed,
				tc.confirmedAt,
				recorder,
			)

			if err := inst.UpdateStatus(); err != nil {
				t.Fatalf("UpdateStatus() error = %v", err)
			}
			if got := inst.GetStatusThreadSafe(); got != tc.wantStatus {
				t.Fatalf("UpdateStatus() status = %q, want %q", got, tc.wantStatus)
			}
			if recorder.sampleCalls != 1 || recorder.confirmCalls != tc.wantConfirmCall {
				t.Fatalf("status/confirmation calls = %d/%d, want 1/%d", recorder.sampleCalls, recorder.confirmCalls, tc.wantConfirmCall)
			}
			if recorder.confirmCalls > 0 && recorder.confirmWant != "waiting" {
				t.Fatalf("confirmation expected status = %q, want waiting", recorder.confirmWant)
			}
			_, evidenceAt, _ := inst.StatusEvidenceSnapshot()
			if evidenceAt != tc.wantEvidenceAt {
				t.Fatalf("outward evidence = %d, want %d", evidenceAt, tc.wantEvidenceAt)
			}
		})
	}
}

func TestInstanceUpdateStatus_CodexColdWrappersConvergeAfterOneConfirmation(t *testing.T) {
	now := time.Now()
	for wrapper := 0; wrapper < 2; wrapper++ {
		recorder := &codexOuterStatusRecorder{}
		inst := newCodexOuterStatusTestInstance(
			"cold-wrapper",
			decisiveCodexWaitingSample(now, false),
			true,
			now.Add(time.Second),
			recorder,
		)
		if err := inst.UpdateStatus(); err != nil {
			t.Fatalf("wrapper %d UpdateStatus() error = %v", wrapper+1, err)
		}
		if got := inst.GetStatusThreadSafe(); got != StatusWaiting {
			t.Fatalf("wrapper %d status = %q, want waiting", wrapper+1, got)
		}
		if recorder.sampleCalls != 1 || recorder.confirmCalls != 1 {
			t.Fatalf("wrapper %d calls = %d/%d, want exactly 1/1", wrapper+1, recorder.sampleCalls, recorder.confirmCalls)
		}
	}
}

func TestInstanceUpdateStatus_CodexDueBypassesIdleGateWithoutChangingShellCadence(t *testing.T) {
	newIdleInstance := func(tool string) (*Instance, *codexOuterStatusRecorder) {
		recorder := &codexOuterStatusRecorder{}
		inst := &Instance{
			ID:                  "idle-" + tool,
			Title:               "idle-" + tool,
			Tool:                tool,
			Status:              StatusIdle,
			CreatedAt:           time.Now().Add(-time.Minute),
			lastIdleCheck:       time.Now(),
			lastSessionMetaSync: time.Now(),
			tmuxSession:         &tmux.Session{Name: "idle-" + tool, Command: tool},
			tmuxStatusExistsForTest: func() bool {
				return true
			},
			tmuxStatusSampleForTest: func() (string, tmux.CodexStatusSample, error) {
				recorder.sampleCalls++
				return "active", tmux.CodexStatusSample{
					PaneRead:       true,
					EvidenceStatus: "active",
					EvidenceAt:     time.Now(),
				}, nil
			},
		}
		return inst, recorder
	}

	codex, codexRecorder := newIdleInstance("codex")
	if err := codex.UpdateStatus(); err != nil {
		t.Fatalf("Codex UpdateStatus() error = %v", err)
	}
	if codexRecorder.sampleCalls != 1 || codex.GetStatusThreadSafe() != StatusRunning {
		t.Fatalf("due Codex idle gate = calls/status %d/%q, want 1/running", codexRecorder.sampleCalls, codex.GetStatusThreadSafe())
	}

	shell, shellRecorder := newIdleInstance("shell")
	if err := shell.UpdateStatus(); err != nil {
		t.Fatalf("shell UpdateStatus() error = %v", err)
	}
	if shellRecorder.sampleCalls != 0 || shell.GetStatusThreadSafe() != StatusIdle {
		t.Fatalf("non-Codex idle cadence = calls/status %d/%q, want 0/idle", shellRecorder.sampleCalls, shell.GetStatusThreadSafe())
	}
}
