package agent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/rules"
	"github.com/greprules/greprules/internal/utils"
)

const feedbackBundleSchemaVersion = "greprules.feedback.bundle.v1"

type FeedbackBundle struct {
	SchemaVersion string                             `json:"schemaVersion"`
	GeneratedAt   string                             `json:"generatedAt"`
	ResultHash    string                             `json:"resultHash"`
	Scan          rules.ScanContribution             `json:"scan"`
	Findings      []rules.ScanFindingContribution    `json:"findings"`
	Diagnostics   []rules.ScanDiagnosticContribution `json:"diagnostics"`
	Feedback      []PreparedFindingFeedback          `json:"feedback,omitempty"`
}

type PreparedFindingFeedback struct {
	RuleSlug           string `json:"rule_slug"`
	RuleVersion        string `json:"rule_version"`
	FindingFingerprint string `json:"finding_fingerprint"`
	Verdict            string `json:"verdict"`
	Message            string `json:"message,omitempty"`
	Confidence         *int   `json:"confidence,omitempty"`
}

type feedbackPrepareSummary struct {
	BundlePath  string `json:"bundlePath"`
	Findings    int    `json:"findings"`
	Diagnostics int    `json:"diagnostics"`
}

type feedbackSubmitSummary struct {
	ScanID            string `json:"scanId"`
	Findings          int    `json:"findings"`
	Diagnostics       int    `json:"diagnostics"`
	FeedbackSubmitted int    `json:"feedbackSubmitted"`
}

type manifestRuleRef struct {
	Slug      string
	RuleID    string
	Version   string
	Language  string
	Framework string
}

func RunFeedbackCommand(ctx context.Context, args []string, version string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-feedback prepare|submit")
	}
	switch args[0] {
	case "prepare":
		return runFeedbackPrepare(args[1:], version)
	case "submit":
		return runFeedbackSubmit(ctx, args[1:])
	default:
		return fmt.Errorf("unknown agent-feedback command: %s", args[0])
	}
}

func runFeedbackPrepare(args []string, version string) error {
	fs := flag.NewFlagSet("agent-feedback prepare", flag.ContinueOnError)
	resultPath := fs.String("result", "", "agent-result.json path")
	outPath := fs.String("out", "", "feedback bundle output path")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*resultPath) == "" {
		return errors.New("usage: greprules agent-feedback prepare --result <agent-result.json> [--out feedback-bundle.json]")
	}
	bundle, err := BuildFeedbackBundle(*resultPath, version)
	if err != nil {
		return err
	}
	outputPath := *outPath
	if outputPath == "" {
		outputPath = filepath.Join(filepath.Dir(*resultPath), "feedback-bundle.json")
	}
	if err := writeJSONFile(outputPath, bundle); err != nil {
		return err
	}
	summary := feedbackPrepareSummary{
		BundlePath:  outputPath,
		Findings:    len(bundle.Findings),
		Diagnostics: len(bundle.Diagnostics),
	}
	if *format == "json" {
		return cmdutil.PrintJSON(summary)
	}
	if *format != "text" && *format != "" {
		return fmt.Errorf("unknown output format: %s", *format)
	}
	fmt.Printf("prepared feedback bundle: %s\n", outputPath)
	fmt.Printf("findings=%d diagnostics=%d feedback=%d\n", len(bundle.Findings), len(bundle.Diagnostics), len(bundle.Feedback))
	if len(bundle.Findings) > 0 {
		fmt.Println("Uploaded after approval: rule slug, rule version, finding fingerprint, hashed paths/messages, verdicts, and diagnostic hashes.")
		fmt.Println("Not uploaded: source code, raw file paths, private repository URLs, or code snippets.")
		fmt.Println("Add feedback entries to the bundle, get explicit user approval, then run agent-feedback submit.")
	}
	return nil
}

