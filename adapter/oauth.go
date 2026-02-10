package saxo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	tokenSuffix      = "_token.bin"
	earlyRefreshTime = 2 * time.Minute
)

// Environment types for Saxo Bank
type SaxoEnvironment string

const (
	SaxoSIM  SaxoEnvironment = "sim"
	SaxoLive SaxoEnvironment = "live"
)

// LoadSaxoEnvironmentConfig loads environment-specific Saxo configuration from environment variables
// Returns: oauthConfigs, baseURL, websocketURL, environment, error
func LoadSaxoEnvironmentConfig(logger *slog.Logger) (map[string]*oauth2.Config, string, string, SaxoEnvironment, error) {
	environment := os.Getenv("SAXO_ENVIRONMENT")
	if environment == "" {
		environment = "sim" // Default to SIM for safety
	}

	// Read credentials from simple environment variables
	clientID := os.Getenv("SAXO_CLIENT_ID")
	clientSecret := os.Getenv("SAXO_CLIENT_SECRET")

	// Validate credentials
	if clientID == "" {
		return nil, "", "", "", fmt.Errorf("SAXO_CLIENT_ID not set")
	}
	if clientSecret == "" {
		return nil, "", "", "", fmt.Errorf("SAXO_CLIENT_SECRET not set")
	}

	var authURL, tokenURL, baseURL, websocketURL string
	var saxoEnv SaxoEnvironment

	// Set URLs based on environment
	switch environment {
	case "sim":
		authURL = "https://sim.logonvalidation.net/authorize"
		tokenURL = "https://sim.logonvalidation.net/token"
		baseURL = "https://gateway.saxobank.com/sim/openapi"
		websocketURL = "https://sim-streaming.saxobank.com/sim/oapi/streaming/ws"
		saxoEnv = SaxoSIM
		logger.Info("Using SIM trading environment",
			"environment", "sim",
			"base_url", baseURL,
			"websocket_url", websocketURL)

	case "live":
		authURL = "https://live.logonvalidation.net/authorize"
		tokenURL = "https://live.logonvalidation.net/token"
		baseURL = "https://gateway.saxobank.com/openapi"
		websocketURL = "https://live-streaming.saxobank.com/oapi/streaming/ws"
		saxoEnv = SaxoLive
		logger.Warn("LIVE trading environment - real money at risk!",
			"environment", "live",
			"base_url", baseURL,
			"websocket_url", websocketURL)

	default:
		return nil, "", "", "", fmt.Errorf("invalid SAXO_ENVIRONMENT: %s (must be 'sim' or 'live')", environment)
	}

	// Create OAuth2 configuration
	oauthConfig := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"openapi"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		RedirectURL: "", // Set dynamically by auth handlers
	}

	configs := map[string]*oauth2.Config{
		"saxo": oauthConfig,
	}

	return configs, baseURL, websocketURL, saxoEnv, nil
}

// CreateSaxoAuthClient creates a new SaxoAuthClient with environment configuration
func CreateSaxoAuthClient(logger *slog.Logger) (*SaxoAuthClient, error) {
	configs, baseURL, websocketURL, environment, err := LoadSaxoEnvironmentConfig(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load Saxo configuration: %w", err)
	}

	tokenStorage := NewTokenStorage()
	return NewSaxoAuthClient(configs, baseURL, websocketURL, tokenStorage, environment, logger), nil
}

// SaxoAuthClient implements AuthClient with full legacy functionality
type SaxoAuthClient struct {
	providerConfigs map[string]*oauth2.Config
	environment     SaxoEnvironment
	baseURL         string
	websocketURL    string // Separate WebSocket URL for new streaming domain (Dec 2025)
	tokenStorage    TokenStorage
	tokenUpdated    chan TokenInfo
	currentToken    TokenInfo
	tokenMutex      sync.RWMutex
	logger          *slog.Logger
}

