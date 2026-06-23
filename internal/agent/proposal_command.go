package agent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/auth"
	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/rules"
)

const ruleProposalBundleSchemaVersion = "greprules.rule_proposal.bundle.v1"

type RuleProposalBundle struct {
	SchemaVersion string                  `json:"schemaVersion"`
	GeneratedAt   string                  `json:"generatedAt"`
	Proposal      rules.RuleUploadRequest `json:"proposal"`
}

type proposalPrepareSummary struct {
	BundlePath string `json:"bundlePath"`
}

type proposalSubmitSummary struct {
	Slug             string `json:"slug"`
	Version          string `json:"version"`
	ValidationStatus string `json:"validationStatus"`
}

func RunProposalCommand(ctx context.Context, args []string, version string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-proposal prepare|submit")
	}
	switch args[0] {
	case "prepare":
		return runProposalPrepare(args[1:])
	case "submit":
		return runProposalSubmit(ctx, args[1:])
	default:
		return fmt.Errorf("unknown agent-proposal command: %s", args[0])
	}
}

func runProposalPrepare(args []string) error {
	fs := flag.NewFlagSet("agent-proposal prepare", flag.ContinueOnError)
	outPath := fs.String("out", "rule-proposal-bundle.json", "rule proposal bundle output path")
	title := fs.String("title", "Agent-generated rule proposal", "proposal title")
	ruleID := fs.String("rule-id", "agent.generated.rule", "OpenGrep rule id")
	language := fs.String("language", "generic", "rule language")
	license := fs.String("license", "Apache-2.0", "rule license")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bundle := NewRuleProposalTemplate(*title, *ruleID, *language, *license)
	if err := writeJSONFile(*outPath, bundle); err != nil {
		return err
	}
	summary := proposalPrepareSummary{BundlePath: *outPath}
	if *format == "json" {
		return cmdutil.PrintJSON(summary)
	}
	if *format != "text" && *format != "" {
		return fmt.Errorf("unknown output format: %s", *format)
	}
	fmt.Printf("prepared rule proposal bundle: %s\n", *outPath)
	fmt.Println("Edit TODO fields, keep one positive and one negative test fixture, get explicit user approval, then run agent-proposal submit.")
	fmt.Println("Uploaded after approval: rule YAML, license, provenance, generated metadata, test fixtures, and consent metadata.")
	return nil
}

func runProposalSubmit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-proposal submit", flag.ContinueOnError)
	bundlePath := fs.String("bundle", "", "rule proposal bundle path")
	consentSession := fs.String("consent-session", "", "explicit user approval session id")
	registryFlag := fs.String("registry", "", "greprules registry URL")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*bundlePath) == "" || strings.TrimSpace(*consentSession) == "" {
		return errors.New("usage: greprules agent-proposal submit --bundle <rule-proposal-bundle.json> --consent-session <id>")
	}
	bundle, err := readRuleProposalBundle(*bundlePath)
	if err != nil {
		return err
	}
	if bundle.SchemaVersion != ruleProposalBundleSchemaVersion {
		return fmt.Errorf("unsupported rule proposal bundle schema: %s", bundle.SchemaVersion)
	}
	consent := rules.ContributionConsent{
		Mode:            "explicit_user_approval",
		SessionID:       *consentSession,
		ApprovedAt:      time.Now().UTC().Format(time.RFC3339),
		Scope:           []string{"rule_proposal"},
		RedactionPolicy: "no_source_code",
	}
	bundle.Proposal.Metadata.AgentConsent = &consent
	if err := validateRuleProposalBundle(bundle); err != nil {
		return err
	}
	registry := auth.ResolveRegistry(*registryFlag)
	authToken, err := auth.RequiredToken(registry)
	if err != nil {
		return err
	}
	client := rules.NewRegistry(registry)
	response, err := client.CreateRule(ctx, authToken, bundle.Proposal)
	if err != nil {
		return err
	}
	summary := proposalSubmitSummary{
		Slug:             response.Rule.Slug,
		Version:          response.Version.Version,
		ValidationStatus: fallbackString(response.Rule.ValidationStatus, response.Validation.Status),
	}
	if *format == "json" {
		return cmdutil.PrintJSON(summary)
	}
	if *format != "text" && *format != "" {
		return fmt.Errorf("unknown output format: %s", *format)
	}
	fmt.Printf("submitted rule proposal: slug=%s version=%s validation=%s\n",
		summary.Slug,
		summary.Version,
		summary.ValidationStatus,
	)
	return nil
}