func runFeedbackSubmit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-feedback submit", flag.ContinueOnError)
	bundlePath := fs.String("bundle", "", "feedback bundle path")
	consentSession := fs.String("consent-session", "", "explicit user approval session id")
	registryFlag := fs.String("registry", "", "greprules registry URL")
	apiKeyFlag := fs.String("api-key", "", "greprules API key")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*bundlePath) == "" || strings.TrimSpace(*consentSession) == "" {
		return errors.New("usage: greprules agent-feedback submit --bundle <feedback-bundle.json> --consent-session <id>")
	}
	apiKey := strings.TrimSpace(*apiKeyFlag)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GREPRULES_API_KEY"))
	}
	if apiKey == "" {
		return errors.New("GREPRULES_API_KEY or --api-key is required")
	}
	bundle, err := readFeedbackBundle(*bundlePath)
	if err != nil {
		return err
	}
	if bundle.SchemaVersion != feedbackBundleSchemaVersion {
		return fmt.Errorf("unsupported feedback bundle schema: %s", bundle.SchemaVersion)
	}
	registry := strings.TrimSpace(*registryFlag)
	if registry == "" {
		registry = strings.TrimSpace(os.Getenv("GREPRULES_REGISTRY"))
	}
	if registry == "" {
		registry = config.DefaultRegistry
	}
	consent := rules.ContributionConsent{
		Mode:            "explicit_user_approval",
		SessionID:       *consentSession,
		ApprovedAt:      time.Now().UTC().Format(time.RFC3339),
		Scope:           consentScope(bundle),
		RedactionPolicy: "no_source_code",
	}
	client := rules.NewRegistry(registry)
	scanResponse, err := client.CreateScan(ctx, apiKey, rules.ScanCreateRequest{
		Consent:  consent,
		Scan:     bundle.Scan,
		Findings: bundle.Findings,
	})
	if err != nil {
		return err
	}
	findingIDs := map[string]string{}
	for _, finding := range scanResponse.Findings {
		findingIDs[findingKey(finding.RuleSlug, finding.RuleVersion, finding.FindingFingerprint)] = finding.ID
	}
	if len(bundle.Diagnostics) > 0 {
		if _, err := client.CreateScanDiagnostics(ctx, apiKey, rules.ScanDiagnosticCreateRequest{
			ScanID:      scanResponse.ScanID,
			Consent:     consent,
			Diagnostics: bundle.Diagnostics,
		}); err != nil {
			return err
		}
	}
	submitted := 0
	for _, feedback := range bundle.Feedback {
		findingID := findingIDs[findingKey(feedback.RuleSlug, feedback.RuleVersion, feedback.FindingFingerprint)]
		if findingID == "" {
			return fmt.Errorf("feedback references unknown finding: %s", feedback.FindingFingerprint)
		}
		if _, err := client.CreateFindingFeedback(ctx, apiKey, feedback.RuleSlug, rules.FindingFeedbackCreateRequest{
			FindingID:  findingID,
			Verdict:    feedback.Verdict,
			Message:    feedback.Message,
			Confidence: feedback.Confidence,
			ActorType:  "agent",
			AgentName:  "greprules-agent",
			Consent:    consent,
			Evidence: map[string]any{
				"bundle_schema": bundle.SchemaVersion,
				"result_hash":   bundle.ResultHash,
			},
		}); err != nil {
			return err
		}
		submitted++
	}
	summary := feedbackSubmitSummary{
		ScanID:            scanResponse.ScanID,
		Findings:          len(scanResponse.Findings),
		Diagnostics:       len(bundle.Diagnostics),
		FeedbackSubmitted: submitted,
	}
	if *format == "json" {
		return cmdutil.PrintJSON(summary)
	}
	if *format != "text" && *format != "" {
		return fmt.Errorf("unknown output format: %s", *format)
	}
	fmt.Printf("submitted scan feedback: scan=%s findings=%d diagnostics=%d feedback=%d\n",
		summary.ScanID,
		summary.Findings,
		summary.Diagnostics,
		summary.FeedbackSubmitted,
	)
	return nil
}

func BuildFeedbackBundle(resultPath string, version string) (FeedbackBundle, error) {
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		return FeedbackBundle{}, err
	}
	var result AgentResult
	if err := json.Unmarshal(resultData, &result); err != nil {
		return FeedbackBundle{}, err
	}
	root := result.Repo.Root
	if root == "" {
		root = filepath.Dir(resultPath)
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		return FeedbackBundle{}, err
	}
	ruleRefs, err := loadManifestRuleRefs(root, lock)
	if err != nil {
		return FeedbackBundle{}, err
	}
	findings := make([]rules.ScanFindingContribution, 0, len(result.Findings))
	for _, finding := range result.Findings {
		ref := ruleRefs[finding.RuleID]
		if ref.Slug == "" {
			ref = manifestRuleRef{Slug: safeRuleSlug(finding.RuleID), RuleID: finding.RuleID, Version: "unknown"}
		}
		findings = append(findings, rules.ScanFindingContribution{
			RuleSlug:           ref.Slug,
			OpenGrepRuleID:     finding.RuleID,
			RuleVersion:        fallbackString(ref.Version, "unknown"),
			FindingFingerprint: findingFingerprint(ref.Slug, ref.Version, finding),
			Severity:           normalizeFeedbackSeverity(finding.Severity),
			PathHash:           sha256Field(finding.Path),
			MessageHash:        sha256Field(finding.Message),
			LineStart:          finding.Start.Line,
			LineEnd:            finding.End.Line,
			Metadata:           map[string]any{},
		})
	}
	diagnostics := make([]rules.ScanDiagnosticContribution, 0, len(result.Warnings)+len(result.Errors))
	for _, warning := range result.Warnings {
		diagnostics = append(diagnostics, diagnosticContribution("scan_warning", "warning", warning))
	}
	for _, scanError := range result.Errors {
		diagnostics = append(diagnostics, diagnosticContribution("scan_error", "error", scanError))
	}
	sort.Slice(findings, func(i, j int) bool {
		return findingKey(findings[i].RuleSlug, findings[i].RuleVersion, findings[i].FindingFingerprint) <
			findingKey(findings[j].RuleSlug, findings[j].RuleVersion, findings[j].FindingFingerprint)
	})
	resultHash := sha256Field(string(resultData))
	return FeedbackBundle{
		SchemaVersion: feedbackBundleSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		ResultHash:    resultHash,
		Scan: rules.ScanContribution{
			Source:           feedbackSource(),
			GreprulesVersion: fallbackString(version, "dev"),
			OpenGrepVersion:  fallbackString(result.Engine.Version, "unknown"),
			SubmissionHash:   resultHash,
			ProjectHash:      sha256Field(root),
			Languages:        uniquePackLanguages(lock),
			Frameworks:       []string{},
			RulePackSlugs:    packIDs(lock),
			PrivacyMode:      "hashes_only",
		},
		Findings:    findings,
		Diagnostics: diagnostics,
		Feedback:    []PreparedFindingFeedback{},
	}, nil
}

