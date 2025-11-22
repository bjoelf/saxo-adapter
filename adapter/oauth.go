package saxo

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/bjoelf/pivot-web2/internal/adapters/storage"
	"github.com/bjoelf/pivot-web2/internal/ports"
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
		environment = "sim" // Default to SIM for safety following legacy pattern
	}

	var clientID, clientSecret, authURL, tokenURL, baseURL, websocketURL string
	var saxoEnv SaxoEnvironment

	// Load environment-specific credentials following legacy broker/oauth.go pattern
	// WebSocket URLs updated for December 2025 breaking change (new streaming domains)
	switch environment {
	case "sim":
		clientID = os.Getenv("SIM_CLIENT_ID")
		clientSecret = os.Getenv("SIM_CLIENT_SECRET")
		authURL = "https://sim.logonvalidation.net/authorize"
		tokenURL = "https://sim.logonvalidation.net/token"
		baseURL = "https://gateway.saxobank.com/sim/openapi"
		websocketURL = "https://sim-streaming.saxobank.com/sim/oapi/streaming/ws" // New streaming domain (Dec 2025)
		saxoEnv = SaxoSIM
		logger.Println("✓ Using SIM trading environment")

	case "live":
		clientID = os.Getenv("LIVE_CLIENT_ID")
		clientSecret = os.Getenv("LIVE_CLIENT_SECRET")
		authURL = "https://live.logonvalidation.net/authorize"
		tokenURL = "https://live.logonvalidation.net/token"
		baseURL = "https://gateway.saxobank.com/openapi"
		websocketURL = "https://live-streaming.saxobank.com/oapi/streaming/ws" // New streaming domain (Dec 2025)
		saxoEnv = SaxoLive
		logger.Println("⚠️  WARNING: Configured for LIVE trading environment - real money at risk!")

	default:
		return nil, "", "", "", fmt.Errorf("invalid SAXO_ENVIRONMENT: %s (must be 'sim' or 'live')", environment)
	}

	// Validate environment-specific credentials (following legacy validation pattern)
	if clientID == "" {
		return nil, "", "", "", fmt.Errorf("missing %s OAuth credentials not set for %s environment", environment, environment)
	}
	if clientSecret == "" {
		return nil, "", "", "", fmt.Errorf("missing %s OAuth credentials not set for %s environment", environment, environment)
	}

	// Log environment configuration for debugging (following legacy logging pattern)
	logger.Printf("Saxo Environment: %s", environment)
	logger.Printf("Client ID: %s", maskClientID(clientID))
	logger.Printf("Auth URL: %s", authURL)
	logger.Printf("API Base URL: %s", baseURL)
	logger.Printf("WebSocket URL: %s", websocketURL)

	// Create OAuth2 configuration (RedirectURL will be set dynamically by auth handlers based on request)
	oauthConfig := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"openapi"}, // Saxo Bank OpenAPI scope
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		RedirectURL: "", // Will be set dynamically by auth handlers using SetRedirectURL()
	}

	// Return config map that works with existing NewSaxoAuthClient constructor
	provider := os.Getenv("PROVIDER")
	if provider == "" {
		provider = "saxo"
	}

	configs := map[string]*oauth2.Config{
		provider: oauthConfig,
	}

	return configs, baseURL, websocketURL, saxoEnv, nil
}

// maskClientID masks client ID for logging security (following legacy security pattern)
func maskClientID(clientID string) string {
	if len(clientID) <= 8 {
		return "****"
	}
	return clientID[:4] + "****" + clientID[len(clientID)-4:]
}

// CreateSaxoAuthClient creates a new SaxoAuthClient with environment configuration
func CreateSaxoAuthClient(logger *log.Logger) (*SaxoAuthClient, error) {
	configs, baseURL, websocketURL, environment, err := LoadSaxoEnvironmentConfig(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load Saxo configuration: %w", err)
	}

	storage := storage.NewTokenStorage()
	return NewSaxoAuthClient(configs, baseURL, websocketURL, storage, environment, logger), nil
}

