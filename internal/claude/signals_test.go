package claude

import (
	"fmt"
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

func TestClaudeSession_Signals(t *testing.T) {
	tests := []struct {
		name             string
		status           ClaudeSessionStatus
		expectedSeverity common.SignalSeverity
		expectedLabel    string
		expectNil        bool
	}{
		{
			name:             "NeedsAttention returns SeverityUrgent",
			status:           StatusNeedsAttention,
			expectedSeverity: common.SeverityUrgent,
			expectedLabel:    "needs attention",
		},
		{
			name:             "Working returns SeverityActive",
			status:           StatusWorking,
			expectedSeverity: common.SeverityActive,
			expectedLabel:    "working",
		},
		{
			name:             "ReadyForReview returns SeverityWarning",
			status:           StatusReadyForReview,
			expectedSeverity: common.SeverityWarning,
			expectedLabel:    "ready for review",
		},
		{
			name:             "Done returns SeveritySuccess",
			status:           StatusDone,
			expectedSeverity: common.SeveritySuccess,
			expectedLabel:    "done",
		},
		{
			name:             "Idle returns SeverityInfo",
			status:           StatusIdle,
			expectedSeverity: common.SeverityInfo,
			expectedLabel:    "idle",
		},
		{
			name:      "unknown status returns nil",
			status:    ClaudeSessionStatus("unknown"),
			expectNil: true,
		},
		{
			name:      "empty status returns nil",
			status:    ClaudeSessionStatus(""),
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := &ClaudeSession{
				Model:  gorm.Model{ID: 42},
				Status: tt.status,
			}
			signals := cs.Signals()

			if tt.expectNil {
				if signals != nil {
					t.Fatalf("expected nil signals, got %v", signals)
				}
				return
			}

			if len(signals) != 1 {
				t.Fatalf("expected 1 signal, got %d", len(signals))
			}

			sig := signals[0]
			if sig.Severity != tt.expectedSeverity {
				t.Errorf("expected severity %q, got %q", tt.expectedSeverity, sig.Severity)
			}
			if sig.Label != tt.expectedLabel {
				t.Errorf("expected label %q, got %q", tt.expectedLabel, sig.Label)
			}
			if sig.Source != "claude" {
				t.Errorf("expected source %q, got %q", "claude", sig.Source)
			}
			expectedKey := fmt.Sprintf("claude:%d", cs.ID)
			if sig.Key != expectedKey {
				t.Errorf("expected key %q, got %q", expectedKey, sig.Key)
			}
		})
	}
}
