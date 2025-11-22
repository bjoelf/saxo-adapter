package saxo

import (
	"os"
	"strconv"
)

// TestConfig manages test environment configuration following legacy pattern
type TestConfig struct {
	UseSIMEnvironment    bool
	SkipIntegrationTests bool
	MockBrokerResponses  bool
	SaxoClientID         string
	SaxoClientSecret     string
	SaxoBaseURL          string
}

// LoadTestConfig loads test configuration from environment variables
// Following legacy .env and YAML config pattern
func LoadTestConfig() TestConfig {
	skipIntegration, _ := strconv.ParseBool(os.Getenv("SKIP_INTEGRATION"))
	useMocks, _ := strconv.ParseBool(os.Getenv("USE_MOCKS"))

	// Default to SIM environment for safety (never LIVE in tests)
	saxoEnv := os.Getenv("SAXO_ENV")
	if saxoEnv == "" {
		saxoEnv = "SIM"
	}

	var baseURL string
	if saxoEnv == "SIM" {
		baseURL = "https://gateway.saxobank.com/sim/openapi"
	} else {
		baseURL = "https://gateway.saxobank.com/openapi" // LIVE - use with extreme caution
	}

	return TestConfig{
		UseSIMEnvironment:    saxoEnv == "SIM",
		SkipIntegrationTests: skipIntegration,
		MockBrokerResponses:  useMocks,
		SaxoClientID:         os.Getenv("SAXO_CLIENT_ID"),
		SaxoClientSecret:     os.Getenv("SAXO_CLIENT_SECRET"),
		SaxoBaseURL:          baseURL,
	}
}

// IsIntegrationTestEnabled checks if integration tests should run
func (tc TestConfig) IsIntegrationTestEnabled() bool {
	return !tc.SkipIntegrationTests && tc.SaxoClientID != "" && tc.SaxoClientSecret != ""
}

// GetSIMCredentials returns SIM environment credentials for testing
func (tc TestConfig) GetSIMCredentials() (clientID, clientSecret, baseURL string) {
	if !tc.UseSIMEnvironment {
		panic("Attempted to get SIM credentials in non-SIM environment - safety check failed")
	}
	return tc.SaxoClientID, tc.SaxoClientSecret, tc.SaxoBaseURL
}
