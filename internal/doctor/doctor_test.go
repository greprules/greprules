package doctor

import (
	"testing"

	"github.com/greprules/greprules/internal/config"
)

func TestRecommendationsUseManagedSetupWhenManagedIsMissing(t *testing.T) {
	cfg := config.DefaultConfig()

	report := Report{
		OpenGrep: OpenGrepStatus{
			Managed: RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
			Active:  RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
		},
	}

	AddOpenGrepRecommendations(&report, cfg)

	if !containsString(report.RecommendedCommands, "greprules setup-opengrep") {
		t.Fatalf("expected setup recommendation, got %#v", report.RecommendedCommands)
	}
	for _, command := range report.RecommendedCommands {
		if command == "greprules agent-config set opengrep.mode system --global" {
			t.Fatalf("status should not recommend config set, got %#v", report.RecommendedCommands)
		}
	}
}

func TestRecommendationsUseManagedSetupWhenNoRuntimeIsReady(t *testing.T) {
	cfg := config.DefaultConfig()

	report := Report{
		OpenGrep: OpenGrepStatus{
			Managed: RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
			Active:  RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
		},
	}

	AddOpenGrepRecommendations(&report, cfg)

	if !containsString(report.RecommendedCommands, "greprules setup-opengrep") {
		t.Fatalf("expected setup recommendation, got %#v", report.RecommendedCommands)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
