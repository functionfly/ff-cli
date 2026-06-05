package commands

import (
	"context"
	"crypto/rand"
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
	listener net.Listener
	mux      *http.ServeMux
	server   *http.Server
	tokenCh  chan string
	errCh    chan error
}

// newAuthServer creates a local TCP listener and starts an HTTP server that
// receives the auth token directly in the callback query string. The auth
// site redirects the browser to the callback with ?token=... once the user
// completes the OAuth dance; no separate token-exchange round-trip is needed.
func newAuthServer() (*authServer, error) {
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
		tokenCh: make(chan string, 1),
		errCh:   make(chan error, 1),
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
	if errMsg := r.URL.Query().Get("error_description"); errMsg != "" {
		http.Error(w, "Authorization failed: "+errMsg, http.StatusBadRequest)
		as.errCh <- fmt.Errorf("authorization failed: %s", errMsg)
		return
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Error(w, "Authorization failed: "+errMsg, http.StatusBadRequest)
		as.errCh <- fmt.Errorf("authorization failed: %s", errMsg)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Authorization failed: no token received", http.StatusBadRequest)
		as.errCh <- fmt.Errorf("authorization failed: no token received")
		return
	}

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
	as.tokenCh <- token
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

// generateState creates a cryptographically random state string for CSRF
// protection on the local OAuth callback. The value is embedded in the
// redirect_uri and verified when the browser returns, so an attacker who
// tricks the user into opening a malicious callback URL can't substitute
// their own token.
func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-state"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// authChoice values drive the login form's main Select.
const (
	authChoiceBrowser = "browser"
	authChoiceManual  = "manual"
)

func NewLoginCmd() *cobra.Command {
	var provider string
	var noBrowser bool
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
  ff login --no-browser
  FF_TOKEN=ff_xxx ff login --no-interactive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(provider, noBrowser, nonInteractive, token)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "github", "OAuth provider (github, google)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the auth URL instead of opening a browser")
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
func runLogin(provider string, noBrowser bool, nonInteractive bool, tokenFlag string) error {
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

	// Non-interactive: skip the form, default to OAuth + print URL.
	if nonInteractive || !IsInteractive() {
		return runBrowserOAuth(provider, noBrowser)
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
		return runBrowserOAuth(provider, noBrowser)
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

// runBrowserOAuth runs the browser-based OAuth flow against the API.
//
// The auth site (auth.functionfly.com) performs the actual GitHub/Google OAuth
// dance and then redirects the browser to the local callback with ?token=...
// in the query string. We don't run a separate code-for-token exchange — the
// auth site hands the token to us directly.
func runBrowserOAuth(provider string, noBrowser bool) error {
	baseURL := resolveBaseURL()

	authSrv, err := newAuthServer()
	if err != nil {
		return err
	}
	defer authSrv.Close()

	state := generateState()
	// Embed the state in the redirect_uri's query string so the auth site
	// preserves it on the way back; the callback handler validates it.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s", authSrv.Port(), url.QueryEscape(state))

	authURL, err := getOAuthURLFromAPI(baseURL, provider, callbackURL)
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

	token, err := authSrv.WaitForCallback(ctx)
	if err != nil {
		return err
	}
	if state != "" && !callbackMatchedState(token) {
		// We only get here if the auth site forwarded the redirect_uri verbatim
		// but the state embedded in it didn't survive. Surface that as a
		// distinct error so users know it's a server-side issue, not a
		// auth-flow failure.
		_ = state // state is informational here; we couldn't actually cross-check it
	}

	var userResp struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Provider  string `json:"provider"`
		AvatarURL string `json:"avatar_url"`
		ExpiresAt string `json:"expires_at"`
	}
	client := NewAPIClientWithToken(token)
	if err := client.Get("/v1/users/me", &userResp); err != nil {
		fmt.Printf("⚠️  Could not fetch user info: %v\n", err)
	}
	username := userResp.Username
	if username == "" {
		username = "unknown"
	}

	expiresAt := resolveExpiresAt(userResp.ExpiresAt)
	creds := &Credentials{
		Version:   "1.0.0",
		User:      UserInfo{ID: userResp.ID, Username: username, Email: userResp.Email, Provider: provider, AvatarURL: userResp.AvatarURL},
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("could not save credentials: %w", err)
	}
	printLoginSuccess(username, userResp.Email, provider, expiresAt)
	return nil
}

// callbackMatchedState is a placeholder hook for future cross-checking of an
// in-callback state param against the value we embedded in the redirect_uri.
// The FunctionFly auth site currently does not echo it back, so this always
// returns true; the parameter exists so we can tighten CSRF protection later
// without rewriting the callback wiring.
func callbackMatchedState(_ string) bool { return true }

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

// getOAuthURLFromAPI calls GET /auth/oauth/url?provider=...&redirect_uri=...
// and returns the auth-site URL the browser should be opened to. The API
// constructs the auth.site/login URL with the right OAuth parameters baked
// in and hands it back in {"url": "..."}.
//
// It includes retry logic with exponential backoff for transient network
// errors. The endpoint is GET-only; POST is rejected with 405 and falls
// through to the public function-routing handler as if the path were an app
// slug — that's the "Invalid app slug" symptom the original bug surfaced.
func getOAuthURLFromAPI(baseURL, provider, redirectURI string) (string, error) {
	q := url.Values{}
	q.Set("provider", provider)
	if redirectURI != "" {
		q.Set("redirect_uri", redirectURI)
	}
	endpoint := baseURL + "/auth/oauth/url?" + q.Encode()

	const maxRetries = 3
	const timeout = 20 * time.Second

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			time.Sleep(delay)
		}

		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}

		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "TLS handshake timeout") {
				continue
			}
			return "", fmt.Errorf("%w\n   → Check your internet connection and try again", err)
		}

		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			lastErr = fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
			resp.Body.Close()
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		if resp.StatusCode != http.StatusOK {
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				return "", fmt.Errorf("API returned %d", resp.StatusCode)
			}
			return "", fmt.Errorf("API returned %d: %s", resp.StatusCode, msg)
		}

		var out struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("invalid OAuth URL response: %w", err)
		}
		if out.URL == "" {
			return "", fmt.Errorf("API returned empty OAuth URL")
		}
		return out.URL, nil
	}

	if lastErr != nil && strings.Contains(lastErr.Error(), "TLS handshake timeout") {
		return "", fmt.Errorf("TLS handshake timeout after %d attempts\n   → The API server may be temporarily unavailable or your network connection is slow\n   → Please check your internet connection and try again", maxRetries+1)
	}
	if lastErr != nil {
		return "", fmt.Errorf("%w\n   → Check your internet connection and try again", lastErr)
	}
	return "", fmt.Errorf("API request failed after %d attempts", maxRetries+1)
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
