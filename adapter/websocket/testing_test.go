package websocket

import (
	"os"
	"testing"
)

// TestMain sets up test environment following legacy testing patterns
func TestMain(m *testing.M) {
	// Set test environment variables
	os.Setenv("SAXO_ENV", "TEST")
	os.Setenv("USE_MOCKS", "true")

	// Run tests
	code := m.Run()

	// Cleanup
	os.Exit(code)
}
