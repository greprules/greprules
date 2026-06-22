package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type ContributionConsent struct {
	Mode            string   `json:"mode"`
	SessionID       string   `json:"session_id"`
	ApprovedAt      string   `json:"approved_at"`
	Scope           []string `json:"scope"`
	RedactionPolicy string   `json:"redaction_policy"`
}

type ScanContribution struct {
	Source           string   `json:"source"`
	GreprulesVersion string   `json:"greprules_version"`
	OpenGrepVersion  string   `json:"opengrep_version"`
	ProjectHash      string   `json:"project_hash"`
	Languages        []string `json:"languages"`
	Frameworks       []string `json:"frameworks"`
	RulePackSlugs    []string `json:"rule_pack_slugs"`
	PrivacyMode      string   `json:"privacy_mode"`
}

type ScanFindingContribution struct {
	RuleSlug           string         `json:"rule_slug"`
	OpenGrepRuleID     string         `json:"opengrep_rule_id"`
	RuleVersion        string         `json:"rule_version"`
	FindingFingerprint string         `json:"finding_fingerprint"`
	Severity           string         `json:"severity,omitempty"`
	PathHash           string         `json:"path_hash"`
	MessageHash        string         `json:"message_hash"`
	LineStart          int            `json:"line_start,omitempty"`
	LineEnd            int            `json:"line_end,omitempty"`
	Metadata           map[string]any `json:"metadata"`
}

type ScanDiagnosticContribution struct {
	FindingID      string         `json:"finding_id,omitempty"`
	RuleSlug       string         `json:"rule_slug,omitempty"`
	Kind           string         `json:"kind"`
	Severity       string         `json:"severity"`
	Language       string         `json:"language,omitempty"`
	Parser         string         `json:"parser,omitempty"`
	FileExtension  string         `json:"file_extension,omitempty"`
	PathHash       string         `json:"path_hash,omitempty"`
	MessageHash    string         `json:"message_hash"`
	DiagnosticCode string         `json:"diagnostic_code,omitempty"`
	Count          int            `json:"count"`
	Details        map[string]any `json:"details"`
}

type ScanCreateRequest struct {
	Consent  ContributionConsent       `json:"consent"`
	Scan     ScanContribution          `json:"scan"`
	Findings []ScanFindingContribution `json:"findings"`
}

type ScanCreatedFinding struct {
	ID                 string `json:"id"`
	RuleSlug           string `json:"rule_slug"`
	RuleVersion        string `json:"rule_version"`
	FindingFingerprint string `json:"finding_fingerprint"`
}

type ScanCreateResponse struct {
	Success  bool                 `json:"success"`
	ScanID   string               `json:"scan_id"`
	Findings []ScanCreatedFinding `json:"findings"`
}

type ScanDiagnosticCreateRequest struct {
	ScanID      string                       `json:"scan_id"`
	Consent     ContributionConsent          `json:"consent"`
	Diagnostics []ScanDiagnosticContribution `json:"diagnostics"`
}

type ScanDiagnosticCreateResponse struct {
	Success       bool     `json:"success"`
	DiagnosticIDs []string `json:"diagnostic_ids"`
}

type FindingFeedbackCreateRequest struct {
	FindingID  string              `json:"finding_id"`
	Verdict    string              `json:"verdict"`
	Message    string              `json:"message,omitempty"`
	Confidence *int                `json:"confidence,omitempty"`
	ActorType  string              `json:"actor_type"`
	AgentName  string              `json:"agent_name,omitempty"`
	Consent    ContributionConsent `json:"consent"`
	Evidence   map[string]any      `json:"evidence"`
}

type FindingFeedbackCreateResponse struct {
	Success    bool   `json:"success"`
	FeedbackID string `json:"feedback_id"`
	FindingID  string `json:"finding_id"`
	Verdict    string `json:"verdict"`
}

func (c Client) CreateScan(ctx context.Context, apiKey string, request ScanCreateRequest) (ScanCreateResponse, error) {
	var response ScanCreateResponse
	err := c.postJSON(ctx, "/api/scans", apiKey, request, &response)
	return response, err
}

func (c Client) CreateScanDiagnostics(ctx context.Context, apiKey string, request ScanDiagnosticCreateRequest) (ScanDiagnosticCreateResponse, error) {
	var response ScanDiagnosticCreateResponse
	err := c.postJSON(ctx, "/api/scan-diagnostics", apiKey, request, &response)
	return response, err
}

func (c Client) CreateFindingFeedback(ctx context.Context, apiKey string, ruleSlug string, request FindingFeedbackCreateRequest) (FindingFeedbackCreateResponse, error) {
	var response FindingFeedbackCreateResponse
	err := c.postJSON(ctx, "/api/rules/"+url.PathEscape(ruleSlug)+"/findings/feedback", apiKey, request, &response)
	return response, err
}

func (c Client) postJSON(ctx context.Context, endpoint string, apiKey string, requestBody any, target any) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("greprules API key is required")
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ResolveURL(endpoint), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("authorization", "Bearer "+apiKey)
	req.Header.Set("content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("POST %s failed: %s: %s", req.URL.String(), resp.Status, strings.TrimSpace(string(limited)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
