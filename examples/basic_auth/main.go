package main

import (
"context"
"fmt"
"log"
"os"

saxo "github.com/bjoelf/saxo-adapter/adapter"
)

func main() {
// Create a logger
logger := log.New(os.Stdout, "[AUTH-EXAMPLE] ", log.LstdFlags)

logger.Println("=== Saxo Adapter - Basic Authentication Example ===")
logger.Println()
logger.Println("This example demonstrates broker-agnostic authentication")
logger.Println("using generic interfaces (AuthClient, BrokerClient)")
logger.Println()

// Step 1: Create broker services
// Note: We use the factory function here for convenience, but the rest of the
// code uses only generic interfaces. This allows swapping brokers later.
logger.Println("Creating broker services...")

var authClient saxo.AuthClient
var brokerClient saxo.BrokerClient
var err error

// Factory function creates Saxo-specific implementations
// but returns them as generic interface types
authClient, brokerClient, err = saxo.CreateBrokerServices(logger)
if err != nil {
logger.Fatalf("Failed to create broker services: %v", err)
}
logger.Println("✅ Broker services created successfully")

// Step 2: Authenticate with broker
// From here on, we only use the AuthClient interface - broker-agnostic!
ctx := context.Background()
logger.Println()
logger.Println("Starting authentication...")
logger.Println("⚠️  A browser window will open for broker login")

if err := authClient.Login(ctx); err != nil {
logger.Fatalf("Authentication failed: %v", err)
}

logger.Println("✅ Authentication successful!")

// Step 3: Verify authentication by fetching account balance
// Using BrokerClient interface - works with any broker implementation
logger.Println()
logger.Println("Verifying authentication by fetching account balance...")

balance, err := brokerClient.GetBalance(true)
if err != nil {
logger.Fatalf("Failed to get balance: %v", err)
}

// Step 4: Display account information
logger.Println()
logger.Println("=== Account Information ===")
fmt.Printf("💰 Account Balance: %.2f %s\n", balance.TotalValue, balance.Currency)
fmt.Printf("📊 Cash Balance:    %.2f %s\n", balance.CashBalance, balance.Currency)
logger.Println()

// Step 5: Check authentication status using interface method
logger.Println("Checking authentication status...")
if authClient.IsAuthenticated() {
logger.Println("✅ Session is authenticated and active")
} else {
logger.Println("⚠️  Session is not authenticated")
}

logger.Println()
logger.Println("=== Authentication Example Complete ===")
logger.Println()
logger.Println("Key Takeaways:")
logger.Println("  - Application code uses only generic interfaces")
logger.Println("  - AuthClient.Login() works with any broker")
logger.Println("  - BrokerClient.GetBalance() is broker-agnostic")
logger.Println("  - Easy to swap Saxo for another broker implementation")
logger.Println()
logger.Println("Next steps:")
logger.Println("  - Run examples/place_order to place a test order")
logger.Println("  - Run examples/websocket_prices to stream real-time prices")
}
