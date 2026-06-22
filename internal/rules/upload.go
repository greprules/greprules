package rules

import (
	"context"
	"net/url"
)

type RuleUploadTestFixture struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Language string `json:"language,omitempty"`
	Code     string `json:"code"`
	Expected string `json:"expected,omitempty"`
}

type RuleUploadMetadata struct {
	License            string                  `json:"license"`
	Language           string                  `json:"language,omitempty"`
	Framework          string                  `json:"framework,omitempty"`
	Severity           string                  `json:"severity,omitempty"`
	Confidence         string                  `json:"confidence,omitempty"`
	CVE                []string                `json:"cve"`
	GHSA               []string                `json:"ghsa"`
	CWE                []string                `json:"cwe"`
	Tags               []string                `json:"tags"`
	References         []string                `json:"references"`
	Source             string                  `json:"source,omitempty"`
	SourceCommit       string                  `json:"source_commit,omitempty"`
	SourceType         string                  `json:"source_type"`
	SourceContext      string                  `json:"source_context"`
	RuleNamespace      string                  `json:"rule_namespace,omitempty"`
	OriginalRuleID     string                  `json:"original_rule_id,omitempty"`
	GeneratedBy        string                  `json:"generated_by,omitempty"`
	GeneratedAt        string                  `json:"generated_at,omitempty"`
	Version            string                  `json:"version,omitempty"`
	VulnerablePattern  string                  `json:"vulnerable_pattern,omitempty"`
	RecommendedFix     string                  `json:"recommended_fix,omitempty"`
	FalsePositiveNotes string                  `json:"false_positive_notes,omitempty"`
	Tests              []RuleUploadTestFixture `json:"tests"`
	AgentConsent       *ContributionConsent    `json:"agent_consent,omitempty"`
}

type RuleUploadRequest struct {
	Title          string             `json:"title"`
	Slug           string             `json:"slug,omitempty"`
	RuleID         string             `json:"rule_id,omitempty"`
	RuleNamespace  string             `json:"rule_namespace,omitempty"`
	OriginalRuleID string             `json:"original_rule_id,omitempty"`
	Description    string             `json:"description,omitempty"`
	YAML           string             `json:"yaml"`
	Metadata       RuleUploadMetadata `json:"metadata"`
}

type RuleUploadResponse struct {
	Success bool `json:"success"`
	Rule    struct {
		Slug             string `json:"slug"`
		ValidationStatus string `json:"validation_status"`
	} `json:"rule"`
	Version struct {
		Version          string `json:"version"`
		ValidationStatus string `json:"validation_status"`
	} `json:"version"`
	Validation struct {
		Status string `json:"status"`
	} `json:"validation"`
}

func (c Client) CreateRule(ctx context.Context, apiKey string, request RuleUploadRequest) (RuleUploadResponse, error) {
	var response RuleUploadResponse
	err := c.postJSON(ctx, "/api/rules", apiKey, request, &response)
	return response, err
}

func (c Client) UpdateRule(ctx context.Context, apiKey string, slug string, request RuleUploadRequest) (RuleUploadResponse, error) {
	var response RuleUploadResponse
	err := c.postJSON(ctx, "/api/me/rules/"+url.PathEscape(slug), apiKey, request, &response)
	return response, err
}