func NewSaxoAuthClient(
	configs map[string]*oauth2.Config,
	baseURL string,
	websocketURL string,
	storage TokenStorage,
	environment SaxoEnvironment,
	logger *slog.Logger,
) *SaxoAuthClient {
	return &SaxoAuthClient{
		providerConfigs: configs,
		baseURL:         baseURL,
		websocketURL:    websocketURL,
		tokenStorage:    storage,
		environment:     environment,
		tokenUpdated:    nil, // CRITICAL: Must be nil so StartAuthenticationKeeper creates it
		logger:          logger,
	}
}

// GetBaseURL returns the base URL for API calls
func (sac *SaxoAuthClient) GetBaseURL() string {
	return sac.baseURL
}

// GetWebSocketURL returns the WebSocket URL for streaming connections
// Following December 2025 breaking change - new streaming domain
func (sac *SaxoAuthClient) GetWebSocketURL() string {
	return sac.websocketURL
}

// GetAccessToken implements AuthClient
func (sac *SaxoAuthClient) GetAccessToken() (string, error) {
	token, err := sac.getValidToken(context.Background())
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// IsAuthenticated implements AuthClient
func (sac *SaxoAuthClient) IsAuthenticated() bool {
	// Use getValidToken which auto-refreshes expired tokens (following legacy pattern)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	token, err := sac.getValidToken(ctx)
	if err != nil {
		return false
	}
	return token.AccessToken != ""
}

// Login implements AuthClient - CLI-friendly OAuth flow with temporary callback server
func (sac *SaxoAuthClient) Login(ctx context.Context) error {
	// Check if already authenticated
	if sac.IsAuthenticated() {
		sac.logger.Info("Already authenticated with valid token")
		return nil
	}

	// CLI mode: Start temporary localhost server for OAuth callback
	sac.logger.Info("Starting CLI OAuth authentication flow")
	return sac.loginCLI(ctx, "saxo")
}

// Logout implements AuthClient
func (sac *SaxoAuthClient) Logout() error {
	sac.logger.Info("Starting logout process")

	// Stop token refresh goroutine
	sac.tokenMutex.Lock()
	if sac.tokenUpdated != nil {
		close(sac.tokenUpdated)
		sac.tokenUpdated = nil
		sac.logger.Info("Token refresh goroutine stopped")
	}
	sac.currentToken = TokenInfo{}
	sac.tokenMutex.Unlock()

	// Clear from file storage
	filename := sac.getTokenFilename("saxo")
	if err := sac.tokenStorage.DeleteToken(filename); err != nil {
		sac.logger.Warn("Failed to delete token file", "error", err)
		// Continue with logout even if file deletion fails
	}

	sac.logger.Info("Logout completed successfully")
	return nil
}

// RefreshToken implements AuthClient with legacy logic
func (sac *SaxoAuthClient) RefreshToken(ctx context.Context) error {
	// CRITICAL: Use cached token directly to avoid circular dependency with getValidToken()
	// The TokenSource.Token() call below will handle checking expiry and refreshing automatically
	sac.tokenMutex.RLock()
	token := sac.currentToken
	sac.tokenMutex.RUnlock()

	// If no cached token, try loading from file
	if token.AccessToken == "" {
		var err error
		token, err = sac.getToken("saxo")
		if err != nil {
			return err
		}
	}

	config := sac.providerConfigs["saxo"]
	if config == nil {
		return fmt.Errorf("no OAuth config for saxo")
	}

	// Create token source and refresh
	// IMPORTANT: TokenSource.Token() automatically checks expiry and refreshes if needed
	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	src := config.TokenSource(ctx, oauthToken)
	newToken, err := src.Token()
	if err != nil {
		sac.logger.Error("Unable to refresh token",
			"function", "RefreshToken",
			"error", err)
		return err
	}

	// Check if token was actually refreshed (access token changed)
	if newToken.AccessToken == token.AccessToken {
		sac.logger.Debug("Token was not refreshed (same access token)",
			"function", "RefreshToken")
		// Still update in case expiry changed
	}

	// Convert and store
	refreshedToken := sac.oauth2ToTokenInfo(*newToken, "saxo")
	if err := sac.storeToken(refreshedToken); err != nil {
		sac.logger.Error("Unable to save refreshed token",
			"function", "RefreshToken",
			"error", err)
		return err
	}

	sac.logger.Info("Token refreshed successfully",
		"function", "RefreshToken",
		"expiry", newToken.Expiry)
	return nil
}

// GetHTTPClient returns configured HTTP client with current token
func (sac *SaxoAuthClient) GetHTTPClient(ctx context.Context) (*http.Client, error) {
	token, err := sac.getValidToken(ctx)
	if err != nil {
		return nil, err
	}

	config := sac.providerConfigs["saxo"]
	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	return config.Client(ctx, oauthToken), nil
}

// StartAuthenticationKeeper starts the token refresh background process
// Following EXACT legacy pattern from pivot-web/broker/oauth.go:235
// This is the ONLY entry point for token management - called ONCE at boot
func (sac *SaxoAuthClient) StartAuthenticationKeeper(provider string) {
	sac.logger.Info("Authentication keeper started",
		"function", "StartAuthenticationKeeper",
		"provider", provider)

	token, err := sac.getValidToken(context.Background())
	if err != nil {
		sac.logger.Warn("Unable to fetch valid token from file, authentication required",
			"function", "StartAuthenticationKeeper",
			"error", err)
		return
	}

	timeToExpiry := time.Until(token.RefreshExpiry) - earlyRefreshTime
	sac.logger.Info("Valid token loaded from file",
		"function", "StartAuthenticationKeeper",
		"expiry", token.Expiry,
		"refresh_in", timeToExpiry)

	// only run this part once (following legacy oauth.go:250)
	if sac.tokenUpdated == nil {
		sac.logger.Debug("Setting up ticker and channel for token refresh",
			"function", "StartAuthenticationKeeper")

		ticker := time.NewTicker(timeToExpiry)
		sac.tokenUpdated = make(chan TokenInfo, 1)

		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_, err := sac.getValidToken(context.Background())
					if err != nil {
						sac.logger.Error("Unable to refresh token",
							"function", "StartAuthenticationKeeper",
							"error", err)
					}
				case newToken, ok := <-sac.tokenUpdated:
					if !ok {
						sac.logger.Info("Token update channel closed, stopping authentication keeper",
							"function", "StartAuthenticationKeeper")
						return
					}
					ticker.Reset(time.Until(newToken.RefreshExpiry) - earlyRefreshTime)
					sac.logger.Info("Token updated, reset refresh timer",
						"function", "StartAuthenticationKeeper",
						"next_refresh_in", time.Until(newToken.RefreshExpiry)-earlyRefreshTime)
				}
			}
		}()
	}
}