// SaxoAuthClient implements ports.AuthClient with full legacy functionality
type SaxoAuthClient struct {
	providerConfigs map[string]*oauth2.Config
	environment     SaxoEnvironment
	baseURL         string
	websocketURL    string // Separate WebSocket URL for new streaming domain (Dec 2025)
	tokenStorage    ports.TokenStorage
	tokenUpdated    chan ports.TokenInfo
	currentToken    ports.TokenInfo
	tokenMutex      sync.RWMutex
	logger          *log.Logger
}

func NewSaxoAuthClient(
	configs map[string]*oauth2.Config,
	baseURL string,
	websocketURL string,
	storage ports.TokenStorage,
	environment SaxoEnvironment,
	logger *log.Logger,
) *SaxoAuthClient {
	return &SaxoAuthClient{
		providerConfigs: configs,
		baseURL:         baseURL,
		websocketURL:    websocketURL,
		tokenStorage:    storage,
		environment:     environment,
		tokenUpdated:    make(chan ports.TokenInfo, 1),
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

// GetAccessToken implements ports.AuthClient
func (sac *SaxoAuthClient) GetAccessToken() (string, error) {
	token, err := sac.getValidToken(context.Background())
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// IsAuthenticated implements ports.AuthClient
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

// Login implements ports.AuthClient - generates OAuth URL for redirect

func (sac *SaxoAuthClient) Login(ctx context.Context) error {
	// This will be called by web handlers for redirect-based login
	return fmt.Errorf("use Connect method for OAuth flow")
}

// Logout implements ports.AuthClient
func (sac *SaxoAuthClient) Logout() error {
	sac.tokenMutex.Lock()
	defer sac.tokenMutex.Unlock()

	sac.currentToken = ports.TokenInfo{}

	// Clear from file storage
	filename := sac.getTokenFilename("saxo")
	return sac.tokenStorage.DeleteToken(filename)
}

// RefreshToken implements ports.AuthClient with legacy logic
func (sac *SaxoAuthClient) RefreshToken(ctx context.Context) error {
	token, err := sac.getToken("saxo")
	if err != nil {
		return err
	}

	config := sac.providerConfigs["saxo"]
	if config == nil {
		return fmt.Errorf("no OAuth config for saxo")
	}

	// Create token source and refresh
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
		sac.tokenUpdated = make(chan ports.TokenInfo, 1)

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
					sac.logger.Printf("StartTokenRefresh: WARNING - Token already expired or expiring soon, refreshing immediately")
					return 1 * time.Second // Refresh almost immediately
				}

				sac.logger.Printf("StartTokenRefresh: WebSocket ACTIVE - next refresh in %v (before access token expires)", interval)
				return interval
			}

			// WebSocket inactive: defer to StartAuthenticationKeeper's 58-minute cycle
			// Return a long interval so we don't interfere with the main keeper
			sac.logger.Printf("StartTokenRefresh: WebSocket INACTIVE - using long interval (StartAuthenticationKeeper handles refreshes)")
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

				// Time to refresh token
				sac.logger.Println("StartTokenRefresh: Refreshing token for WebSocket...")

				if err := sac.RefreshToken(context.Background()); err != nil {
					sac.logger.Printf("StartTokenRefresh: Token refresh failed: %v", err)
					// Retry sooner on failure
					ticker.Reset(1 * time.Minute)
					continue
				}

				sac.logger.Println("StartTokenRefresh: ✓ Token refreshed successfully")

				// Re-authorize WebSocket with new token
				if currentContextID != "" {
					sac.logger.Printf("StartTokenRefresh: Re-authorizing WebSocket (contextID: %s)", currentContextID)
					if err := sac.ReauthorizeWebSocket(context.Background(), currentContextID); err != nil {
						sac.logger.Printf("StartTokenRefresh: WebSocket re-authorization failed: %v", err)
					} else {
						sac.logger.Println("StartTokenRefresh: ✓ WebSocket re-authorized with new token")
					}
				}

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

	// Get current token
	token, err := sac.getValidToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get valid token: %w", err)
	}

	// Build re-authorization URL
	reauthorizeURL := fmt.Sprintf("%s/streaming/ws/authorize?contextid=%s", sac.websocketURL, contextID)

	// Create token source with early expiry (2 minutes before actual expiry)
	// This ensures token refresh happens BEFORE WebSocket re-authorization if needed
	// Following legacy pattern: oauth2.ReuseTokenSourceWithExpiry
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
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Saxo returns 202 Accepted for successful re-authorization
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("re-authorization failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Get potentially refreshed token from token source
	// This is critical - if token was refreshed during re-auth, we need to save it
	newToken, err := tokenSource.Token()
	if err != nil {
		sac.logger.Printf("ReauthorizeWebSocket: Unable to get token after reauthorization: %v", err)
		return err
	}

	// Check if token was actually refreshed (access token changed)
	if newToken.AccessToken != token.AccessToken {
		sac.logger.Println("ReauthorizeWebSocket: Token was refreshed during re-authorization, saving new token")
		refreshedToken := sac.oauth2ToTokenInfo(*newToken, "saxo")
		if err := sac.storeToken(refreshedToken); err != nil {
			sac.logger.Printf("ReauthorizeWebSocket: Warning - failed to save refreshed token: %v", err)
			// Continue anyway - re-authorization succeeded
		} else {
			sac.logger.Printf("ReauthorizeWebSocket: New token saved, expires at %v", newToken.Expiry)
		}
	}

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

func (sac *SaxoAuthClient) getToken(provider string) (ports.TokenInfo, error) {
	sac.tokenMutex.RLock()
	defer sac.tokenMutex.RUnlock()

	// Return cached token if valid
	if sac.currentToken.AccessToken != "" && time.Now().Before(sac.currentToken.Expiry) {
		return sac.currentToken, nil
	}

	// Try to load from file
	filename := sac.getTokenFilename(provider)
	return sac.tokenStorage.LoadToken(filename)
}

func (sac *SaxoAuthClient) getValidToken(ctx context.Context) (ports.TokenInfo, error) {
	token, err := sac.getToken("saxo")
	if err != nil {
		return ports.TokenInfo{}, err
	}

	// Token is valid
	if time.Now().Before(token.Expiry) {
		return token, nil
	}

	// Need to refresh
	sac.logger.Printf("getValidToken: Token expired at %v, refreshing", token.Expiry)
	if err := sac.RefreshToken(ctx); err != nil {
		return ports.TokenInfo{}, err
	}

	// Return refreshed token
	return sac.getToken("saxo")
}

func (sac *SaxoAuthClient) storeToken(token ports.TokenInfo) error {
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
	return sac.tokenStorage.SaveToken(filename, token)
}

func (sac *SaxoAuthClient) getTokenFilename(provider string) string {
	// Include environment in filename to separate SIM/LIVE tokens
	return fmt.Sprintf("%s_%s%s", provider, sac.environment, tokenSuffix)
}

func (sac *SaxoAuthClient) oauth2ToTokenInfo(token oauth2.Token, provider string) ports.TokenInfo {
	return ports.TokenInfo{
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

	refreshExpiresInStr := token.Extra("refresh_token_expires_in")
	refreshExpiresIn, err := strconv.Atoi(fmt.Sprintf("%v", refreshExpiresInStr))
	if err != nil {
		sac.logger.Printf("calcRefreshTokenExpiry: Error parsing refresh_token_expires_in: %v", err)
		// Fallback: assume refresh token lasts 24 hours beyond access token
		return expiryTime.Add(24 * time.Hour)
	}

	expiresInStr := token.Extra("expires_in")
	expiresIn, err := strconv.Atoi(fmt.Sprintf("%v", expiresInStr))
	if err != nil {
		sac.logger.Printf("calcRefreshTokenExpiry: Error parsing expires_in: %v", err)
		// Use the refresh expires in value directly
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
