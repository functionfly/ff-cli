package commands

import (
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
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// authServer is a reusable local HTTP server for OAuth callback handling.
type authServer struct {
	listener     net.Listener
	mux          *http.ServeMux
	server       *http.Server
	tokenCh      chan string
	errCh        chan error
	state        string
	codeVerifier string
}

// newAuthServer creates a local TCP listener and starts an HTTP server.
// The provided state and codeVerifier are used to validate the OAuth callback.
func newAuthServer(state, codeVerifier string) (*authServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not start callback server: %w", err)
	}
	mux := http.NewServeMux()
	as := &authServer{
		listener: listener,
		mux:      mux,
		server: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 30 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		tokenCh:      make(chan string, 1),
		errCh:        make(chan error, 1),
		state:        state,
		codeVerifier: codeVerifier,
	}

	as.mux.HandleFunc("/callback", as.handleCallback)
	go func() {
		if err := as.server.Serve(as.listener); err != nil && err != http.ErrServerClosed {
			as.errCh <- err
		}
	}()

	return as, nil
}

func (as *authServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state parameter to prevent CSRF attacks.
	if r.URL.Query().Get("state") != as.state {
		http.Error(w, "State mismatch — possible CSRF attack", http.StatusBadRequest)
		as.errCh <- fmt.Errorf("state mismatch: expected %q, got %q", as.state, r.URL.Query().Get("state"))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		errMsg := r.URL.Query().Get("error")
		if errMsg == "" {
			errMsg = "no authorization code received"
		}
		http.Error(w, "Authorization failed: "+errMsg, http.StatusBadRequest)
		as.errCh <- fmt.Errorf("authorization failed: %s", errMsg)
		return
	}

	// Store code for exchange and notify waiting goroutine.
	w.Header().Set("Content-Type", "text/html")
	namespace := os.Getenv("FF_CLI_NAMESPACE")
	if namespace == "" {
		namespace = "fx://<your-username>/*"
	}
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.card{background:#1e293b;border-radius:12px;padding:40px;max-width:420px;width:90%;text-align:center;box-shadow:0 25px 50px rgba(0,0,0,.5)}
h2{color:#4ade80;margin:0 0 8px;font-size:28px}
p{color:#94a3b8;margin:0 0 24px;line-height:1.6}
.code{background:#0f172a;border-radius:8px;padding:16px;font-family:'SF Mono',Monaco,monospace;font-size:14px;color:#7dd3fc;word-break:break-all;margin:16px 0}
small{color:#64748b;display:block;margin-top:20px}
</style>
</head>
<body>
<div class="card">
<h2>✅ Authentication successful!</h2>
<p>Your CLI session is ready.</p>
<div class="code">`+namespace+`</div>
<small>Run <code style="color:#7dd3fc">ff whoami</code> to verify your session.</small>
</div>
</body>
</html>`)
	as.tokenCh <- code
}

func (as *authServer) Port() int {
	return as.listener.Addr().(*net.TCPAddr).Port
}

func (as *authServer) WaitForCallback(ctx context.Context) (string, error) {
	select {
	case code := <-as.tokenCh:
		_ = as.Close()
		return code, nil
	case err := <-as.errCh:
		_ = as.Close()
		return "", err
	case <-ctx.Done():
		_ = as.Close()
		return "", ctx.Err()
	}
}

func (as *authServer) Close() error {
	return as.server.Close()
}

// generatePKCEPair generates a PKCE code verifier and its S256 challenge.
func generatePKCEPair() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("PKCE randomness: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// generateState creates a cryptographically random state string.
func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-state"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// authResponse represents the API's response to an OAuth token exchange.
type authResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at"`
	User         *struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Provider  string `json:"provider"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

// exchangeCode exchanges an OAuth authorization code for tokens.
func exchangeCode(ctx context.Context, baseURL, code, redirectURI, codeVerifier string) (*authResponse, error) {
	data := url.Values{}
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/auth/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		var errMsg struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &errMsg); err == nil && errMsg.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s", errMsg.Error)
		}
		return nil, fmt.Errorf("token exchange returned HTTP %d", resp.StatusCode)
	}

	var out authResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}
	return &out, nil
}

// authChoice values drive the login form's main Select.
const (
	authChoiceBrowser = "browser"
	authChoiceManual  = "manual"
)

func NewLoginCmd() *cobra.Command {
	var provider string
	var noBrowser bool
	var inviteCode string
	var nonInteractive bool
	var token string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with FunctionFly",
		Long: "Authenticate with FunctionFly.\n\n" +
			"Interactive mode shows a menu to either open the browser for OAuth or paste a CLI access token. " +
			"Non-interactive mode defaults to OAuth and prints the auth URL; set FF_TOKEN to use a token directly.",
		Example: `  ff login
  ff login --provider github
  ff login --invite-code CODE
  ff login --no-browser
  FF_TOKEN=ff_xxx ff login --no-interactive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(provider, noBrowser, inviteCode, nonInteractive, token)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "github", "OAuth provider (github, google)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the auth URL instead of opening a browser")
	cmd.Flags().StringVar(&inviteCode, "invite-code", "", "Invite code for OAuth signup (or set FF_INVITE_CODE)")
	cmd.Flags().BoolVar(&nonInteractive, "no-interactive", false, "Fail without prompting in non-interactive environments")
	cmd.Flags().StringVar(&token, "token", "", "Use a CLI access token directly (or set FF_TOKEN)")
	return cmd
}

// runLogin implements the Resend-style clack login flow.
//
// Interactive mode shows a huh form with a Note card and a Select offering:
//   - "Open functionfly.com/login in browser" (default, OAuth + PKCE)
//   - "Enter an access token manually"
//
// Non-interactive mode bypasses the form: if FF_TOKEN / --token is set it is
// validated and saved; otherwise the OAuth URL is printed and the user is
// expected to complete the flow manually.
func runLogin(provider string, noBrowser bool, inviteCodeFlag string, nonInteractive bool, tokenFlag string) error {
	// Token env / flag short-circuit (works in both interactive and non-interactive).
	// Run this BEFORE the non-interactive env-var check so a --token flag
	// is honoured even when FF_TOKEN is not set in the environment.
	if tokenFlag == "" {
		tokenFlag = os.Getenv("FF_TOKEN")
	}
	if tokenFlag != "" {
		return completeManualToken(provider, tokenFlag)
	}

	if nonInteractive && !IsInteractive() {
		if err := checkAuthEnvVars(); err != nil {
			return err
		}
	}

	// Invite code resolution.
	inviteCode := inviteCodeFlag
	if inviteCode == "" {
		inviteCode = os.Getenv("FF_INVITE_CODE")
	}

	// Non-interactive: skip the form, default to OAuth + print URL.
	if nonInteractive || !IsInteractive() {
		return runBrowserOAuth(provider, noBrowser, inviteCode)
	}

	// Interactive: Resend-style clack form.
	choice, err := promptAuthChoice()
	if err != nil {
		return err
	}
	switch choice {
	case authChoiceManual:
		tok, err := promptToken()
		if err != nil {
			return err
		}
		return completeManualToken(provider, tok)
	default:
		return runBrowserOAuth(provider, noBrowser, inviteCode)
	}
}

// promptAuthChoice renders the clack-style authentication card and returns the user's selection.
func promptAuthChoice() (string, error) {
	authSite := resolveAuthSiteURL()
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("FunctionFly Authentication").
				Description(fmt.Sprintf("Use a CLI access token for full access to every ff command.\n"+
					"Create or copy one from %s/api-keys.\n", authSite)),
			huh.NewSelect[string]().
				Title("How would you like to authenticate?").
				Options(
					huh.NewOption(fmt.Sprintf("Open %s/login in browser", authSite), authChoiceBrowser).Selected(true),
					huh.NewOption("Enter an access token manually", authChoiceManual),
				).
				Filtering(false).
				Value(&choice),
		),
	).WithTheme(loginTheme())
	if err := form.Run(); err != nil {
		return "", err
	}
	return choice, nil
}

// promptToken renders a masked input for a CLI access token and returns the value.
func promptToken() (string, error) {
	authSite := resolveAuthSiteURL()
	var token string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Paste your CLI access token").
				Description(fmt.Sprintf("Find or generate one at %s/api-keys", authSite)).
				EchoMode(huh.EchoModePassword).
				Value(&token).
				Validate(validatePastedToken),
		),
	).WithTheme(loginTheme())
	if err := form.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(token), nil
}