// ReauthorizeWebSocket re-authorizes an active WebSocket connection with a refreshed token
// Implements Saxo streaming API: PUT /streaming/ws/authorize?contextid={contextid}
// Expected response: 202 Accepted
// Following legacy broker/oauth.go reauthorizeAndSaveToken pattern with early expiry token source
func (sac *SaxoAuthClient) ReauthorizeWebSocket(ctx context.Context, contextID string) error {
	if contextID == "" {
		return fmt.Errorf("contextID cannot be empty")
	}

	// Get current token (cached or from file)
	// CRITICAL: Use getToken() not getValidToken() to avoid circular refresh
	// The TokenSource below will handle expiry check and refresh automatically!
	token, err := sac.getToken("saxo")
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Build re-authorization URL following pivot-web pattern
	// Parse WebSocket URL and append /authorize
	baseURL, err := url.Parse(sac.websocketURL)
	if err != nil {
		return fmt.Errorf("failed to parse WebSocket URL: %w", err)
	}
	// Change scheme from wss to https for authorization endpoint
	baseURL.Scheme = "https"
	baseURL.Path = baseURL.Path + "/authorize"

	// Add contextID as query parameter
	params := url.Values{}
	params.Set("contextid", contextID)
	baseURL.RawQuery = params.Encode()

	reauthorizeURL := baseURL.String()

	// Create token source with early expiry (2 minutes before actual expiry)
	// This ensures token refresh happens BEFORE WebSocket re-authorization if needed
	// Following legacy pattern: oauth2.ReuseTokenSourceWithExpiry
	// KEY: The TokenSource automatically checks expiry and refreshes if needed!
	oauthToken := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	tokenSource := sac.createTokenSourceWithEarlyExpiry(ctx, oauthToken, earlyRefreshTime)
	client := oauth2.NewClient(ctx, tokenSource)

	// Create PUT request (no body required)
	req, err := http.NewRequestWithContext(ctx, "PUT", reauthorizeURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	// CRITICAL: The oauth2.Client automatically calls tokenSource.Token() before the request
	// If token is expired or within earlyRefreshTime, it refreshes automatically!
	sac.logger.Debug("Sending WebSocket re-authorization PUT request",
		"function", "ReauthorizeWebSocket",
		"url", reauthorizeURL,
		"context_id", contextID)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Saxo returns 202 Accepted for successful re-authorization
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		sac.logger.Error("Re-authorization failed",
			"function", "ReauthorizeWebSocket",
			"status", resp.StatusCode,
			"body", string(body))
		return fmt.Errorf("re-authorization failed with status %d: %s", resp.StatusCode, string(body))
	}

	sac.logger.Info("Re-authorization request successful",
		"function", "ReauthorizeWebSocket",
		"status", resp.StatusCode)

	// Get potentially refreshed token from token source
	// This is critical - if token was refreshed during re-auth, we need to save it
	newToken, err := tokenSource.Token()
	if err != nil {
		sac.logger.Error("Unable to get token after reauthorization",
			"function", "ReauthorizeWebSocket",
			"error", err)
		return err
	}

	// LEGACY PATTERN: Check if the token has actually been refreshed
	// If not refreshed, it's an ERROR because we only call this when token is expiring
	if newToken.AccessToken == token.AccessToken {
		sac.logger.Warn("Token was not refreshed (same access token)",
			"function", "ReauthorizeWebSocket")
		return fmt.Errorf("token was not refreshed")
	}

	sac.logger.Info("New token obtained after reauthorization",
		"function", "ReauthorizeWebSocket",
		"expiry", newToken.Expiry)

	// Store the new token
	refreshedToken := sac.oauth2ToTokenInfo(*newToken, "saxo")
	if err := sac.storeToken(refreshedToken); err != nil {
		sac.logger.Error("Unable to save refreshed token",
			"function", "ReauthorizeWebSocket",
			"error", err)
		return err
	}

	sac.logger.Info("Successfully reauthorized and saved new token",
		"function", "ReauthorizeWebSocket")
	return nil
}

