package output

import (
	"encoding/json"
	"os"
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

func WriteAgentResult(path string, result AgentResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
