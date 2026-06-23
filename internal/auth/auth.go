package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/rules"
)

const stateSchemaVersion = "greprules.auth.v1"

type State struct {
	SchemaVersion string  `json:"schemaVersion"`
	Tokens        []Token `json:"tokens"`
}

type Token struct {
	Registry  string `json:"registry"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type startResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type pollResponse struct {
	Status    string `json:"status"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Interval  int    `json:"interval"`
}

type apiErrorResponse struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func RunCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules auth login|status|logout")
	}
	switch args[0] {
	case "login":
		return runLogin(ctx, args[1:])
	case "status":
		return runStatus(args[1:])
	case "logout":
		return runLogout(args[1:])
	default:
		return fmt.Errorf("unknown auth command: %s", args[0])
	}
}

func RequiredToken(registry string) (string, error) {
	resolved := ResolveRegistry(registry)
	state, err := loadState()
	if err != nil {
		return "", err
	}
	for _, token := range state.Tokens {
		if normalizeRegistry(token.Registry) != resolved {
			continue
		}
		if token.Token == "" {
			break
		}
		if tokenExpired(token.ExpiresAt) {
			return "", fmt.Errorf("greprules login expired for %s; run `greprules auth login`", resolved)
		}
		return token.Token, nil
	}
	return "", fmt.Errorf("greprules login required for %s; run `greprules auth login`", resolved)
}

func StoreToken(registry string, token string, expiresAt string) error {
	resolved := ResolveRegistry(registry)
	if strings.TrimSpace(token) == "" {
		return errors.New("auth token is empty")
	}
	state, err := loadState()
	if err != nil {
		return err
	}
	next := Token{
		Registry:  resolved,
		Token:     strings.TrimSpace(token),
		ExpiresAt: strings.TrimSpace(expiresAt),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	replaced := false
	for i := range state.Tokens {
		if normalizeRegistry(state.Tokens[i].Registry) == resolved {
			state.Tokens[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		state.Tokens = append(state.Tokens, next)
	}
	return saveState(state)
}

func ResolveRegistry(value string) string {
	if strings.TrimSpace(value) == "" {
		value = os.Getenv("GREPRULES_REGISTRY")
	}
	if strings.TrimSpace(value) == "" {
		value = config.DefaultRegistry
	}
	return normalizeRegistry(value)
}

func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	registryFlag := fs.String("registry", "", "greprules registry URL")
	clientName := fs.String("client-name", "greprules CLI", "client name shown in the browser approval")
	noBrowser := fs.Bool("no-browser", false, "print the approval URL without opening a browser")
	agentMode := fs.Bool("agent", false, "emit machine-readable login events for coding agents")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: greprules auth login [--registry URL] [--no-browser] [--agent]")
	}
	registry := ResolveRegistry(*registryFlag)
	if *agentMode {
		*noBrowser = true
		if strings.TrimSpace(*clientName) == "greprules CLI" {
			*clientName = "greprules agent"
		}
	}
	var start startResponse
	if _, err := postJSON(ctx, registry, "/api/cli-auth/start", map[string]string{
		"client_name": strings.TrimSpace(*clientName),
	}, &start); err != nil {
		return err
	}
	if start.Interval <= 0 {
		start.Interval = 3
	}
	if start.ExpiresIn <= 0 {
		start.ExpiresIn = 600
	}
	if *agentMode {
		if err := emitAgentEvent("approval_required", map[string]any{
			"registry":                  registry,
			"user_code":                 start.UserCode,
			"verification_uri":          start.VerificationURI,
			"verification_uri_complete": start.VerificationURIComplete,
			"expires_in":                start.ExpiresIn,
			"interval":                  start.Interval,
		}); err != nil {
			return err
		}
	} else {
		fmt.Printf("Open this URL to authorize greprules CLI:\n%s\n", start.VerificationURIComplete)
		fmt.Printf("Code: %s\n", start.UserCode)
	}
	if !*noBrowser {
		if err := openBrowser(start.VerificationURIComplete); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open browser automatically: %v\n", err)
		}
	}
	if *agentMode {
		if err := emitAgentEvent("waiting_for_approval", map[string]any{
			"registry": registry,
		}); err != nil {
			return err
		}
	} else {
		fmt.Println("Waiting for browser approval...")
	}

	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for {
		if time.Now().After(deadline) {
			return errors.New("browser login expired; run `greprules auth login` again")
		}
		var poll pollResponse
		status, err := postJSON(ctx, registry, "/api/cli-auth/poll", map[string]string{
			"device_code": start.DeviceCode,
		}, &poll)
		if err != nil {
			return err
		}
		if status == http.StatusAccepted || poll.Status == "authorization_pending" {
			if poll.Interval > 0 {
				start.Interval = poll.Interval
			}
			sleep := time.Duration(start.Interval) * time.Second
			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if poll.Token == "" {
			return errors.New("browser login did not return a CLI token")
		}
		if err := StoreToken(registry, poll.Token, poll.ExpiresAt); err != nil {
			return err
		}
		if *agentMode {
			payload := map[string]any{
				"registry": registry,
			}
			if poll.ExpiresAt != "" {
				payload["expires_at"] = poll.ExpiresAt
			}
			return emitAgentEvent("logged_in", payload)
		} else {
			fmt.Printf("Logged in to %s\n", registry)
			if poll.ExpiresAt != "" {
				fmt.Printf("Token expires at %s\n", poll.ExpiresAt)
			}
		}
		return nil
	}
}

func emitAgentEvent(event string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["event"] = event
	encoder := json.NewEncoder(os.Stdout)
	return encoder.Encode(payload)
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	registryFlag := fs.String("registry", "", "greprules registry URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: greprules auth status [--registry URL]")
	}
	registry := ResolveRegistry(*registryFlag)
	state, err := loadState()
	if err != nil {
		return err
	}
	for _, token := range state.Tokens {
		if normalizeRegistry(token.Registry) == registry && token.Token != "" && !tokenExpired(token.ExpiresAt) {
			fmt.Printf("logged in to %s\n", registry)
			if token.ExpiresAt != "" {
				fmt.Printf("expires_at=%s\n", token.ExpiresAt)
			}
			return nil
		}
	}
	return fmt.Errorf("not logged in to %s; run `greprules auth login`", registry)
}