func loadManifestRuleRefs(root string, lock config.Lock) (map[string]manifestRuleRef, error) {
	refs := map[string]manifestRuleRef{}
	for _, pack := range lock.Packs {
		manifestPath := pack.ManifestPath
		if !filepath.IsAbs(manifestPath) {
			manifestPath = filepath.Join(root, manifestPath)
		}
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, err
		}
		var manifest rules.PackManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		for _, rule := range manifest.Rules {
			version := fallbackString(rule.Version, manifest.BuildID)
			ref := manifestRuleRef{
				Slug:      rule.Slug,
				RuleID:    rule.RuleID,
				Version:   fallbackString(version, "unknown"),
				Language:  rule.Language,
				Framework: rule.Framework,
			}
			for _, key := range manifestRuleKeys(rule) {
				if key != "" {
					refs[key] = ref
				}
			}
		}
	}
	return refs, nil
}

func manifestRuleKeys(rule rules.ManifestRule) []string {
	keys := []string{rule.RuleID, rule.OriginalRuleID, rule.Slug}
	keys = append(keys, rule.CanonicalRuleIDs...)
	return keys
}

func diagnosticContribution(kind string, severity string, value string) rules.ScanDiagnosticContribution {
	return rules.ScanDiagnosticContribution{
		DiagnosticFingerprint: diagnosticFingerprint(kind, severity, value),
		Kind:                  kind,
		Severity:              severity,
		MessageHash:           sha256Field(value),
		Count:                 1,
		Details: map[string]any{
			"source": "agent-result",
		},
	}
}

func consentScope(bundle FeedbackBundle) []string {
	scope := []string{"scan_session"}
	if len(bundle.Findings) > 0 {
		scope = append(scope, "scan_findings")
	}
	if len(bundle.Diagnostics) > 0 {
		scope = append(scope, "scan_diagnostics")
	}
	if len(bundle.Feedback) > 0 {
		scope = append(scope, "finding_feedback")
	}
	return scope
}

func readFeedbackBundle(path string) (FeedbackBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FeedbackBundle{}, err
	}
	var bundle FeedbackBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return FeedbackBundle{}, err
	}
	return bundle, nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sha256Field(value string) string {
	return "sha256:" + utils.SHA256Bytes([]byte(value))
}

func findingFingerprint(ruleSlug string, ruleVersion string, finding Finding) string {
	return sha256Field(strings.Join([]string{
		ruleSlug,
		ruleVersion,
		finding.RuleID,
		finding.Path,
		fmt.Sprint(finding.Start.Line),
		fmt.Sprint(finding.Start.Col),
		fmt.Sprint(finding.End.Line),
		fmt.Sprint(finding.End.Col),
		finding.Message,
	}, "\x00"))
}

func diagnosticFingerprint(kind string, severity string, value string) string {
	return sha256Field(strings.Join([]string{kind, severity, value}, "\x00"))
}

func findingKey(ruleSlug string, ruleVersion string, fingerprint string) string {
	return ruleSlug + "\x00" + ruleVersion + "\x00" + fingerprint
}

func feedbackSource() string {
	switch agentProviderFromEnv() {
	case "codex":
		return "codex"
	case "claude-code", "claude":
		return "claude"
	case "hermes":
		return "hermes"
	default:
		return "unknown"
	}
}

func uniquePackLanguages(lock config.Lock) []string {
	seen := map[string]bool{}
	var languages []string
	for _, pack := range lock.Packs {
		for _, language := range pack.Languages {
			normalized := strings.TrimSpace(strings.ToLower(language))
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			languages = append(languages, normalized)
		}
	}
	sort.Strings(languages)
	return languages
}

func packIDs(lock config.Lock) []string {
	ids := make([]string, 0, len(lock.Packs))
	for _, pack := range lock.Packs {
		ids = append(ids, pack.ID)
	}
	sort.Strings(ids)
	return ids
}

func normalizeFeedbackSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return "critical"
	case "high", "error":
		return "high"
	case "medium", "warning", "warn":
		return "medium"
	case "low":
		return "low"
	case "info", "informational":
		return "info"
	default:
		return ""
	}
}

func safeRuleSlug(value string) string {
	slug := strings.ToLower(value)
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, slug)
	slug = strings.Trim(slug, "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		return "unknown-rule"
	}
	return slug
}
