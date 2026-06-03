package doctor

import (
	"testing"

	"github.com/greprules/greprules/internal/config"
)

func TestRecommendationsPreferSystemWhenManagedIsMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OpenGrep.Mode = "managed"
	cfg.OpenGrep.Managed = true

	report := Report{
		OpenGrep: OpenGrepStatus{
			Managed: RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
			System:  RuntimeCheck{OK: true},
			Active:  RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
		},
	}

	AddOpenGrepRecommendations(&report, cfg)

	if !containsString(report.RecommendedCommands, "greprules config set opengrep.mode system --global") {
		t.Fatalf("expected system config recommendation, got %#v", report.RecommendedCommands)
	}
	if containsString(report.RecommendedCommands, "greprules setup-opengrep") {
		t.Fatalf("expected system recommendation to take precedence over setup, got %#v", report.RecommendedCommands)
	}
}

func TestRecommendationsUseManagedSetupWhenNoRuntimeIsReady(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OpenGrep.Mode = "managed"
	cfg.OpenGrep.Managed = true

	report := Report{
		OpenGrep: OpenGrepStatus{
			Managed: RuntimeCheck{OK: false, Error: "managed OpenGrep runtime is not installed"},
			System:  RuntimeCheck{OK: false, Error: "system opengrep not found"},
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
