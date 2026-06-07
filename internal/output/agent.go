package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type AgentResult struct {
	SchemaVersion string     `json:"schemaVersion"`
	Status        string     `json:"status"`
	Repo          RepoInfo   `json:"repo"`
	Packs         []PackInfo `json:"packs"`
	Engine        EngineInfo `json:"engine"`
	Scan          ScanInfo   `json:"scan"`
	Findings      []Finding  `json:"findings"`
	JSONPath      string     `json:"jsonPath,omitempty"`
	SARIFPath     string     `json:"sarifPath,omitempty"`
	Warnings      []string   `json:"warnings,omitempty"`
	Errors        []string   `json:"errors,omitempty"`
}

type SummaryOptions struct {
	Automatic bool
	Label     string
}

type RepoInfo struct {
	Root         string   `json:"root"`
	ChangedMode  bool     `json:"changedMode"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
}

type PackInfo struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	RulePath   string `json:"rulePath"`
	TotalRules int    `json:"totalRules"`
}

type EngineInfo struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Source  string `json:"source"`
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Managed bool   `json:"managed"`
}

type ScanInfo struct {
	StartedAt  string   `json:"startedAt,omitempty"`
	FinishedAt string   `json:"finishedAt,omitempty"`
	Targets    []string `json:"targets"`
	Configs    []string `json:"configs,omitempty"`
}

type Finding struct {
	RuleID   string         `json:"ruleId"`
	Path     string         `json:"path"`
	Start    Location       `json:"start"`
	End      Location       `json:"end"`
	Message  string         `json:"message"`
	Severity string         `json:"severity"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Location struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

func FindingsFromOpenGrepJSON(path string) ([]Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Results []struct {
			CheckID string   `json:"check_id"`
			Path    string   `json:"path"`
			Start   Location `json:"start"`
			End     Location `json:"end"`
			Extra   struct {
				Message  string         `json:"message"`
				Severity string         `json:"severity"`
				Metadata map[string]any `json:"metadata"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	findings := make([]Finding, 0, len(raw.Results))
	for _, result := range raw.Results {
		findings = append(findings, Finding{
			RuleID:   result.CheckID,
			Path:     result.Path,
			Start:    result.Start,
			End:      result.End,
			Message:  result.Extra.Message,
			Severity: result.Extra.Severity,
			Metadata: result.Extra.Metadata,
		})
	}
	return findings, nil
}

func WarningsFromOpenGrepJSON(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Errors []map[string]any `json:"errors"`
		Paths  struct {
			Skipped []map[string]any `json:"skipped"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	warnings := make([]string, 0, len(raw.Errors)+len(raw.Paths.Skipped))
	for _, diagnostic := range raw.Errors {
		warnings = append(warnings, "OpenGrep diagnostic: "+diagnosticSummary(diagnostic))
	}
	for _, skipped := range raw.Paths.Skipped {
		warnings = append(warnings, "OpenGrep skipped path: "+diagnosticSummary(skipped))
	}
	return warnings, nil
}

func diagnosticSummary(values map[string]any) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := []string{"type", "level", "code", "path", "message", "reason"}
	parts := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			parts = append(parts, key+"="+diagnosticValue(value))
			seen[key] = true
		}
	}
	for key, value := range values {
		if !seen[key] {
			parts = append(parts, key+"="+diagnosticValue(value))
		}
	}
	return strings.Join(parts, " ")
}

func diagnosticValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%g", typed)
	case bool:
		return fmt.Sprintf("%t", typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func WriteAgentResult(path string, result AgentResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func SummarizeAgentResult(path string, options SummaryOptions) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	var result AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	scanLabel := options.Label
	if scanLabel == "" {
		scanLabel = "edited-file"
		if result.Repo.ChangedMode {
			scanLabel = "changed-file"
		}
	}
	prefix := "greprules "
	if options.Automatic {
		prefix += "automatic "
	}
	lines := []string{
		fmt.Sprintf(
			"%s%s scan completed: status=%s, findings=%d, warnings=%d, errors=%d, targets=%d.",
			prefix,
			scanLabel,
			fallbackString(result.Status, "unknown"),
			len(result.Findings),
			len(result.Warnings),
			len(result.Errors),
			len(result.Scan.Targets),
		),
	}
	if len(result.Findings) == 0 {
		if options.Automatic {
			lines = append(lines, "No OpenGrep findings were reported for the current automatic scan.")
		} else {
			lines = append(lines, "No OpenGrep findings were reported for this scan.")
		}
	} else if options.Automatic {
		lines = append(lines, "Review "+path+" and classify each finding as true positive, false positive, or needs investigation. Report findings and reasoning only. Do not edit code, add suppressions, chase zero findings, or rerun greprules unless the user explicitly asks.")
	} else {
		lines = append(lines, "Review "+path+" and classify each finding as true positive, false positive, or needs investigation. Report findings and reasoning only unless the user asks for fixes.")
	}
	lines = append(lines, "Full result: "+path)
	return strings.Join(lines, "\n")
}

func fallbackString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
