package saxo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
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
func LoadSaxoEnvironmentConfig(logger *log.Logger) (map[string]*oauth2.Config, string, string, SaxoEnvironment, error) {
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
		logger.Println("✓ Using SIM trading environment")

	case "live":
		authURL = "https://live.logonvalidation.net/authorize"
		tokenURL = "https://live.logonvalidation.net/token"
		baseURL = "https://gateway.saxobank.com/openapi"
		websocketURL = "https://live-streaming.saxobank.com/oapi/streaming/ws"
		saxoEnv = SaxoLive
		logger.Println("⚠️  WARNING: LIVE trading environment - real money at risk!")

	default:
		return nil, "", "", "", fmt.Errorf("invalid SAXO_ENVIRONMENT: %s (must be 'sim' or 'live')", environment)
	}

	// Log configuration
	logger.Printf("Environment: %s", environment)
	logger.Printf("API URL: %s", baseURL)
	logger.Printf("WebSocket URL: %s", websocketURL)

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
func CreateSaxoAuthClient(logger *log.Logger) (*SaxoAuthClient, error) {
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
	logger          *log.Logger
}

func NewSaxoAuthClient(
	configs map[string]*oauth2.Config,
	baseURL string,
	websocketURL string,
	storage TokenStorage,
	environment SaxoEnvironment,
	logger *log.Logger,
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
		sac.logger.Println("✅ Already authenticated with valid token")
		return nil
	}

	// CLI mode: Start temporary localhost server for OAuth callback
	sac.logger.Println("🔐 Starting CLI OAuth authentication flow...")
	return sac.loginCLI(ctx, "saxo")
}

// Logout implements AuthClient
func (sac *SaxoAuthClient) Logout() error {
	sac.tokenMutex.Lock()
	defer sac.tokenMutex.Unlock()

	sac.currentToken = TokenInfo{}

	// Clear from file storage
	filename := sac.getTokenFilename("saxo")
	return sac.tokenStorage.DeleteToken(filename)
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
		sac.logger.Printf("RefreshToken: Unable to refresh token: %v", err)
		return err
	}

	// Check if token was actually refreshed (access token changed)
	if newToken.AccessToken == token.AccessToken {
		sac.logger.Println("RefreshToken: Token was not refreshed (same access token)")
		// Still update in case expiry changed
	}

	// Convert and store
	refreshedToken := sac.oauth2ToTokenInfo(*newToken, "saxo")
	if err := sac.storeToken(refreshedToken); err != nil {
		sac.logger.Printf("RefreshToken: Unable to save refreshed token: %v", err)
		return err
	}

	sac.logger.Printf("RefreshToken: Got new token that expires at %v", newToken.Expiry)
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
	sac.logger.Println("StartAuthenticationKeeper started")

	token, err := sac.getValidToken(context.Background())
	if err != nil {
		sac.logger.Println("StartAuthenticationKeeper: Unable to fetch a valid token from file. Connect has to be called.")
		return
	}

	timeToExpiry := time.Until(token.RefreshExpiry) - earlyRefreshTime
	sac.logger.Printf("StartAuthenticationKeeper: Fetched a valid token from file, expires at %v, refresh in %v",
		token.Expiry, timeToExpiry)

	// only run this part once (following legacy oauth.go:250)
	if sac.tokenUpdated == nil {
		sac.logger.Println("StartAuthenticationKeeper: Setting up ticker and channel for token refresh")

		ticker := time.NewTicker(timeToExpiry)
		sac.tokenUpdated = make(chan TokenInfo, 1)

		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_, err := sac.getValidToken(context.Background())
					if err != nil {
						sac.logger.Println("StartAuthenticationKeeper: Unable to refresh the token :(")
					}
				case newToken, ok := <-sac.tokenUpdated:
					if !ok {
						sac.logger.Println("StartAuthenticationKeeper: Token update channel closed, stopping authentication keeper")
						return
					}
					ticker.Reset(time.Until(newToken.RefreshExpiry) - earlyRefreshTime)
					sac.logger.Printf("StartAuthenticationKeeper: Token updated. Next refresh in: %v",
						time.Until(newToken.RefreshExpiry)-earlyRefreshTime)
				}
			}
		}()
	}
}