// createTokenSourceWithEarlyExpiry creates a token source that refreshes tokens before actual expiry
// Following legacy broker/oauth.go pattern
func (sac *SaxoAuthClient) createTokenSourceWithEarlyExpiry(ctx context.Context, token *oauth2.Token, earlyExpiry time.Duration) oauth2.TokenSource {
	config := sac.providerConfigs["saxo"]
	baseSource := config.TokenSource(ctx, token)
	return oauth2.ReuseTokenSourceWithExpiry(token, baseSource, earlyExpiry)
}

// Private methods implementing legacy functionality

func (sac *SaxoAuthClient) getToken(provider string) (TokenInfo, error) {
	sac.tokenMutex.RLock()
	// Return cached token if valid
	if sac.currentToken.AccessToken != "" && time.Now().Before(sac.currentToken.Expiry) {
		defer sac.tokenMutex.RUnlock()
		// sac.logger.Printf("getToken: Returning cached token (expires in %v)", time.Until(sac.currentToken.Expiry))
		return sac.currentToken, nil
	}
	sac.tokenMutex.RUnlock()

	// Upgrade to write lock to load from file and update cache
	sac.tokenMutex.Lock()
	defer sac.tokenMutex.Unlock()

	// Double-check after acquiring write lock (another goroutine might have updated)
	if sac.currentToken.AccessToken != "" && time.Now().Before(sac.currentToken.Expiry) {
		// sac.logger.Printf("getToken: Returning cached token after re-check (expires in %v)", time.Until(sac.currentToken.Expiry))
		return sac.currentToken, nil
	}

	// Try to load from file
	filename := sac.getTokenFilename(provider)
	tokenInfo, err := sac.tokenStorage.LoadToken(filename)
	if err != nil {
		sac.logger.Debug("Failed to load token from file",
			"function", "getToken",
			"filename", filename,
			"error", err)
		return TokenInfo{}, err
	}

	// Update cached token with loaded value (we have write lock)
	sac.currentToken = *tokenInfo
	sac.logger.Debug("Loaded token from file and updated cache",
		"function", "getToken",
		"expires_in", time.Until(tokenInfo.Expiry))

	return *tokenInfo, nil
}

