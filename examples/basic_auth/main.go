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

	// Step 1: Create auth client
	logger.Println("Creating authentication client...")

	var authClient saxo.AuthClient
	var err error

	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Fatalf("Failed to create auth client: %v", err)
	}
	logger.Println("✅ Auth client created successfully")

	// Step 2: Create broker services (inject authClient)
	logger.Println("Creating broker services...")

	// CreateBrokerServices returns BrokerClient interface
	brokerClientInterface, err := saxo.CreateBrokerServices(authClient, logger)
	if err != nil {
		logger.Fatalf("Failed to create broker services: %v", err)
	}

	// Type assert to concrete *SaxoBrokerClient to access Saxo-specific methods
	// Generic BrokerClient interface only has core trading methods
	// Saxo-specific methods like GetBalance() are on the concrete type
	saxoBrokerClient, ok := brokerClientInterface.(*saxo.SaxoBrokerClient)
	if !ok {
		logger.Fatalf("Failed to cast to *SaxoBrokerClient")
	}
	logger.Println("✅ Broker services created successfully")

	// Step 3: Authenticate with broker
	// From here on, we only use the AuthClient interface - broker-agnostic!
	ctx := context.Background()
	logger.Println()
	logger.Println("Starting authentication...")
	logger.Println("⚠️  A browser window will open for broker login")

	if err := authClient.Login(ctx); err != nil {
		logger.Fatalf("Authentication failed: %v", err)
	}

	logger.Println("✅ Authentication successful!")

	// Step 4: Verify authentication by fetching account balance
	// Now uses generic BrokerClient interface
	logger.Println()
	logger.Println("Verifying authentication by fetching account balance...")

	balance, err := saxoBrokerClient.GetBalance(ctx)
	if err != nil {
		logger.Fatalf("Failed to get balance: %v", err)
	}

	// Step 5: Display account information
	logger.Println()
	logger.Println("=== Account Information ===")
	fmt.Printf("💰 Account Balance: %.2f %s\n", balance.TotalValue, balance.Currency)
	fmt.Printf("📊 Cash Balance:    %.2f %s\n", balance.CashBalance, balance.Currency)
	logger.Println()

	// Step 6: Check authentication status using interface method
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
	logger.Println("  - Application code uses generic AuthClient interface")
	logger.Println("  - AuthClient.Login() works with any broker")
	logger.Println("  - Core trading methods use generic BrokerClient interface")
	logger.Println("  - Broker-specific methods (GetBalance) require concrete type")
	logger.Println("  - Easy to swap Saxo for another broker implementation")
	logger.Println()
	logger.Println("Next steps:")
	logger.Println("  - Run examples/place_order to place a test order")
	logger.Println("  - Run examples/websocket_prices to stream real-time prices")
}