// StartTokenEarlyRefresh starts WebSocket-aware token refresh manager
// This works TOGETHER with StartAuthenticationKeeper:
// - StartAuthenticationKeeper: Always runs, refreshes every ~58 min (before refresh token expires)
// - StartTokenEarlyRefresh: Runs when WebSocket active, refreshes every ~18 min (before access token expires)
//
// When WebSocket is connected, this refreshes tokens more frequently (18 min) and re-authorizes
// the WebSocket connection to keep it alive. When WebSocket is disconnected, falls back to
// StartAuthenticationKeeper's 58-minute refresh cycle.
//
// wsConnected: channel receiving WebSocket connection state (true=connected, false=disconnected)
// wsContextID: channel receiving current WebSocket contextID for re-authorization
func (sac *SaxoAuthClient) StartTokenEarlyRefresh(ctx context.Context, wsConnected <-chan bool, wsContextID <-chan string) {
	sac.logger.Println("StartTokenRefresh: Starting WebSocket-aware token refresh manager")

	go func() {
		// Track WebSocket state
		isWebSocketConnected := false
		currentContextID := ""

		// Helper function to calculate next refresh interval
		calculateRefreshInterval := func() time.Duration {
			token, err := sac.getValidToken(context.Background())
			if err != nil {
				sac.logger.Printf("StartTokenRefresh: Unable to get token: %v", err)
				return 5 * time.Minute // Fallback
			}

			if isWebSocketConnected {
				// WebSocket active: refresh before access token expires (20 min)
				// Use earlyRefreshTime (2 min) before expiry = ~18 min
				interval := time.Until(token.Expiry) - earlyRefreshTime

				// CRITICAL: Prevent panic from negative/zero intervals
				if interval <= 0 {
					sac.logger.Printf("StartTokenRefresh: WARNING - Token already expired or expiring soon (expires in %v), refreshing immediately", time.Until(token.Expiry))
					return 1 * time.Second // Refresh almost immediately
				}

				sac.logger.Printf("StartTokenRefresh: WebSocket ACTIVE - token expires at %v (in %v), next refresh in %v (before access token expires)",
					token.Expiry, time.Until(token.Expiry), interval)
				return interval
			}

			// WebSocket inactive: defer to StartAuthenticationKeeper's 58-minute cycle
			// Return a long interval so we don't interfere with the main keeper
			sac.logger.Printf("StartTokenRefresh: WebSocket INACTIVE - token expires at %v (in %v), using long interval (StartAuthenticationKeeper handles refreshes)",
				token.Expiry, time.Until(token.Expiry))
			return 24 * time.Hour // Very long interval when WebSocket is off
		}

		// Initial refresh interval
		ticker := time.NewTicker(calculateRefreshInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				sac.logger.Println("StartTokenRefresh: Context cancelled, stopping token refresh")
				return

			case connected := <-wsConnected:
				// WebSocket state changed
				isWebSocketConnected = connected
				if connected {
					sac.logger.Println("StartTokenRefresh: WebSocket connected - switching to 18min refresh interval")
				} else {
					sac.logger.Println("StartTokenRefresh: WebSocket disconnected - deferring to StartAuthenticationKeeper")
				}
				// Recalculate interval immediately
				ticker.Reset(calculateRefreshInterval())

			case contextID := <-wsContextID:
				// WebSocket context ID updated (connection or reconnection)
				currentContextID = contextID
				sac.logger.Printf("StartTokenRefresh: WebSocket contextID updated: %s", contextID)

			case <-ticker.C:
				// Only refresh if WebSocket is connected
				// When WebSocket is off, StartAuthenticationKeeper handles refreshes
				if !isWebSocketConnected {
					sac.logger.Println("StartTokenRefresh: Skipping refresh - WebSocket inactive")
					ticker.Reset(calculateRefreshInterval())
					continue
				}

				sac.logger.Println("StartTokenRefresh: Timer fired, checking if refresh needed")

				// LEGACY PATTERN: Check if token needs refresh (less than 2 minutes remaining)
				currentToken, err := sac.getToken("saxo")
				if err != nil {
					sac.logger.Printf("StartTokenRefresh: Unable to get current token: %v", err)
					ticker.Reset(1 * time.Minute)
					continue
				}

				timeUntilExpiry := time.Until(currentToken.Expiry)
				if timeUntilExpiry > 2*time.Minute {
					sac.logger.Printf("StartTokenRefresh: Token still valid for %s (>2min), skipping refresh", timeUntilExpiry)
					ticker.Reset(calculateRefreshInterval())
					continue
				}

				// Check if WebSocket connection exists
				if currentContextID == "" {
					sac.logger.Println("StartTokenRefresh: No WebSocket connection to reauthorize")
					ticker.Reset(calculateRefreshInterval())
					continue
				}

				// Perform the token refresh via WebSocket reauthorization
				sac.logger.Println("StartTokenRefresh: Attempting to reauthorize WebSocket connection")
				if err := sac.ReauthorizeWebSocket(context.Background(), currentContextID); err != nil {
					sac.logger.Printf("StartTokenRefresh: Reauthorization failed: %v", err)
					ticker.Reset(1 * time.Minute)
					continue
				}

				sac.logger.Printf("StartTokenRefresh: Token refreshed successfully, new token expires in %s",
					time.Until(sac.currentToken.Expiry))

				// Recalculate interval for next refresh
				ticker.Reset(calculateRefreshInterval())
			}
		}
	}()
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
	sac.logger.Printf("ReauthorizeWebSocket: Sending PUT request to %s", reauthorizeURL)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Saxo returns 202 Accepted for successful re-authorization
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		sac.logger.Printf("ReauthorizeWebSocket: Re-authorization FAILED with status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("re-authorization failed with status %d: %s", resp.StatusCode, string(body))
	}

	sac.logger.Printf("ReauthorizeWebSocket: Re-authorization request successful (status %d)", resp.StatusCode)

	// Get potentially refreshed token from token source
	// This is critical - if token was refreshed during re-auth, we need to save it
	newToken, err := tokenSource.Token()
	if err != nil {
		sac.logger.Printf("ReauthorizeWebSocket: Unable to get token after reauthorization: %v", err)
		return err
	}

	// LEGACY PATTERN: Check if the token has actually been refreshed
	// If not refreshed, it's an ERROR because we only call this when token is expiring
	if newToken.AccessToken == token.AccessToken {
		sac.logger.Println("ReauthorizeWebSocket: Token was not refreshed, it's the same as before")
		return fmt.Errorf("token was not refreshed")
	}

	sac.logger.Printf("ReauthorizeWebSocket: Got a new token that expires at %v", newToken.Expiry)

	// Store the new token
	refreshedToken := sac.oauth2ToTokenInfo(*newToken, "saxo")
	if err := sac.storeToken(refreshedToken); err != nil {
		sac.logger.Printf("ReauthorizeWebSocket: Unable to save the refreshed token to file: %v", err)
		return err
	}

	sac.logger.Println("ReauthorizeWebSocket: Successfully reauthorized and saved new token")
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
		sac.logger.Printf("getToken: Failed to load token from file %s: %v", filename, err)
		return TokenInfo{}, err
	}

	// Update cached token with loaded value (we have write lock)
	sac.currentToken = *tokenInfo
	sac.logger.Printf("getToken: Loaded token from file and updated cache (expires in %v)", time.Until(tokenInfo.Expiry))

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
	sac.logger.Printf("getValidToken: Token expired at %v, refreshing", token.Expiry)
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
		sac.logger.Println("storeToken: Channel send would block, skipping")
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
			sac.logger.Printf("calcRefreshTokenExpiry: Error parsing refresh_token_expires_in string: %v", err)
			return expiryTime.Add(24 * time.Hour)
		}
	default:
		sac.logger.Printf("calcRefreshTokenExpiry: Unexpected type for refresh_token_expires_in: %T", refreshExpiresInRaw)
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
			sac.logger.Printf("calcRefreshTokenExpiry: Error parsing expires_in string: %v", err)
			return expiryTime.Add(time.Duration(refreshExpiresIn) * time.Second)
		}
	default:
		sac.logger.Printf("calcRefreshTokenExpiry: Unexpected type for expires_in: %T", expiresInRaw)
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
	sac.logger.Printf("SetRedirectURL: Updated redirect URL to %s", redirectURL)
	return nil
}