func runLogout(args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	registryFlag := fs.String("registry", "", "greprules registry URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: greprules auth logout [--registry URL]")
	}
	registry := ResolveRegistry(*registryFlag)
	state, err := loadState()
	if err != nil {
		return err
	}
	filtered := state.Tokens[:0]
	for _, token := range state.Tokens {
		if normalizeRegistry(token.Registry) != registry {
			filtered = append(filtered, token)
		}
	}
	state.Tokens = filtered
	if err := saveState(state); err != nil {
		return err
	}
	fmt.Printf("logged out of %s\n", registry)
	return nil
}

func postJSON(ctx context.Context, registry string, endpoint string, requestBody any, target any) (int, error) {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	registryClient := rules.NewRegistry(registry)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registryClient.ResolveURL(endpoint), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(limited))
		var apiError apiErrorResponse
		if json.Unmarshal(limited, &apiError) == nil && len(apiError.Errors) > 0 {
			message = apiError.Errors[0].Message
		}
		return resp.StatusCode, fmt.Errorf("POST %s failed: %s: %s", req.URL.String(), resp.Status, message)
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(target)
}

func loadState() (State, error) {
	path, err := statePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{SchemaVersion: stateSchemaVersion}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.SchemaVersion == "" {
		state.SchemaVersion = stateSchemaVersion
	}
	return state, nil
}

func saveState(state State) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if state.SchemaVersion == "" {
		state.SchemaVersion = stateSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func statePath() (string, error) {
	root, err := config.UserStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "auth.json"), nil
}

func normalizeRegistry(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = config.DefaultRegistry
	}
	return strings.TrimRight(value, "/")
}

func tokenExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return true
	}
	return !t.After(time.Now().UTC())
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