func (sac *SaxoAuthClient) getValidToken(ctx context.Context) (TokenInfo, error) {
	token, err := sac.getToken("saxo")
	if err != nil {
		return TokenInfo{}, err
	}

	// Token is valid
	if time.Now().Before(token.Expiry) {
		return token, nil
	}

	// Need to refresh
	sac.logger.Info("Token expired, refreshing",
		"function", "getValidToken",
		"expired_at", token.Expiry)
	if err := sac.RefreshToken(ctx); err != nil {
		return TokenInfo{}, err
	}

	// Return refreshed token
	return sac.getToken("saxo")
}

func (sac *SaxoAuthClient) storeToken(token TokenInfo) error {
	// Update cached token
	sac.tokenMutex.Lock()
	sac.currentToken = token
	sac.tokenMutex.Unlock()

	// Non-blocking channel send
	select {
	case sac.tokenUpdated <- token:
	default:
		sac.logger.Debug("Channel send would block, skipping",
			"function", "storeToken")
	}

	// Store to file
	filename := sac.getTokenFilename(token.Provider)
	return sac.tokenStorage.SaveToken(filename, &token)
}

func (sac *SaxoAuthClient) getTokenFilename(provider string) string {
	// Include environment in filename to separate SIM/LIVE tokens
	return fmt.Sprintf("%s_%s%s", provider, sac.environment, tokenSuffix)
}

func (sac *SaxoAuthClient) oauth2ToTokenInfo(token oauth2.Token, provider string) TokenInfo {
	return TokenInfo{
		AccessToken:   token.AccessToken,
		RefreshToken:  token.RefreshToken,
		Expiry:        token.Expiry,
		RefreshExpiry: sac.calcRefreshTokenExpiry(token),
		Provider:      provider,
	}
}

// calcRefreshTokenExpiry implements legacy logic for refresh token expiry calculation
func (sac *SaxoAuthClient) calcRefreshTokenExpiry(token oauth2.Token) time.Time {
	expiryTime := token.Expiry

	// Get refresh_token_expires_in with proper nil check
	refreshExpiresInRaw := token.Extra("refresh_token_expires_in")
	if refreshExpiresInRaw == nil {
		// Field not present in token response, use 24-hour default
		return expiryTime.Add(24 * time.Hour)
	}

	// Try type assertion first (API may return int or float64)
	var refreshExpiresIn int
	switch v := refreshExpiresInRaw.(type) {
	case int:
		refreshExpiresIn = v
	case float64:
		refreshExpiresIn = int(v)
	case string:
		var err error
		refreshExpiresIn, err = strconv.Atoi(v)
		if err != nil {
			sac.logger.Warn("Error parsing refresh_token_expires_in string, using 24h default",
				"function", "calcRefreshTokenExpiry",
				"value", v,
				"error", err)
			return expiryTime.Add(24 * time.Hour)
		}
	default:
		sac.logger.Warn("Unexpected type for refresh_token_expires_in, using 24h default",
			"function", "calcRefreshTokenExpiry",
			"type", fmt.Sprintf("%T", refreshExpiresInRaw))
		return expiryTime.Add(24 * time.Hour)
	}

	// Get expires_in with proper nil check
	expiresInRaw := token.Extra("expires_in")
	if expiresInRaw == nil {
		// Field not present, use refresh expires in value directly
		return expiryTime.Add(time.Duration(refreshExpiresIn) * time.Second)
	}

	// Try type assertion for expires_in
	var expiresIn int
	switch v := expiresInRaw.(type) {
	case int:
		expiresIn = v
	case float64:
		expiresIn = int(v)
	case string:
		var err error
		expiresIn, err = strconv.Atoi(v)
		if err != nil {
			sac.logger.Warn("Error parsing expires_in string, using refresh_token_expires_in",
				"function", "calcRefreshTokenExpiry",
				"value", v,
				"error", err)
			return expiryTime.Add(time.Duration(refreshExpiresIn) * time.Second)
		}
	default:
		sac.logger.Warn("Unexpected type for expires_in, using refresh_token_expires_in",
			"function", "calcRefreshTokenExpiry",
			"type", fmt.Sprintf("%T", expiresInRaw))
		return expiryTime.Add(time.Duration(refreshExpiresIn) * time.Second)
	}

	// Calculate exact refresh token expiry
	refreshExpiry := expiryTime.Add(time.Duration(refreshExpiresIn-expiresIn) * time.Second)
	return refreshExpiry
}