// BuildRedirectURL creates redirect URL based on request host (following legacy pattern)
func (sac *SaxoAuthClient) BuildRedirectURL(host string, provider string) string {
	sac.logger.Printf("BuildRedirectURL: Request host: %s", host)
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
	sac.logger.Printf("GenerateAuthURL: Generated auth URL for %s environment", envName)

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
		sac.logger.Printf("ExchangeCodeForToken: Token exchange failed: %v", err)
		return err
	}

	// Convert and store token using legacy patterns
	tokenInfo := sac.oauth2ToTokenInfo(*token, provider)
	if err := sac.storeToken(tokenInfo); err != nil {
		sac.logger.Printf("ExchangeCodeForToken: Unable to save token: %v", err)
		return err
	}

	sac.logger.Printf("ExchangeCodeForToken: Token obtained, expires at %v", token.Expiry)
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

	sac.logger.Printf("📍 OAuth callback URL: %s", redirectURL)

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
			sac.logger.Printf("❌ OAuth callback: Invalid state parameter")
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errorChan <- fmt.Errorf("invalid state parameter")
			return
		}

		// Get authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			sac.logger.Printf("❌ OAuth callback: No authorization code received")
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
		sac.logger.Printf("🌐 Starting temporary callback server on http://localhost:%s", callbackPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Open browser with authorization URL
	sac.logger.Println("🌐 Opening browser for authentication...")
	sac.logger.Printf("📋 If browser doesn't open, visit this URL manually:")
	sac.logger.Printf("   %s", authURL)
	sac.logger.Println()

	if err := openBrowser(authURL); err != nil {
		sac.logger.Printf("⚠️  Could not open browser automatically: %v", err)
		sac.logger.Println("📋 Please open the URL above manually in your browser")
	}

	sac.logger.Println("⏳ Waiting for authentication callback...")

	// Wait for callback or timeout
	var code string
	select {
	case code = <-codeChan:
		sac.logger.Println("✅ Authorization code received")
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
		sac.logger.Printf("⚠️  Server shutdown error (non-critical): %v", err)
	}

	// Exchange authorization code for token
	sac.logger.Println("🔄 Exchanging authorization code for access token...")
	if err := sac.ExchangeCodeForToken(ctx, code, provider); err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	sac.logger.Println("✅ Authentication successful! Token saved.")
	sac.logger.Println()

	// Start authentication keeper for automatic token refresh
	sac.StartAuthenticationKeeper(provider)
	sac.logger.Println("🔄 Token refresh manager started (auto-refresh every 58 minutes)")

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