// loginTheme returns the huh theme to use for the login form, honouring
// the NO_COLOR / FF_NO_COLOR / TERM=dumb conventions.
func loginTheme() *huh.Theme {
	if !IsColorTerminal() {
		return huh.ThemeBase()
	}
	return huh.ThemeCharm()
}

// minAccessTokenLen is the minimum plausible CLI access token length. Most
// FunctionFly tokens are 100+ characters; anything under 8 is almost
// certainly a paste error.
const minAccessTokenLen = 8

// validatePastedToken enforces non-empty + minimum length on a pasted token.
// It is exported via the huh form's Validate hook and reused by any code path
// that accepts a token (flag, env var, or interactive prompt).
func validatePastedToken(s string) error {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return fmt.Errorf("token cannot be empty")
	}
	if len(trimmed) < minAccessTokenLen {
		return fmt.Errorf("token is too short (got %d chars, need at least %d) — paste the full token from %s/api-keys",
			len(trimmed), minAccessTokenLen, resolveAuthSiteURL())
	}
	return nil
}

// completeManualToken validates a user-supplied token against the API and
// persists the resulting credentials. Provider is recorded as "token" so
// `ff whoami` reflects how the session was obtained.
func completeManualToken(provider, token string) error {
	if err := validatePastedToken(token); err != nil {
		return err
	}
	if !allowTokenValidation() {
		return fmt.Errorf("too many failed token validations; wait a minute and try again")
	}
	client := NewAPIClientWithToken(token)
	var user struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Provider  string `json:"provider"`
		AvatarURL string `json:"avatar_url"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := client.Get("/v1/users/me", &user); err != nil {
		recordTokenValidationFailure()
		return fmt.Errorf("token rejected by API: %w", err)
	}
	username := user.Username
	if username == "" {
		username = "unknown"
	}
	providerLabel := "token"
	if provider != "" && provider != "github" {
		providerLabel = provider
	}
	creds := &Credentials{
		Version:   "1.0.0",
		User:      UserInfo{ID: user.ID, Username: username, Email: user.Email, Provider: providerLabel, AvatarURL: user.AvatarURL},
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: resolveExpiresAt(user.ExpiresAt),
		CreatedAt: time.Now(),
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("could not save credentials: %w", err)
	}
	printLoginSuccess(username, user.Email, providerLabel, creds.ExpiresAt)
	return nil
}

// runBrowserOAuth runs the original PKCE + browser flow.
func runBrowserOAuth(provider string, noBrowser bool, inviteCode string) error {
	baseURL := resolveBaseURL()

	state := generateState()
	codeVerifier, codeChallenge, err := generatePKCEPair()
	if err != nil {
		return fmt.Errorf("could not generate PKCE: %w", err)
	}

	authSrv, err := newAuthServer(state, codeVerifier)
	if err != nil {
		return err
	}
	defer authSrv.Close()
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", authSrv.Port())

	authURL, err := getOAuthURLFromAPI(baseURL, provider, callbackURL, codeChallenge, inviteCode)
	if err != nil {
		return fmt.Errorf("get OAuth URL: %w", err)
	}

	if noBrowser {
		fmt.Printf("🔐 Open this URL in your browser to authenticate with %s:\n%s\n", provider, authURL)
	} else if IsInteractive() {
		authSite := resolveAuthSiteURL()
		fmt.Printf("🔐 Opening %s/login in your browser (provider: %s)...\n", authSite, provider)
		if err := openBrowser(authURL); err != nil {
			fmt.Printf("Could not open browser automatically: %v\nOpen this URL manually:\n%s\n\n", err, authURL)
		}
	} else {
		fmt.Printf("🔐 Open this URL in your browser to authenticate with %s:\n%s\n", provider, authURL)
	}
	fmt.Printf("Waiting for authentication (Ctrl+C to cancel)...\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	code, err := authSrv.WaitForCallback(ctx)
	if err != nil {
		return err
	}

	resp, err := exchangeCode(ctx, baseURL, code, callbackURL, codeVerifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	if resp.Token == "" {
		return fmt.Errorf("token exchange returned empty token")
	}

	var userResp struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Provider  string `json:"provider"`
		AvatarURL string `json:"avatar_url"`
	}
	if resp.User != nil && resp.User.ID != "" {
		userResp = struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			Email     string `json:"email"`
			Provider  string `json:"provider"`
			AvatarURL string `json:"avatar_url"`
		}{
			ID:        resp.User.ID,
			Username:  resp.User.Username,
			Email:     resp.User.Email,
			Provider:  resp.User.Provider,
			AvatarURL: resp.User.AvatarURL,
		}
	} else {
		client := NewAPIClientWithToken(resp.Token)
		if err := client.Get("/v1/users/me", &userResp); err != nil {
			fmt.Printf("⚠️  Could not fetch user info: %v\n", err)
		}
	}

	expiresAt := resolveExpiresAt(resp.ExpiresAt)
	creds := &Credentials{
		Version:      "1.0.0",
		User:         UserInfo{ID: userResp.ID, Username: userResp.Username, Email: userResp.Email, Provider: provider, AvatarURL: userResp.AvatarURL},
		Token:        resp.Token,
		TokenType:    "Bearer",
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("could not save credentials: %w", err)
	}
	printLoginSuccess(userResp.Username, userResp.Email, provider, expiresAt)
	return nil
}

func printLoginSuccess(username, email, provider string, expiresAt time.Time) {
	if username == "" {
		username = "unknown"
	}
	daysLeft := int(time.Until(expiresAt).Hours() / 24)
	fmt.Printf("\n✅ Logged in as %s\n", username)
	if email != "" {
		fmt.Printf("   Email:    %s\n", email)
	}
	fmt.Printf("   Provider: %s\n", provider)
	fmt.Printf("   Session:  expires in %d days\n", daysLeft)
	fmt.Printf("\nYour namespace: fx://%s/*\n", username)
}

// getOAuthURLFromAPI calls POST /auth/oauth/url with PKCE challenge and returns the URL to open.
// It includes retry logic with exponential backoff for transient network errors.
func getOAuthURLFromAPI(baseURL, provider, redirectURI, codeChallenge, inviteCode string) (string, error) {
	data := url.Values{}
	data.Set("provider", provider)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_challenge", codeChallenge)
	data.Set("code_challenge_method", "S256")
	if inviteCode != "" {
		data.Set("invite_code", inviteCode)
	}

	const maxRetries = 3
	const timeout = 20 * time.Second

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			time.Sleep(delay)
		}

		req, err := http.NewRequest(http.MethodPost, baseURL+"/auth/oauth/url", strings.NewReader(data.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "TLS handshake timeout") {
				continue
			}
			return "", fmt.Errorf("%w\n   → Check your internet connection and try again", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("API returned %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			msg := string(body)
			if msg == "" {
				return "", fmt.Errorf("API returned %d", resp.StatusCode)
			}
			if resp.StatusCode == 400 && contains(msg, "invite code") {
				return "", fmt.Errorf("API returned %d: %s\n   → FunctionFly is invite-only. Use --invite-code or set FF_INVITE_CODE", resp.StatusCode, msg)
			}
			return "", fmt.Errorf("API returned %d: %s", resp.StatusCode, msg)
		}

		var out struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", err
		}
		if out.URL == "" {
			return "", fmt.Errorf("API returned empty OAuth URL")
		}
		return out.URL, nil
	}

	if strings.Contains(lastErr.Error(), "TLS handshake timeout") {
		return "", fmt.Errorf("TLS handshake timeout after %d attempts\n   → The API server may be temporarily unavailable or your network connection is slow\n   → Please check your internet connection and try again", maxRetries+1)
	}
	return "", fmt.Errorf("%w\n   → Check your internet connection and try again", lastErr)
}

// checkAuthEnvVars validates that required env vars are set in non-interactive mode.
// Returns an error if FF_TOKEN is not set, guiding the user to use token-based auth.
func checkAuthEnvVars() error {
	if os.Getenv("FF_TOKEN") != "" {
		return nil
	}
	return fmt.Errorf("not logged in and no FF_TOKEN set\n   → Set FF_TOKEN or run ff login interactively")
}

// tokenValidationWindow tracks the timestamps of recent token validation
// failures so the CLI can back off a runaway paste / brute-force attempt.
type tokenValidationWindow struct {
	mu        sync.Mutex
	failures  []time.Time
	maxRecent int
	window    time.Duration
}

var tokenRateLimit = &tokenValidationWindow{
	maxRecent: 5,
	window:    time.Minute,
}

// allowTokenValidation reports whether a new token validation is allowed.
func allowTokenValidation() bool {
	tokenRateLimit.mu.Lock()
	defer tokenRateLimit.mu.Unlock()
	cutoff := time.Now().Add(-tokenRateLimit.window)
	recent := tokenRateLimit.failures[:0]
	for _, t := range tokenRateLimit.failures {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	tokenRateLimit.failures = recent
	return len(recent) < tokenRateLimit.maxRecent
}

// recordTokenValidationFailure records one failed token validation attempt.
func recordTokenValidationFailure() {
	tokenRateLimit.mu.Lock()
	defer tokenRateLimit.mu.Unlock()
	tokenRateLimit.failures = append(tokenRateLimit.failures, time.Now())
}