// GetOAuthConfig returns the OAuth2 config for web handlers
func (sac *SaxoAuthClient) GetOAuthConfig(provider string) *oauth2.Config {
	return sac.providerConfigs[provider]
}

// SetRedirectURL updates the redirect URL for OAuth config (for web handlers)
func (sac *SaxoAuthClient) SetRedirectURL(provider string, redirectURL string) error {
	config := sac.providerConfigs[provider]
	if config == nil {
		return fmt.Errorf("no OAuth config for provider: %s", provider)
	}

	config.RedirectURL = redirectURL
	sac.logger.Debug("Updated OAuth redirect URL",
		"function", "SetRedirectURL",
		"provider", provider,
		"redirect_url", redirectURL)
	return nil
}

// BuildRedirectURL creates redirect URL based on request host (following legacy pattern)
func (sac *SaxoAuthClient) BuildRedirectURL(host string, provider string) string {
	sac.logger.Debug("Building redirect URL from host",
		"function", "BuildRedirectURL",
		"host", host,
		"provider", provider)
	if host == "localhost:3001" {
		return fmt.Sprintf("http://localhost:3001/oauth/%s/callback", provider)
	}
	return fmt.Sprintf("http://%s/oauth/%s/callback", host, provider)
}

// GenerateAuthURL creates OAuth authorization URL with state parameter
func (sac *SaxoAuthClient) GenerateAuthURL(provider string, state string) (string, error) {
	config := sac.providerConfigs[provider]
	if config == nil {
		return "", fmt.Errorf("no OAuth config for provider: %s", provider)
	}

	// Generate authorization URL following legacy pattern
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	// Log environment for debugging (critical for SIM vs LIVE)
	envName := "Unknown"
	switch sac.environment {
	case SaxoSIM:
		envName = "Simulation"
	case SaxoLive:
		envName = "Live Trading"
	}
	sac.logger.Info("Generated authorization URL",
		"function", "GenerateAuthURL",
		"environment", envName,
		"provider", provider)

	return authURL, nil
}

// ExchangeCodeForToken exchanges authorization code for access token (for web flow)
func (sac *SaxoAuthClient) ExchangeCodeForToken(ctx context.Context, code string, provider string) error {
	config := sac.providerConfigs[provider]
	if config == nil {
		return fmt.Errorf("no OAuth config for provider: %s", provider)
	}

	// Exchange code for token following legacy callback pattern
	token, err := config.Exchange(ctx, code)
	if err != nil {
		sac.logger.Error("Failed to exchange authorization code for token",
			"function", "ExchangeCodeForToken",
			"provider", provider,
			"error", err)
		return err
	}

	// Convert and store token using legacy patterns
	tokenInfo := sac.oauth2ToTokenInfo(*token, provider)
	if err := sac.storeToken(tokenInfo); err != nil {
		sac.logger.Error("Failed to save exchanged token",
			"function", "ExchangeCodeForToken",
			"provider", provider,
			"error", err)
		return err
	}

	sac.logger.Info("Token obtained and stored",
		"function", "ExchangeCodeForToken",
		"provider", provider,
		"expiry", token.Expiry)
	return nil
}