func NewRuleProposalTemplate(title string, ruleID string, language string, license string) RuleProposalBundle {
	now := time.Now().UTC().Format(time.RFC3339)
	trimmedRuleID := fallbackString(strings.TrimSpace(ruleID), "agent.generated.rule")
	trimmedLanguage := fallbackString(strings.TrimSpace(language), "generic")
	return RuleProposalBundle{
		SchemaVersion: ruleProposalBundleSchemaVersion,
		GeneratedAt:   now,
		Proposal: rules.RuleUploadRequest{
			Title:         fallbackString(strings.TrimSpace(title), "Agent-generated rule proposal"),
			RuleID:        trimmedRuleID,
			RuleNamespace: "community",
			Description:   "TODO: summarize the vulnerability this rule detects.",
			YAML:          proposalTemplateYAML(trimmedRuleID, trimmedLanguage),
			Metadata: rules.RuleUploadMetadata{
				License:            fallbackString(strings.TrimSpace(license), "Apache-2.0"),
				Language:           trimmedLanguage,
				Severity:           "medium",
				Confidence:         "medium",
				CWE:                []string{},
				CVE:                []string{},
				GHSA:               []string{},
				Tags:               []string{"agent-generated"},
				References:         []string{},
				SourceType:         "agent_generated",
				SourceContext:      "agent_context",
				RuleNamespace:      "community",
				Source:             "TODO: describe the approved agent analysis source.",
				GeneratedBy:        "greprules-agent",
				GeneratedAt:        now,
				Version:            "1.0.0",
				VulnerablePattern:  "TODO: describe the vulnerable code pattern.",
				RecommendedFix:     "TODO: describe the recommended fix.",
				FalsePositiveNotes: "TODO: describe expected safe contexts or tuning notes.",
				Tests: []rules.RuleUploadTestFixture{
					{
						Name:     "positive detects vulnerable pattern",
						Kind:     "positive",
						Language: trimmedLanguage,
						Code:     "TODO: public minimal vulnerable fixture",
						Expected: "Rule should report one finding.",
					},
					{
						Name:     "negative ignores safe pattern",
						Kind:     "negative",
						Language: trimmedLanguage,
						Code:     "TODO: public minimal safe fixture",
						Expected: "Rule should report no findings.",
					},
				},
			},
		},
	}
}

func proposalTemplateYAML(ruleID string, language string) string {
	return fmt.Sprintf(`rules:
  - id: %s
    languages: [%s]
    severity: WARNING
    message: TODO: explain the detected vulnerability.
    pattern: TODO_REPLACE_ME($X)
`, ruleID, language)
}

func readRuleProposalBundle(path string) (RuleProposalBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuleProposalBundle{}, err
	}
	var bundle RuleProposalBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return RuleProposalBundle{}, err
	}
	return bundle, nil
}

func validateRuleProposalBundle(bundle RuleProposalBundle) error {
	proposal := bundle.Proposal
	var missing []string
	if strings.TrimSpace(proposal.Title) == "" {
		missing = append(missing, "proposal.title")
	}
	if strings.TrimSpace(proposal.YAML) == "" {
		missing = append(missing, "proposal.yaml")
	}
	if strings.TrimSpace(proposal.Metadata.License) == "" {
		missing = append(missing, "proposal.metadata.license")
	}
	if strings.TrimSpace(proposal.Metadata.Source) == "" {
		missing = append(missing, "proposal.metadata.source")
	}
	if strings.TrimSpace(proposal.Metadata.GeneratedBy) == "" {
		missing = append(missing, "proposal.metadata.generated_by")
	}
	if strings.TrimSpace(proposal.Metadata.GeneratedAt) == "" {
		missing = append(missing, "proposal.metadata.generated_at")
	}
	if strings.TrimSpace(proposal.Metadata.VulnerablePattern) == "" {
		missing = append(missing, "proposal.metadata.vulnerable_pattern")
	}
	if strings.TrimSpace(proposal.Metadata.RecommendedFix) == "" {
		missing = append(missing, "proposal.metadata.recommended_fix")
	}
	if proposal.Metadata.AgentConsent == nil {
		missing = append(missing, "proposal.metadata.agent_consent")
	}
	if !hasRuleProposalTest(proposal.Metadata.Tests, "positive") {
		missing = append(missing, "proposal.metadata.tests[positive]")
	}
	if !hasRuleProposalTest(proposal.Metadata.Tests, "negative") {
		missing = append(missing, "proposal.metadata.tests[negative]")
	}
	if len(missing) > 0 {
		return fmt.Errorf("rule proposal bundle missing required fields: %s", strings.Join(missing, ", "))
	}
	if containsProposalPlaceholder(proposal) {
		return errors.New("rule proposal bundle still contains TODO placeholders")
	}
	return nil
}

func hasRuleProposalTest(tests []rules.RuleUploadTestFixture, kind string) bool {
	for _, test := range tests {
		if test.Kind == kind && strings.TrimSpace(test.Code) != "" {
			return true
		}
	}
	return false
}

func containsProposalPlaceholder(proposal rules.RuleUploadRequest) bool {
	values := []string{
		proposal.Title,
		proposal.Description,
		proposal.YAML,
		proposal.Metadata.Source,
		proposal.Metadata.VulnerablePattern,
		proposal.Metadata.RecommendedFix,
		proposal.Metadata.FalsePositiveNotes,
	}
	for _, test := range proposal.Metadata.Tests {
		values = append(values, test.Name, test.Code, test.Expected)
	}
	for _, value := range values {
		if strings.Contains(strings.ToUpper(value), "TODO") {
			return true
		}
	}
	return false
}
