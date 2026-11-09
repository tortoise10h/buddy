package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPKCE_Generation(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	if len(pkce.CodeVerifier) < 43 {
		t.Errorf("expected verifier length >= 43, got %d", len(pkce.CodeVerifier))
	}
	if pkce.CodeChallenge == "" {
		t.Errorf("expected non-empty challenge")
	}
	if pkce.Method != "S256" {
		t.Errorf("expected method 'S256', got %s", pkce.Method)
	}
}

func TestOAuth_ClientRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/mcp/register" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}

		var req ClientRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ClientRegistrationResponse{
			ClientID:   "client_test_123",
			ClientName: req.ClientName,
		})
	}))
	defer server.Close()

	ctx := context.Background()
	clientID, err := RegisterOAuthClient(ctx, server.Client(), server.URL, "http://127.0.0.1:54321/callback")
	if err != nil {
		t.Fatalf("RegisterOAuthClient failed: %v", err)
	}
	if clientID != "client_test_123" {
		t.Errorf("expected client_id 'client_test_123', got '%s'", clientID)
	}
}

func TestOAuth_TokenExchange_FullAccessAndReadOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/mcp/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()

		code := r.Form.Get("code")
		w.Header().Set("Content-Type", "application/json")
		if code == "readonly-code" {
			_ = json.NewEncoder(w).Encode(TokenExchangeResponse{
				AccessToken: "token_readonly",
				TokenType:   "Bearer",
				ExpiresIn:   2592000,
				Scope:       "orchestration:read",
			})
			return
		}

		_ = json.NewEncoder(w).Encode(TokenExchangeResponse{
			AccessToken: "token_full_access",
			TokenType:   "Bearer",
			ExpiresIn:   2592000,
			Scope:       "orchestration:read orchestration:operate",
		})
	}))
	defer server.Close()

	ctx := context.Background()

	// 1. Full access scope succeeds
	tok, err := ExchangeOAuthCode(ctx, server.Client(), server.URL, "cid", "http://127.0.0.1/cb", "verif", "full-code")
	if err != nil {
		t.Fatalf("ExchangeOAuthCode failed for full access: %v", err)
	}
	if tok.AccessToken != "token_full_access" {
		t.Errorf("expected token_full_access, got %s", tok.AccessToken)
	}

	// 2. Read only scope fails with actionable error
	_, err = ExchangeOAuthCode(ctx, server.Client(), server.URL, "cid", "http://127.0.0.1/cb", "verif", "readonly-code")
	if err == nil {
		t.Fatalf("expected error for read-only scope, got nil")
	}
	if !strings.Contains(err.Error(), "read-only") || !strings.Contains(err.Error(), "Full access") {
		t.Errorf("expected clear read-only rejection error, got: %v", err)
	}
}

func TestOAuth_LoopbackFlow_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")

	// Mock T3 Code server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/mcp/register":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(ClientRegistrationResponse{ClientID: "test_cid_456"})
		case "/oauth/mcp/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(TokenExchangeResponse{
				AccessToken: "issued_oauth_token_777",
				TokenType:   "Bearer",
				ExpiresIn:   2592000,
				Scope:       "orchestration:read orchestration:operate",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := LoopbackOAuthOptions{
		Origin:        server.URL,
		HTTPClient:    server.Client(),
		TokenFilePath: tokenPath,
		NoBrowser:     false,
		BrowserOpener: func(authURL string) error {
			// Simulate user browser navigation to callback
			parsed, err := url.Parse(authURL)
			if err != nil {
				return err
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			stateVal := parsed.Query().Get("state")

			callbackURL := fmt.Sprintf("%s?code=valid_code_123&state=%s", redirectURI, stateVal)
			go func() {
				time.Sleep(50 * time.Millisecond)
				resp, getErr := http.Get(callbackURL)
				if getErr == nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
	}

	tok, err := PerformLoopbackOAuth(ctx, opts)
	if err != nil {
		t.Fatalf("PerformLoopbackOAuth failed: %v", err)
	}

	if tok.AccessToken != "issued_oauth_token_777" {
		t.Errorf("expected access token 'issued_oauth_token_777', got '%s'", tok.AccessToken)
	}

	// Verify file mode 0600 on saved token
	fi, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("failed to stat saved token: %v", err)
	}
	perm := fi.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected file permissions 0600, got %#o", perm)
	}
}

func TestOAuth_InspectStatus(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Missing
	missingPath := filepath.Join(tmpDir, "missing.json")
	st := InspectOAuthToken(missingPath)
	if st.Status != "missing" {
		t.Errorf("expected missing, got %s", st.Status)
	}

	// 2. Expired
	expiredPath := filepath.Join(tmpDir, "expired.json")
	_ = SaveMCPOAuthToken(expiredPath, &MCPOAuthToken{
		AccessToken: "exp_tok",
		IssuedAt:    time.Now().Add(-48 * time.Hour),
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		Scope:       "orchestration:read orchestration:operate",
	})
	st = InspectOAuthToken(expiredPath)
	if st.Status != "expired" {
		t.Errorf("expected expired, got %s", st.Status)
	}
	if !strings.Contains(st.WarningMessage, "expired") {
		t.Errorf("expected warning message to mention expired, got: %s", st.WarningMessage)
	}

	// 3. Expiring soon (<72h)
	expiringSoonPath := filepath.Join(tmpDir, "expiring_soon.json")
	_ = SaveMCPOAuthToken(expiringSoonPath, &MCPOAuthToken{
		AccessToken: "soon_tok",
		IssuedAt:    time.Now().Add(-20 * 24 * time.Hour),
		ExpiresAt:   time.Now().Add(48 * time.Hour), // 48h left (<72h)
		Scope:       "orchestration:read orchestration:operate",
	})
	st = InspectOAuthToken(expiringSoonPath)
	if st.Status != "expiring_soon" {
		t.Errorf("expected expiring_soon, got %s", st.Status)
	}
	if !strings.Contains(st.WarningMessage, "expires in") {
		t.Errorf("expected warning message to mention 'expires in', got: %s", st.WarningMessage)
	}

	// 4. Valid (>72h)
	validPath := filepath.Join(tmpDir, "valid.json")
	_ = SaveMCPOAuthToken(validPath, &MCPOAuthToken{
		AccessToken: "valid_tok",
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(25 * 24 * time.Hour), // 25 days left
		Scope:       "orchestration:read orchestration:operate",
	})
	st = InspectOAuthToken(validPath)
	if st.Status != "valid" {
		t.Errorf("expected valid, got %s", st.Status)
	}
	if st.WarningMessage != "" {
		t.Errorf("expected empty warning message for valid token, got: %s", st.WarningMessage)
	}
}
