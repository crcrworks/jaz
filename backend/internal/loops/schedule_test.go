package loops

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreateDerivesNameWithoutMentionMarkup(t *testing.T) {
	input := CreateLoop{
		Prompt:   "Run [$code-review](/skills/code-review/SKILL.md) on [@backend/internal](</repos/my app/backend/internal>) daily",
		Schedule: Schedule{Kind: ScheduleCron, Expr: "0 9 * * *", Timezone: "UTC"},
	}
	normalized, _, err := NormalizeCreate(input, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NormalizeCreate: %v", err)
	}
	if got, want := normalized.Name, "Run $code-review on @backend/internal daily"; got != want {
		t.Fatalf("derived name = %q, want %q", got, want)
	}
}

// TestNormalizeCreateTakesEveryLevelAnAgentOffers pins that a routine stores the
// level the picker offered. pi names its thinking-off switch "off", so a routine
// on it has to keep that value instead of being refused at creation.
func TestNormalizeCreateTakesEveryLevelAnAgentOffers(t *testing.T) {
	for _, effort := range []string{"off", "none", "minimal", "low", "medium", "high", "xhigh", "max", " OFF "} {
		normalized, _, err := NormalizeCreate(CreateLoop{
			Prompt:          "standup",
			Schedule:        Schedule{Kind: ScheduleCron, Expr: "0 9 * * *", Timezone: "UTC"},
			ReasoningEffort: effort,
		}, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("effort %q: %v", effort, err)
		}
		if want := strings.ToLower(strings.TrimSpace(effort)); normalized.ReasoningEffort != want {
			t.Fatalf("effort %q stored as %q", effort, normalized.ReasoningEffort)
		}
	}
	if _, _, err := NormalizeCreate(CreateLoop{
		Prompt:          "standup",
		Schedule:        Schedule{Kind: ScheduleCron, Expr: "0 9 * * *", Timezone: "UTC"},
		ReasoningEffort: "turbo",
	}, time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("an unknown level must still be refused")
	}
}
