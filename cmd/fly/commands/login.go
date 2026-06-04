package commands

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
	"strings"
	"time"

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
	as := &authServer{
		listener:     listener,
		mux:          http.NewServeMux(),
		tokenCh:      make(chan string, 1),
		errCh:        make(chan error, 1),
		state:        state,
		codeVerifier: codeVerifier,
	}
	as.server = &http.Server{Handler: as.mux}

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
	as.tokenCh <- code // Pass code for exchange; caller knows to call exchangeCode.
}

func (as *authServer) Port() int {
	return as.listener.Addr().(*net.TCPAddr).Port
}

func (as *authServer) WaitForCallback(ctx context.Context) (string, error) {
	select {
	case code := <-as.tokenCh:
		return code, nil
	case err := <-as.errCh:
		as.Close()
		return "", err
	case <-ctx.Done():
		as.Close()
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
	rand.Read(b)
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
// It uses the code verifier for PKCE validation.
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
		json.Unmarshal(body, &errMsg)
		if errMsg.Error != "" {
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

func NewLoginCmd() *cobra.Command {
	var provider string
	var noBrowser bool
	var dev bool
	var email string
	var inviteCode string
	var nonInteractive bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with FunctionFly",
		Long:  "Authenticate with FunctionFly using OAuth or dev email/password.\n\nIn dev mode (--dev flag required), use email/password against the API.",
		Example: `  ff login
  ff login --provider github
  ff login --invite-code CODE
  ff login --dev --email admin@functionfly.local`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(provider, noBrowser, dev, email, inviteCode, nonInteractive)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "github", "OAuth provider (github, google)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the auth URL instead of opening a browser")
	cmd.Flags().BoolVar(&dev, "dev", false, "Use email/password login (dev mode). Requires FF_API_URL for local API.")
	cmd.Flags().StringVar(&email, "email", "", "Email for dev login (or set FF_DEV_EMAIL)")
	cmd.Flags().StringVar(&inviteCode, "invite-code", "", "Invite code for OAuth signup (or set FF_INVITE_CODE)")
	cmd.Flags().BoolVar(&nonInteractive, "no-interactive", false, "Fail without prompting in non-interactive environments")
	return cmd
}

func runLogin(provider string, noBrowser bool, dev bool, emailFlag string, inviteCodeFlag string, nonInteractive bool) error {
	if nonInteractive {
		// In CI/non-interactive mode, fail fast if not a terminal.
		if !IsInteractive() {
			if err := checkAuthEnvVars(); err != nil {
				return err
			}
		}
	}

	// Check for invite code in flag or environment variable.
	inviteCode := inviteCodeFlag
	if inviteCode == "" {
		inviteCode = os.Getenv("FF_INVITE_CODE")
	}

	baseURL := resolveBaseURL()

	// Dev mode: only activated by explicit --dev flag or FF_DEV_LOGIN=1.
	useDev := dev || os.Getenv("FF_DEV_LOGIN") == "1"
	if useDev {
		return runDevLogin(baseURL, emailFlag, nonInteractive)
	}

	// Generate PKCE and state for OAuth security.
	state := generateState()
	codeVerifier, codeChallenge, err := generatePKCEPair()
	if err != nil {
		return fmt.Errorf("could not generate PKCE: %w", err)
	}

	authServer, err := newAuthServer(state, codeVerifier)
	if err != nil {
		return err
	}
	defer authServer.Close()
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", authServer.Port())

	// Get OAuth URL from API (includes redirect_uri, invite_code, PKCE challenge).
	authURL, err := getOAuthURLFromAPI(baseURL, provider, callbackURL, codeChallenge, inviteCode)
	if err != nil {
		return fmt.Errorf("get OAuth URL: %w", err)
	}

	fmt.Printf("🔐 Authenticating with %s...\n", provider)
	if noBrowser {
		fmt.Printf("\nOpen this URL in your browser:\n%s\n\n", authURL)
	} else {
		fmt.Printf("Opening browser...\n")
		if err := openBrowser(authURL); err != nil {
			fmt.Printf("Could not open browser automatically: %v\nOpen this URL manually:\n%s\n\n", err, authURL)
		}
	}
	fmt.Printf("Waiting for authentication (Ctrl+C to cancel)...\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	code, err := authServer.WaitForCallback(ctx)
	if err != nil {
		return err
	}

	// Exchange authorization code for tokens.
	resp, err := exchangeCode(ctx, baseURL, code, callbackURL, codeVerifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	if resp.Token == "" {
		return fmt.Errorf("token exchange returned empty token")
	}

	// Populate user from OAuth response or fetch from /v1/users/me.
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

	username := userResp.Username
	if username == "" {
		username = "unknown"
	}
	daysLeft := int(time.Until(expiresAt).Hours() / 24)
	fmt.Printf("\n✅ Logged in as %s\n", username)
	if userResp.Email != "" {
		fmt.Printf("   Email: %s\n", userResp.Email)
	}
	fmt.Printf("   Provider: %s\n", provider)
	fmt.Printf("   Session:  expires in %d days\n", daysLeft)
	fmt.Printf("\nYour namespace: fx://%s/*\n", username)
	return nil
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
			delay := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
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

// runDevLogin uses POST /v1/auth/login (or /auth/login for localhost)
// with email/password for local/dev API. Password is always prompted
// interactively to avoid exposure in process listings or shell history.
func runDevLogin(baseURL, emailFlag string, nonInteractive bool) error {
	email := emailFlag
	if email == "" {
		email = os.Getenv("FF_DEV_EMAIL")
	}
	password := os.Getenv("FF_DEV_PASSWORD")
	if !nonInteractive && IsInteractive() && email == "" {
		email = Prompt("Email", "admin@functionfly.local")
	}
	if !nonInteractive && IsInteractive() && password == "" {
		password = PromptSecret("Password")
	}
	if email == "" || password == "" {
		return fmt.Errorf("email and password required for dev login (use --email and FF_DEV_PASSWORD)")
	}

	loginPath := "/v1/auth/login"
	if contains(baseURL, "localhost") || contains(baseURL, "127.0.0.1") {
		loginPath = "/auth/login"
	}
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequest("POST", baseURL+loginPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)

	var loginResp struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token,omitempty"`
		ExpiresAt    string `json:"expires_at"`
		User         *struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
			Email    string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return fmt.Errorf("invalid login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var errMsg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(bodyBytes, &errMsg)
		if errMsg.Message != "" {
			return fmt.Errorf("login failed: %s", errMsg.Message)
		}
		return fmt.Errorf("login failed: HTTP %d", resp.StatusCode)
	}
	if loginResp.Token == "" {
		return fmt.Errorf("login response missing token")
	}

	username := "unknown"
	userEmail := email
	userID := ""
	if loginResp.User != nil {
		userID = loginResp.User.ID
		if loginResp.User.Username != "" {
			username = loginResp.User.Username
		}
		if loginResp.User.Email != "" {
			userEmail = loginResp.User.Email
		}
	}

	expiresAt := resolveExpiresAt(loginResp.ExpiresAt)
	daysLeft := int(time.Until(expiresAt).Hours() / 24)
	creds := &Credentials{
		Version:      "1.0.0",
		User:         UserInfo{ID: userID, Username: username, Email: userEmail, Provider: "dev", AvatarURL: ""},
		Token:        loginResp.Token,
		TokenType:    "Bearer",
		RefreshToken: loginResp.RefreshToken,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("could not save credentials: %w", err)
	}
	fmt.Printf("\n✅ Logged in as %s (dev)\n", username)
	fmt.Printf("   Email:    %s\n", userEmail)
	fmt.Printf("   Session:  expires in %d days\n", daysLeft)
	fmt.Printf("\nYour namespace: fx://%s/*\n", username)
	return nil
}

// PromptSecret reads a password without echoing it to the terminal.
func PromptSecret(question string) string {
	fmt.Printf("%s: ", question)
	b := make([]byte, 1)
	var password []byte
	for {
		n, err := os.Stdin.Read(b)
		if err != nil || n == 0 {
			break
		}
		if b[0] == '\n' || b[0] == '\r' {
			break
		}
		password = append(password, b[0])
	}
	fmt.Println()
	return string(password)
}
