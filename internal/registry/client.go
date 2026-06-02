package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type PackSummary struct {
	Slug             string         `json:"slug"`
	Name             string         `json:"name"`
	Summary          string         `json:"summary"`
	SourceType       string         `json:"source_type"`
	Criteria         map[string]any `json:"criteria"`
	TotalRules       int            `json:"total_rules"`
	DownloadCount    int            `json:"download_count"`
	LastCommit       string         `json:"last_commit"`
	Languages        []string       `json:"languages"`
	ValidationStatus string         `json:"validation_status"`
	ManifestURL      string         `json:"manifest_url"`
	DownloadURL      string         `json:"download_url"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

type PackManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Slug          string         `json:"slug"`
	Name          string         `json:"name"`
	Summary       string         `json:"summary"`
	SourceType    string         `json:"source_type"`
	GeneratedAt   string         `json:"generated_at"`
	BuildID       string         `json:"build_id"`
	TotalRules    int            `json:"total_rules"`
	Languages     []string       `json:"languages"`
	Criteria      map[string]any `json:"criteria"`
	Rules         []ManifestRule `json:"rules"`
}

type ManifestRule struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	RuleID           string   `json:"rule_id"`
	CanonicalRuleIDs []string `json:"canonical_rule_ids"`
	OriginalRuleID   string   `json:"original_rule_id"`
	RuleNamespace    string   `json:"rule_namespace"`
	YAMLPath         string   `json:"yaml_path"`
	Language         string   `json:"language"`
	Framework        string   `json:"framework"`
	Severity         string   `json:"severity"`
	Confidence       string   `json:"confidence"`
	License          string   `json:"license"`
	CVE              []string `json:"cve"`
	CWE              []string `json:"cwe"`
	Tags             []string `json:"tags"`
	SourceRepo       string   `json:"source_repo"`
	SourceCommit     string   `json:"source_commit"`
	Version          string   `json:"version"`
}

func New(baseURL string) Client {
	if baseURL == "" {
		baseURL = "https://greprules.io"
	}
	return Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c Client) ListPacks(ctx context.Context) ([]PackSummary, error) {
	var response struct {
		Success bool          `json:"success"`
		Packs   []PackSummary `json:"packs"`
	}
	if err := c.getJSON(ctx, "/api/packs", &response); err != nil {
		return nil, err
	}
	return response.Packs, nil
}

func (c Client) FetchManifest(ctx context.Context, slug string) ([]byte, PackManifest, error) {
	body, err := c.getBytes(ctx, "/api/packs/"+url.PathEscape(slug)+"/manifest.json")
	if err != nil {
		return nil, PackManifest{}, err
	}
	var manifest PackManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, PackManifest{}, err
	}
	return body, manifest, nil
}

func (c Client) DownloadPack(ctx context.Context, slug string) ([]byte, error) {
	return c.getBytes(ctx, "/api/packs/"+url.PathEscape(slug)+"/latest.tar.gz")
}

func (c Client) ResolveURL(value string) string {
	if value == "" {
		return c.BaseURL
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return c.BaseURL + "/" + strings.TrimLeft(value, "/")
	}
	if strings.HasPrefix(value, "/") {
		base.Path = value
		base.RawQuery = ""
		return base.String()
	}
	base.Path = path.Join(base.Path, value)
	base.RawQuery = ""
	return base.String()
}

func (c Client) getJSON(ctx context.Context, endpoint string, target any) error {
	body, err := c.getBytes(ctx, endpoint)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func (c Client) getBytes(ctx context.Context, endpoint string) ([]byte, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ResolveURL(endpoint), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json, application/gzip, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET %s failed: %s: %s", req.URL.String(), resp.Status, strings.TrimSpace(string(limited)))
	}
	return io.ReadAll(resp.Body)
}
