package recommend

import (
	"testing"

	"github.com/greprules/greprules/internal/detect"
	"github.com/greprules/greprules/internal/registry"
)

func TestForDetectionFiltersUnavailablePacks(t *testing.T) {
	result := detect.Result{
		Languages:  []detect.Signal{{Name: "typescript", Confidence: 0.9}},
		Frameworks: []detect.Signal{{Name: "nextjs", Confidence: 0.8}},
	}
	candidates := ForDetection(result, []registry.PackSummary{{Slug: "javascript-typescript-security"}})
	ids := PackIDs(candidates)
	if len(ids) != 1 || ids[0] != "javascript-typescript-security" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
}

func TestForDetectionAddsPythonWebPack(t *testing.T) {
	result := detect.Result{
		Languages:  []detect.Signal{{Name: "python", Confidence: 0.9}},
		Frameworks: []detect.Signal{{Name: "fastapi", Confidence: 0.8}},
	}
	ids := PackIDs(ForDetection(result, nil))
	if !contains(ids, "python-security") || !contains(ids, "python-web-security") {
		t.Fatalf("expected python packs, got %#v", ids)
	}
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}
