package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

func TestCLI_T3AuthStatus_JSONAndText(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	tokenPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")
	_ = t3.SaveMCPOAuthToken(tokenPath, &t3.MCPOAuthToken{
		AccessToken: "test-token-val",
		Origin:      "http://127.0.0.1:45678",
		IssuedAt:    time.Now().Add(-1 * time.Hour),
		ExpiresAt:   time.Now().Add(25 * 24 * time.Hour),
		Scope:       "orchestration:read orchestration:operate",
	})

	// 1. JSON format
	bufJSON := new(bytes.Buffer)
	rootCmd.SetOut(bufJSON)
	rootCmd.SetErr(bufJSON)
	rootCmd.SetArgs([]string{"t3", "auth", "status", "--format", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("t3 auth status json failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(bufJSON.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json output: %v, raw: %s", err, bufJSON.String())
	}

	oauthMap, ok := resp["mcp_oauth"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcp_oauth object in response")
	}
	if oauthMap["status"] != "valid" {
		t.Errorf("expected mcp_oauth status 'valid', got '%v'", oauthMap["status"])
	}
	if oauthMap["scope"] != "orchestration:read orchestration:operate" {
		t.Errorf("expected correct scope, got '%v'", oauthMap["scope"])
	}

	// 2. Text format
	bufText := new(bytes.Buffer)
	rootCmd.SetOut(bufText)
	rootCmd.SetErr(bufText)
	rootCmd.SetArgs([]string{"t3", "auth", "status", "--format", "text"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("t3 auth status text failed: %v", err)
	}

	out := bufText.String()
	if !strings.Contains(out, "T3 Code Authentication Status") {
		t.Errorf("expected text output to contain header, got: %s", out)
	}
	if !strings.Contains(out, "VALID") {
		t.Errorf("expected text output to show VALID, got: %s", out)
	}
}

func TestCLI_T3AuthExpiryWarning_InStatusAndContextInject(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Save token expiring in 24 hours (< 72h)
	tokenPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")
	_ = t3.SaveMCPOAuthToken(tokenPath, &t3.MCPOAuthToken{
		AccessToken: "soon-expiring-token",
		IssuedAt:    time.Now().Add(-29 * 24 * time.Hour),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		Scope:       "orchestration:read orchestration:operate",
	})

	// Add a T3Code task to state
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("my-project", "/tmp/my-project", "my-project", "my-project", state.TaskState{
			TaskSlug:     "my-task",
			Engine:       "t3code",
			WindowID:     "thread_exp_123",
			AgentBlockID: "thread_exp_123",
			Status:       "running",
		})
		return nil
	})

	// Save config with t3code as worker backend
	cfg := config.DefaultConfig()
	cfg.WorkerBackend = "t3code"
	_ = config.SaveConfig(cfg)

	// 1. Check buddy-ctl status JSON
	bufStatus := new(bytes.Buffer)
	rootCmd.SetOut(bufStatus)
	rootCmd.SetErr(bufStatus)
	rootCmd.SetArgs([]string{"status", "--format", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	var statusResp map[string]any
	if err := json.Unmarshal(bufStatus.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to decode status json: %v", err)
	}

	warn, ok := statusResp["warning"].(string)
	if !ok || !strings.Contains(warn, "expires in") {
		t.Errorf("expected 72h warning in status json, got: %v", statusResp["warning"])
	}

	// 2. Check buddy-ctl context-inject
	bufInject := new(bytes.Buffer)
	rootCmd.SetOut(bufInject)
	rootCmd.SetErr(bufInject)
	rootCmd.SetArgs([]string{"context-inject"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("context-inject failed: %v", err)
	}

	var injectResp map[string]any
	if err := json.Unmarshal(bufInject.Bytes(), &injectResp); err != nil {
		t.Fatalf("failed to decode context-inject json: %v", err)
	}

	steps, ok := injectResp["injectSteps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("missing injectSteps in context-inject response")
	}
	step0 := steps[0].(map[string]any)
	eMsg, _ := step0["ephemeralMessage"].(string)

	if !strings.Contains(eMsg, "T3 Code OAuth Warning") || !strings.Contains(eMsg, "expires in") {
		t.Errorf("expected warning in ephemeralMessage, got: %s", eMsg)
	}
}

func TestCLI_T3AuthLogin_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/mcp/register":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(t3.ClientRegistrationResponse{ClientID: "test_cid_login"})
		case "/oauth/mcp/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(t3.TokenExchangeResponse{
				AccessToken: "issued_token_from_login_cmd",
				TokenType:   "Bearer",
				ExpiresIn:   2592000,
				Scope:       "orchestration:read orchestration:operate",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BUDDY_T3_SERVER_URL", server.URL)

	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	defer rPipe.Close()

	go func() {
		scanner := bufio.NewScanner(rPipe)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "/oauth/mcp/authorize?") {
				parsed, err := url.Parse(line)
				if err == nil {
					redirectURI := parsed.Query().Get("redirect_uri")
					stateVal := parsed.Query().Get("state")
					if redirectURI != "" {
						cbURL := fmt.Sprintf("%s?code=auth_code_777&state=%s", redirectURI, stateVal)
						time.Sleep(20 * time.Millisecond)
						resp, err := http.Get(cbURL)
						if err == nil {
							_ = resp.Body.Close()
						}
					}
				}
			}
		}
	}()

	bufOut := new(bytes.Buffer)
	mw := io.MultiWriter(bufOut, wPipe)
	rootCmd.SetOut(mw)
	rootCmd.SetErr(mw)
	rootCmd.SetArgs([]string{"t3", "auth", "login", "--no-browser"})

	execErr := rootCmd.Execute()
	_ = wPipe.Close()

	if execErr != nil {
		t.Fatalf("t3 auth login failed: %v", execErr)
	}

	outStr := bufOut.String()
	if !strings.Contains(outStr, "Successfully authenticated with T3 Code") {
		t.Errorf("expected success message, got: %s", outStr)
	}

	savedPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")
	tok, err := t3.LoadMCPOAuthToken(savedPath)
	if err != nil {
		t.Fatalf("failed to load saved oauth token: %v", err)
	}
	if tok.AccessToken != "issued_token_from_login_cmd" {
		t.Errorf("expected token 'issued_token_from_login_cmd', got '%s'", tok.AccessToken)
	}
}
