package rules

import (
	"sort"
	"strings"
)

type Candidate struct {
	PackID     string   `json:"packId"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
	Languages  []string `json:"languages,omitempty"`
	Frameworks []string `json:"frameworks,omitempty"`
	Available  bool     `json:"available"`
}

type AgentContext struct {
	SchemaVersion       string        `json:"schemaVersion"`
	Root                string        `json:"root"`
	Targets             []string      `json:"targets"`
	Detection           Result        `json:"detection"`
	Candidates          []Candidate   `json:"candidates"`
	AvailablePacks      []PackSummary `json:"availablePacks"`
	NeedsAgentSelection bool          `json:"needsAgentSelection"`
	Guidance            []string      `json:"guidance"`
}

func BuildSelectionContext(selection Selection) AgentContext {
	result := selection.Detection
	return AgentContext{
		SchemaVersion:       "greprules.selection.agent.v1",
		Root:                result.Root,
		Targets:             append([]string{}, result.Targets...),
		Detection:           result,
		Candidates:          append([]Candidate{}, selection.Candidates...),
		AvailablePacks:      append([]PackSummary{}, selection.AvailablePacks...),
		NeedsAgentSelection: len(selection.PackIDs) == 0,
		Guidance: []string{
			"Inspect this selectionContext from the agent-scan scan response.",
			"Use candidates when confidence and target context match the user's scan request.",
			"If candidates are empty or incomplete, inspect targets and availablePacks, choose explicit pack slugs, then run greprules fetch <slug>.",
			"Prefer target-specific language/framework packs over broad repository packs for edited-file or explicit-target scans.",
			"Do not invent pack slugs; select only slugs present in availablePacks unless the user explicitly provided a pack slug.",
		},
	}
}

func ForDetection(result Result, available []PackSummary) []Candidate {
	candidates := make([]Candidate, 0, len(available))
	for _, pack := range available {
		if candidate, ok := candidateForPack(result, pack); ok {
			candidates = append(candidates, candidate)
		}
	}
	sortCandidates(candidates)
	return candidates
}

func candidateForPack(result Result, pack PackSummary) (Candidate, bool) {
	selection := pack.Selection
	if selection.Kind == "source" || selection.Kind == "manual" {
		return Candidate{}, false
	}
	languages := normalizeList(selection.Languages)
	frameworks := normalizeList(selection.Frameworks)
	if selection.Kind == "" && len(languages) == 0 && len(frameworks) == 0 {
		languages = normalizeList(pack.Languages)
	}
	kind := selection.Kind
	if kind == "" {
		if len(frameworks) > 0 {
			kind = "framework"
		} else if len(languages) > 0 {
			kind = "language"
		}
	}
	language, languageOK := bestLanguage(result.Languages, languages)
	framework, frameworkOK := bestFramework(result.Frameworks, frameworks)
	switch kind {
	case "language":
		if !languageOK {
			return Candidate{}, false
		}
		return Candidate{
			PackID:     pack.Slug,
			Reason:     "detected " + language.Name,
			Confidence: language.Confidence,
			Languages:  languages,
			Frameworks: frameworks,
			Available:  true,
		}, true
	case "framework":
		if !frameworkOK {
			return Candidate{}, false
		}
		confidence := framework.Confidence
		if languageOK && language.Confidence > 0 {
			confidence = minFloat(1, confidence+0.15)
		}
		return Candidate{
			PackID:     pack.Slug,
			Reason:     "detected " + framework.Name,
			Confidence: confidence,
			Languages:  languages,
			Frameworks: frameworks,
			Available:  true,
		}, true
	case "runtime":
		if frameworkOK {
			confidence := framework.Confidence
			if languageOK && language.Confidence > 0 {
				confidence = minFloat(1, confidence+0.1)
			}
			return Candidate{
				PackID:     pack.Slug,
				Reason:     "detected " + framework.Name,
				Confidence: confidence,
				Languages:  languages,
				Frameworks: frameworks,
				Available:  true,
			}, true
		}
		if languageOK {
			return Candidate{
				PackID:     pack.Slug,
				Reason:     "detected " + language.Name + " runtime ecosystem",
				Confidence: language.Confidence * 0.9,
				Languages:  languages,
				Frameworks: frameworks,
				Available:  true,
			}, true
		}
	}
	return Candidate{}, false
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

func bestLanguage(signals []Signal, packLanguages []string) (Signal, bool) {
	var best Signal
	for _, signal := range signals {
		for _, language := range packLanguages {
			if languageMatches(signal.Name, language) && signal.Confidence >= best.Confidence {
				best = signal
			}
		}
	}
	return best, best.Name != ""
}

func bestFramework(signals []Signal, packFrameworks []string) (Signal, bool) {
	var best Signal
	for _, signal := range signals {
		for _, framework := range packFrameworks {
			if normalizeToken(signal.Name) == normalizeToken(framework) && signal.Confidence >= best.Confidence {
				best = signal
			}
		}
	}
	return best, best.Name != ""
}

func languageMatches(detected string, packLanguage string) bool {
	detected = normalizeToken(detected)
	packLanguage = normalizeToken(packLanguage)
	if detected == "" || packLanguage == "" {
		return false
	}
	if detected == packLanguage {
		return true
	}
	switch detected {
	case "jvm":
		return packLanguage == "java" || packLanguage == "kotlin" || packLanguage == "scala"
	case "java", "kotlin", "scala":
		return packLanguage == "jvm"
	case "dotnet":
		return packLanguage == "csharp"
	case "csharp":
		return packLanguage == "dotnet"
	case "config":
		return packLanguage == "generic" || packLanguage == "yaml" || packLanguage == "json" || packLanguage == "bash" || packLanguage == "sh" || packLanguage == "dockerfile" || packLanguage == "terraform" || packLanguage == "properties"
	case "bash":
		return packLanguage == "sh"
	case "dockerfile", "terraform", "yaml", "json":
		return packLanguage == "config" || packLanguage == "generic"
	}
	return false
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized := normalizeToken(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortCandidates(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Confidence == candidates[j].Confidence {
			return candidates[i].PackID < candidates[j].PackID
		}
		return candidates[i].Confidence > candidates[j].Confidence
	})
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
