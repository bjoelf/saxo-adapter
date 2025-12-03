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
	logger.Println("This example demonstrates broker-agnostic authentication")
	logger.Println("using generic interfaces (AuthClient, BrokerClient)")

	// Step 1: Create auth client
	logger.Println("Creating authentication client...")
	var authClient saxo.AuthClient
	var err error
	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Fatalf("Failed to create auth client: %v", err)
	}
	logger.Println("✅ Auth client created successfully")

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Println("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Fatalf("Authentication failed: %v", err)
	}
	logger.Println("✅ Authenticated successfully")
	logger.Println()

	// Step 3: Create broker services (inject authClient)
	logger.Println("Creating broker services...")

	// CreateBrokerServices returns BrokerClient interface
	brokerClient, err := saxo.CreateBrokerServices(authClient, logger)
	if err != nil {
		logger.Fatalf("Failed to create broker services: %v", err)
	}
	logger.Println("✅ Broker services created successfully")
	logger.Println("✅ Authentication successful!")

	// Step 4: Verify authentication by fetching account balance
	balance, err := brokerClient.GetBalance(ctx)
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
	logger.Println("  - BrokerClient interface provides all core methods")
	logger.Println("  - GetBalance() is part of the generic interface")
	logger.Println("  - Easy to swap Saxo for another broker implementation")
	logger.Println()
	logger.Println("Next steps:")
	logger.Println("  - Run examples/place_order to place a test order")
	logger.Println("  - Run examples/websocket_prices to stream real-time prices")
	logger.Println("  - Run examples/historical_data to fetch market data")
}
