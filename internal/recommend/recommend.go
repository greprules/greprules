package recommend

import (
	"sort"

	"github.com/greprules/greprules/internal/detect"
	"github.com/greprules/greprules/internal/registry"
)

type Candidate struct {
	PackID     string   `json:"packId"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
	Languages  []string `json:"languages,omitempty"`
	Available  bool     `json:"available"`
}

func ForDetection(result detect.Result, available []registry.PackSummary) []Candidate {
	availableSet := map[string]registry.PackSummary{}
	for _, pack := range available {
		availableSet[pack.Slug] = pack
	}
	candidates := map[string]Candidate{}
	add := func(packID, reason string, confidence float64, languages ...string) {
		existing, ok := candidates[packID]
		if ok && existing.Confidence >= confidence {
			return
		}
		_, isAvailable := availableSet[packID]
		candidates[packID] = Candidate{
			PackID:     packID,
			Reason:     reason,
			Confidence: confidence,
			Languages:  languages,
			Available:  isAvailable || len(availableSet) == 0,
		}
	}
	for _, language := range result.Languages {
		switch language.Name {
		case "typescript", "javascript":
			add("javascript-typescript-security", "detected "+language.Name, language.Confidence, language.Name)
			add("nodejs-security", "detected JavaScript runtime ecosystem", language.Confidence*0.9, language.Name)
		case "python":
			add("python-security", "detected python", language.Confidence, "python")
		case "go":
			add("go-security", "detected go", language.Confidence, "go")
		case "java", "jvm":
			add("jvm-security", "detected JVM project", language.Confidence, language.Name)
		case "php":
			add("php-security", "detected php", language.Confidence, "php")
		case "ruby":
			add("ruby-security", "detected ruby", language.Confidence, "ruby")
		case "rust", "c", "cpp":
			add("c-cpp-rust-security", "detected native language project", language.Confidence, language.Name)
		case "csharp", "dotnet":
			add("dotnet-security", "detected .NET project", language.Confidence, language.Name)
		case "config":
			add("config-security", "detected configuration manifests", language.Confidence, "config")
		}
	}
	for _, framework := range result.Frameworks {
		switch framework.Name {
		case "nextjs", "react", "express", "nestjs":
			add("nodejs-security", "detected "+framework.Name, framework.Confidence, "javascript", "typescript")
		case "django", "flask", "fastapi":
			add("python-web-security", "detected "+framework.Name, framework.Confidence, "python")
		case "spring":
			add("java-spring-security", "detected spring", framework.Confidence, "java")
		case "laravel":
			add("php-laravel-security", "detected laravel", framework.Confidence, "php")
		}
	}
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Available {
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].PackID < out[j].PackID
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

func PackIDs(candidates []Candidate) []string {
	ids := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate.PackID] {
			continue
		}
		seen[candidate.PackID] = true
		ids = append(ids, candidate.PackID)
	}
	return ids
}
