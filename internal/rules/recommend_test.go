package rules

import "testing"

func TestForDetectionRequiresSelectionMetadata(t *testing.T) {
	result := Result{
		Languages:  []Signal{{Name: "typescript", Confidence: 0.9}},
		Frameworks: []Signal{{Name: "nextjs", Confidence: 0.8}},
	}
	candidates := ForDetection(result, []PackSummary{{Slug: "javascript-typescript-security"}})
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates without selection metadata, got %#v", candidates)
	}
}

func TestForDetectionReturnsNoOfflineFallback(t *testing.T) {
	result := Result{
		Languages:  []Signal{{Name: "python", Confidence: 0.9}},
		Frameworks: []Signal{{Name: "fastapi", Confidence: 0.8}},
	}
	ids := PackIDs(ForDetection(result, nil))
	if len(ids) != 0 {
		t.Fatalf("expected no offline fallback candidates, got %#v", ids)
	}
}

func TestForDetectionUsesRegistrySelectionMetadata(t *testing.T) {
	result := Result{
		Languages:  []Signal{{Name: "python", Confidence: 0.9}},
		Frameworks: []Signal{{Name: "fastapi", Confidence: 0.8}},
	}
	candidates := ForDetection(result, []PackSummary{
		{
			Slug: "custom-python-security",
			Selection: PackSelection{
				Kind:      "language",
				Languages: []string{"python"},
			},
		},
		{
			Slug: "custom-fastapi-security",
			Selection: PackSelection{
				Kind:       "framework",
				Languages:  []string{"python"},
				Frameworks: []string{"fastapi"},
			},
		},
	})
	ids := PackIDs(candidates)
	if len(ids) != 2 || ids[0] != "custom-fastapi-security" || ids[1] != "custom-python-security" {
		t.Fatalf("expected metadata-driven custom pack order, got %#v", candidates)
	}
}

func TestForDetectionSkipsSourceSelectionPacks(t *testing.T) {
	result := Result{
		Languages: []Signal{{Name: "python", Confidence: 0.9}},
	}
	candidates := ForDetection(result, []PackSummary{
		{
			Slug:      "source-all-rules",
			Languages: []string{"python"},
			Selection: PackSelection{
				Kind:        "source",
				SourceTypes: []string{"indexed"},
			},
		},
		{
			Slug: "python-security",
			Selection: PackSelection{
				Kind:      "language",
				Languages: []string{"python"},
			},
		},
	})
	ids := PackIDs(candidates)
	if len(ids) != 1 || ids[0] != "python-security" {
		t.Fatalf("expected source pack to be skipped, got %#v", candidates)
	}
}
