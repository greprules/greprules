package agent

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeOutputComponent = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func defaultAgentRunOutputDir(label string) string {
	return filepath.Join(
		".greprules",
		"plugin-data",
		agentProviderFromEnv(),
		"sessions",
		agentSessionFromEnv(),
		"runs",
		newAgentRunID(label),
	)
}

func agentProviderFromEnv() string {
	if value := sanitizedEnv("GREPRULES_AGENT_PROVIDER"); value != "" {
		return value
	}
	switch {
	case anyEnvSet("CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_HOME", "CODEX_SHELL", "CODEX_PROJECT_DIR"):
		return "codex"
	case anyEnvSet("CLAUDE_SESSION_ID", "CLAUDE_CONFIG_DIR", "CLAUDE_PROJECT_DIR", "CLAUDE_PLUGIN_ROOT"):
		return "claude-code"
	case anyEnvSet("HERMES_SESSION_ID", "HERMES_TASK_ID", "HERMES_HOME"):
		return "hermes"
	default:
		return "agent"
	}
}

func agentSessionFromEnv() string {
	for _, key := range []string{
		"GREPRULES_AGENT_SESSION_ID",
		"CODEX_THREAD_ID",
		"CODEX_SESSION_ID",
		"CODEX_CONVERSATION_ID",
		"CLAUDE_SESSION_ID",
		"CLAUDECODE_SESSION_ID",
		"HERMES_TASK_ID",
		"HERMES_SESSION_ID",
	} {
		if value := sanitizedEnv(key); value != "" {
			return value
		}
	}
	return "manual"
}

func newAgentRunID(label string) string {
	parts := []string{time.Now().UTC().Format("20060102T150405Z")}
	if safeLabel := sanitizeOutputComponent(label); safeLabel != "" {
		parts = append(parts, safeLabel)
	}
	parts = append(parts, randomHex(6))
	return strings.Join(parts, "-")
}

func sanitizedEnv(key string) string {
	return sanitizeOutputComponent(os.Getenv(key))
}

func sanitizeOutputComponent(value string) string {
	safe := unsafeOutputComponent.ReplaceAllString(strings.TrimSpace(value), "-")
	return strings.Trim(safe, ".-")
}

func anyEnvSet(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "norand"
	}
	return hex.EncodeToString(buf)
}