// loginCLI implements CLI-friendly OAuth flow with temporary localhost callback server
// This allows CLI applications (examples, fx-collector) to authenticate without manual token generation
func (sac *SaxoAuthClient) loginCLI(ctx context.Context, provider string) error {
	config := sac.providerConfigs[provider]
	if config == nil {
		return fmt.Errorf("no OAuth config for provider: %s", provider)
	}

	// Generate random state for CSRF protection
	state, err := generateRandomState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	// Set redirect URL to localhost
	callbackPort := "8080"
	callbackPath := "/oauth/callback"
	redirectURL := fmt.Sprintf("http://localhost:%s%s", callbackPort, callbackPath)
	config.RedirectURL = redirectURL

	sac.logger.Info("OAuth callback URL configured",
		"function", "loginCLI",
		"callback_url", redirectURL,
		"provider", provider)

	// Generate authorization URL
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline)

	// Channel to receive authorization code
	codeChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Start temporary HTTP server for OAuth callback
	server := &http.Server{Addr: ":" + callbackPort}

	http.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		// Verify state parameter
		if r.URL.Query().Get("state") != state {
			sac.logger.Warn("OAuth callback received invalid state parameter (CSRF protection)",
				"function", "loginCLI",
				"provider", provider)
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errorChan <- fmt.Errorf("invalid state parameter")
			return
		}

		// Get authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			sac.logger.Warn("OAuth callback received no authorization code",
				"function", "loginCLI",
				"provider", provider)
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			errorChan <- fmt.Errorf("no authorization code")
			return
		}

		// Send success response to browser
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
			<html>
			<head><title>Authentication Successful</title></head>
			<body style="font-family: Arial, sans-serif; text-align: center; padding: 50px;">
				<h1 style="color: #4CAF50;">✅ Authentication Successful!</h1>
				<p>You can close this window and return to your terminal.</p>
				<p style="color: #666; font-size: 14px;">Token saved to data/saxo_token.bin</p>
			</body>
			</html>
		`)

		// Send code to channel
		codeChan <- code
	})

	// Start server in background
	go func() {
		sac.logger.Info("Starting temporary OAuth callback server",
			"function", "loginCLI",
			"address", fmt.Sprintf("http://localhost:%s", callbackPort),
			"provider", provider)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Open browser with authorization URL
	sac.logger.Info("Opening browser for authentication",
		"function", "loginCLI",
		"auth_url", authURL,
		"provider", provider)

	if err := openBrowser(authURL); err != nil {
		sac.logger.Warn("Could not open browser automatically",
			"function", "loginCLI",
			"auth_url", authURL,
			"provider", provider,
			"error", err)
	}

	sac.logger.Info("Waiting for authentication callback",
		"function", "loginCLI",
		"provider", provider,
		"timeout", "5 minutes")

	// Wait for callback or timeout
	var code string
	select {
	case code = <-codeChan:
		sac.logger.Info("Authorization code received from callback",
			"function", "loginCLI",
			"provider", provider)
	case err := <-errorChan:
		server.Shutdown(context.Background())
		return fmt.Errorf("authentication failed: %w", err)
	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return fmt.Errorf("authentication timeout (5 minutes)")
	case <-ctx.Done():
		server.Shutdown(context.Background())
		return fmt.Errorf("authentication cancelled")
	}

	// Shutdown callback server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		sac.logger.Debug("Callback server shutdown error (non-critical)",
			"function", "loginCLI",
			"provider", provider,
			"error", err)
	}

	// Exchange authorization code for token
	sac.logger.Info("Exchanging authorization code for access token",
		"function", "loginCLI",
		"provider", provider)
	if err := sac.ExchangeCodeForToken(ctx, code, provider); err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	sac.logger.Info("Authentication successful, token saved",
		"function", "loginCLI",
		"provider", provider)

	// Start authentication keeper for automatic token refresh
	sac.StartAuthenticationKeeper(provider)
	sac.logger.Info("Token refresh manager started",
		"function", "loginCLI",
		"provider", provider,
		"refresh_interval", "58 minutes")

	return nil
}

// generateRandomState creates a cryptographically random state string for OAuth CSRF protection
func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// openBrowser opens the default browser on the user's system (cross-platform)
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin": // macOS
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
