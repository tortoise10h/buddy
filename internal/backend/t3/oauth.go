package t3

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MCPOAuthToken models the 30-day OAuth credentials stored at ~/.buddy/t3_mcp_oauth.json.
// Per Decision 1, this token is stored with file mode 0600 and NEVER in state.json.
type MCPOAuthToken struct {
	AccessToken string    `json:"access_token"`
	Origin      string    `json:"origin,omitempty"`
	IssuedAt    time.Time `json:"issued_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scope       string    `json:"scope,omitempty"`
}

// rawOAuthToken is an internal helper for flexible timestamp decoding.
type rawOAuthToken struct {
	AccessToken string          `json:"access_token"`
	AccessTokenAlt string       `json:"accessToken"`
	Origin      string          `json:"origin,omitempty"`
	IssuedAt    json.RawMessage `json:"issued_at,omitempty"`
	ExpiresAt   json.RawMessage `json:"expires_at,omitempty"`
	ExpiresIn   json.RawMessage `json:"expires_in,omitempty"`
	Scope       string          `json:"scope,omitempty"`
}

func parseFlexibleTime(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	str := strings.Trim(string(raw), "\"")
	// Try RFC3339 / ISO8601
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
		return t, nil
	}
	// Try integer unix timestamp (seconds or milliseconds)
	if num, err := strconv.ParseInt(str, 10, 64); err == nil {
		if num > 1e11 { // milliseconds
			return time.UnixMilli(num), nil
		}
		return time.Unix(num, 0), nil
	}
	// Try float
	if flt, err := strconv.ParseFloat(str, 64); err == nil {
		return time.Unix(int64(flt), 0), nil
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", str)
}

// UnmarshalJSON implements custom JSON unmarshaling to handle flexible time formats.
func (t *MCPOAuthToken) UnmarshalJSON(data []byte) error {
	var raw rawOAuthToken
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	token := raw.AccessToken
	if token == "" {
		token = raw.AccessTokenAlt
	}
	t.AccessToken = token
	t.Origin = raw.Origin
	t.Scope = raw.Scope

	if len(raw.IssuedAt) > 0 {
		issued, err := parseFlexibleTime(raw.IssuedAt)
		if err == nil {
			t.IssuedAt = issued
		}
	}

	if len(raw.ExpiresAt) > 0 {
		expires, err := parseFlexibleTime(raw.ExpiresAt)
		if err == nil {
			t.ExpiresAt = expires
		}
	} else if len(raw.ExpiresIn) > 0 {
		var secs int64
		if err := json.Unmarshal(raw.ExpiresIn, &secs); err == nil && secs > 0 {
			base := t.IssuedAt
			if base.IsZero() {
				base = time.Now()
			}
			t.ExpiresAt = base.Add(time.Duration(secs) * time.Second)
		}
	}

	return nil
}

// IsExpired returns true if the token is past its expiration time.
func (t *MCPOAuthToken) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// DefaultMCPOAuthPath returns the canonical path for ~/.buddy/t3_mcp_oauth.json.
func DefaultMCPOAuthPath() string {
	if dir := os.Getenv("BUDDY_DIR"); dir != "" {
		return filepath.Join(dir, "t3_mcp_oauth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".buddy", "t3_mcp_oauth.json")
	}
	return filepath.Join(home, ".buddy", "t3_mcp_oauth.json")
}

// SaveMCPOAuthToken writes the OAuth token to disk with strict 0600 file permissions.
func SaveMCPOAuthToken(path string, token *MCPOAuthToken) error {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for mcp oauth token: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp oauth token: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open mcp oauth token file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write mcp oauth token: %w", err)
	}

	// Ensure permissions are strictly 0600 even if file already existed
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("failed to set 0600 permissions on mcp oauth token: %w", err)
	}

	return nil
}

// LoadMCPOAuthToken reads the OAuth token from disk and validates expiry.
func LoadMCPOAuthToken(path string) (*MCPOAuthToken, error) {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token is missing (file %s not found). Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired, path)
		}
		return nil, fmt.Errorf("failed to read mcp oauth token from %s: %w", path, err)
	}

	var token MCPOAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("malformed mcp oauth token at %s: %w", path, err)
	}

	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token is empty. Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired)
	}

	if token.IsExpired() {
		return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token expired at %s. Run 'buddy-ctl t3 auth login' to re-authenticate", ErrMcpAuthRequired, token.ExpiresAt.Format(time.RFC3339))
	}

	return &token, nil
}

// FileMCPTokenProvider implements MCPTokenProvider by loading ~/.buddy/t3_mcp_oauth.json.
type FileMCPTokenProvider struct {
	mu       sync.RWMutex
	filePath string
}

// NewFileMCPTokenProvider creates a provider for the given file path.
func NewFileMCPTokenProvider(path string) *FileMCPTokenProvider {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}
	return &FileMCPTokenProvider{filePath: path}
}

// GetMCPToken reads and validates the token from file.
func (p *FileMCPTokenProvider) GetMCPToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	token, err := LoadMCPOAuthToken(p.filePath)
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// SetFilePath updates the target file path.
func (p *FileMCPTokenProvider) SetFilePath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filePath = path
}

// OAuthTokenStatus represents the status assessment of an OAuth token.
type OAuthTokenStatus struct {
	Status         string        `json:"status"` // "valid", "expiring_soon", "expired", "missing", "invalid"
	FilePath       string        `json:"file_path"`
	Exists         bool          `json:"exists"`
	Scope          string        `json:"scope,omitempty"`
	IssuedAt       time.Time     `json:"issued_at,omitempty"`
	ExpiresAt      time.Time     `json:"expires_at,omitempty"`
	ExpiresIn      time.Duration `json:"expires_in,omitempty"`
	HoursRemaining float64       `json:"hours_remaining,omitempty"`
	WarningMessage string        `json:"warning_message,omitempty"`
}

// InspectOAuthToken checks the OAuth token on disk without throwing an error if missing or expired.
func InspectOAuthToken(path string) OAuthTokenStatus {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}

	res := OAuthTokenStatus{
		FilePath: path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		res.Status = "missing"
		res.WarningMessage = "T3 Code /mcp OAuth token is missing. Run 'buddy-ctl t3 auth login' to authenticate"
		return res
	}

	res.Exists = true
	var token MCPOAuthToken
	if err := json.Unmarshal(data, &token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		res.Status = "invalid"
		res.WarningMessage = "T3 Code /mcp OAuth token is malformed. Run 'buddy-ctl t3 auth login' to re-authenticate"
		return res
	}

	res.Scope = token.Scope
	res.IssuedAt = token.IssuedAt
	res.ExpiresAt = token.ExpiresAt

	if token.ExpiresAt.IsZero() {
		res.Status = "valid"
		return res
	}

	now := time.Now()
	if now.After(token.ExpiresAt) {
		res.Status = "expired"
		res.WarningMessage = fmt.Sprintf("T3 Code /mcp OAuth token expired at %s. Run 'buddy-ctl t3 auth login' to re-authenticate", token.ExpiresAt.Format(time.RFC3339))
		return res
	}

	rem := token.ExpiresAt.Sub(now)
	res.ExpiresIn = rem
	res.HoursRemaining = rem.Hours()

	// 72-hour threshold for proactive warning
	if rem < 72*time.Hour {
		res.Status = "expiring_soon"
		hours := int(rem.Hours())
		res.WarningMessage = fmt.Sprintf("T3 Code /mcp OAuth expires in %dh — run 'buddy-ctl t3 auth login' soon to avoid interrupting worker Q&A", hours)
		return res
	}

	res.Status = "valid"
	return res
}

// PKCEParams holds a generated code verifier and code challenge.
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	Method        string // "S256"
}

// GeneratePKCE creates a cryptographic S256 PKCE pair according to RFC 7636.
func GeneratePKCE() (*PKCEParams, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes for PKCE: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(bytes)

	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return &PKCEParams{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		Method:        "S256",
	}, nil
}

// ClientRegistrationRequest is the payload sent to POST /oauth/mcp/register.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// ClientRegistrationResponse is the payload received from POST /oauth/mcp/register.
type ClientRegistrationResponse struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
}

// RegisterOAuthClient registers a dynamic client with T3 Code via POST /oauth/mcp/register.
func RegisterOAuthClient(ctx context.Context, httpClient *http.Client, origin, redirectURI string) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	regURL := fmt.Sprintf("%s/oauth/mcp/register", strings.TrimRight(origin, "/"))

	reqBody := ClientRegistrationRequest{
		ClientName:              "Buddy Control Tower",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: "none",
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, regURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to register oauth client at %s: %w", regURL, err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("client registration failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var regResp ClientRegistrationResponse
	if err := json.Unmarshal(bodyBytes, &regResp); err != nil {
		return "", fmt.Errorf("failed to decode client registration response: %w", err)
	}

	if regResp.ClientID == "" {
		return "", fmt.Errorf("empty client_id returned from registration")
	}

	return regResp.ClientID, nil
}

// BuildAuthorizeURL builds the interactive authorization URL to open in the browser.
func BuildAuthorizeURL(origin, clientID, redirectURI, codeChallenge, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("resource", fmt.Sprintf("%s/mcp", strings.TrimRight(origin, "/")))

	return fmt.Sprintf("%s/oauth/mcp/authorize?%s", strings.TrimRight(origin, "/"), q.Encode())
}

// TokenExchangeResponse is the payload received from POST /oauth/mcp/token.
type TokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// ExchangeOAuthCode exchanges the authorization code for an MCP access token via POST /oauth/mcp/token.
func ExchangeOAuthCode(ctx context.Context, httpClient *http.Client, origin, clientID, redirectURI, codeVerifier, code string) (*MCPOAuthToken, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	tokenURL := fmt.Sprintf("%s/oauth/mcp/token", strings.TrimRight(origin, "/"))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", codeVerifier)
	form.Set("resource", fmt.Sprintf("%s/mcp", strings.TrimRight(origin, "/")))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange oauth code at %s: %w", tokenURL, err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp TokenExchangeResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token exchange response: %w", err)
	}

	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("empty access_token received in oauth token response")
	}

	// Verify required scope
	if !strings.Contains(tokenResp.Scope, "operate") {
		return nil, fmt.Errorf("T3 Code OAuth token was granted read-only scope (%q). Buddy Control Tower requires 'Full access' (orchestration:operate) to launch threads and send messages. Please run 'buddy-ctl t3 auth login' again and select 'Full access'", tokenResp.Scope)
	}

	issuedAt := time.Now().UTC()
	var expiresAt time.Time
	if tokenResp.ExpiresIn > 0 {
		expiresAt = issuedAt.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &MCPOAuthToken{
		AccessToken: tokenResp.AccessToken,
		Origin:      origin,
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
		Scope:       tokenResp.Scope,
	}, nil
}

// LoopbackOAuthOptions configures the loopback OAuth pairing flow.
type LoopbackOAuthOptions struct {
	Origin         string
	PreferredPort  int
	NoBrowser      bool
	BrowserOpener  func(url string) error
	HTTPClient     *http.Client
	TokenFilePath  string
	StatusCallback func(step string, detail string)
}

// DefaultBrowserOpener opens URLs via system default browser on macOS.
func DefaultBrowserOpener(targetURL string) error {
	cmd := exec.Command("open", targetURL)
	return cmd.Start()
}

// PerformLoopbackOAuth executes the entire 30-day OAuth pairing flow.
func PerformLoopbackOAuth(ctx context.Context, opts LoopbackOAuthOptions) (*MCPOAuthToken, error) {
	origin := strings.TrimRight(opts.Origin, "/")
	if origin == "" {
		discovery := NewServerDiscovery()
		runtime, err := discovery.DiscoverRuntime()
		if err != nil {
			return nil, fmt.Errorf("failed to discover running T3 Code origin: %w", err)
		}
		origin = strings.TrimRight(runtime.Origin, "/")
	}

	// 1. Start ephemeral loopback HTTP server
	addr := fmt.Sprintf("127.0.0.1:%d", opts.PreferredPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind loopback callback listener on %s: %w", addr, err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	if opts.StatusCallback != nil {
		opts.StatusCallback("listening", fmt.Sprintf("Bound loopback listener at %s", redirectURI))
	}

	// 2. Generate PKCE & state
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	authState := base64.RawURLEncoding.EncodeToString(stateBytes)

	// 3. Register client dynamically via POST /oauth/mcp/register
	if opts.StatusCallback != nil {
		opts.StatusCallback("registering", fmt.Sprintf("Registering client with T3 Code at %s", origin))
	}
	clientID, err := RegisterOAuthClient(ctx, opts.HTTPClient, origin, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("client registration failed: %w", err)
	}

	// 4. Construct authorization URL
	authURL := BuildAuthorizeURL(origin, clientID, redirectURI, pkce.CodeChallenge, authState)

	// 5. Open browser (or notify caller)
	if opts.StatusCallback != nil {
		opts.StatusCallback("authorizing", authURL)
	}

	if !opts.NoBrowser {
		opener := opts.BrowserOpener
		if opener == nil {
			opener = DefaultBrowserOpener
		}
		_ = opener(authURL)
	}

	// 6. Wait for callback
	type callbackResult struct {
		code  string
		err   error
	}
	ch := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errStr := q.Get("error"); errStr != "" {
			desc := q.Get("error_description")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(`<!DOCTYPE html><html><body><h2>Authentication Failed</h2><p>%s: %s</p></body></html>`, errStr, desc)))
			ch <- callbackResult{err: fmt.Errorf("oauth error from server: %s (%s)", errStr, desc)}
			return
		}

		receivedState := q.Get("state")
		if receivedState != authState {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h2>Invalid State</h2><p>State parameter did not match.</p></body></html>`))
			ch <- callbackResult{err: fmt.Errorf("mismatched oauth state")}
			return
		}

		code := q.Get("code")
		if code == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h2>Missing Code</h2><p>Authorization code not returned.</p></body></html>`))
			ch <- callbackResult{err: fmt.Errorf("missing authorization code")}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Buddy &mdash; Authentication Successful</title></head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; text-align: center; padding: 60px 20px;">
  <div style="max-width: 480px; margin: 0 auto; background: #f6f8fa; border: 1px solid #d0d7de; border-radius: 8px; padding: 32px;">
    <h2 style="color: #1a7f37; margin-top: 0;">&#10003; Successfully Paired!</h2>
    <p style="color: #24292f; font-size: 15px; line-height: 1.5;">Buddy Control Tower is now paired with T3 Code. You can close this window and return to your terminal.</p>
  </div>
</body>
</html>`))

		ch <- callbackResult{code: code}
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		code = res.code
	}

	// 7. Exchange code for access token
	if opts.StatusCallback != nil {
		opts.StatusCallback("exchanging", "Exchanging authorization code for 30-day token")
	}
	token, err := ExchangeOAuthCode(ctx, opts.HTTPClient, origin, clientID, redirectURI, pkce.CodeVerifier, code)
	if err != nil {
		return nil, err
	}

	// 8. Persist to disk with mode 0600
	tokenPath := opts.TokenFilePath
	if tokenPath == "" {
		tokenPath = DefaultMCPOAuthPath()
	}
	if err := SaveMCPOAuthToken(tokenPath, token); err != nil {
		return nil, fmt.Errorf("failed to save oauth token: %w", err)
	}

	return token, nil
}
