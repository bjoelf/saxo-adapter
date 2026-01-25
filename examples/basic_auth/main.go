package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("=== Saxo Adapter - Basic Authentication Example ===")
	logger.Info("This example demonstrates broker-agnostic authentication")
	logger.Info("using generic interfaces (AuthClient, BrokerClient)")

	// Step 1: Create auth client
	logger.Info("Creating authentication client...")
	var authClient saxo.AuthClient
	var err error
	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Error("Failed to create auth client: %v", "error", err); os.Exit(1)
	}
	logger.Info("✅ Auth client created successfully")

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Info("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Error("Authentication failed: %v", "error", err); os.Exit(1)
	}
	logger.Info("✅ Authenticated successfully")
	logger.Info("")

	// Step 3: Create broker services (inject authClient)
	logger.Info("Creating broker services...")

	// CreateBrokerServices returns BrokerClient interface
	brokerClient, err := saxo.CreateBrokerServices(authClient, logger)
	if err != nil {
		logger.Error("Failed to create broker services: %v", "error", err); os.Exit(1)
	}
	logger.Info("✅ Broker services created successfully")
	logger.Info("✅ Authentication successful!")

	// Step 4: Verify authentication by fetching account balance
	balance, err := brokerClient.GetBalance(ctx)
	if err != nil {
		logger.Error("Failed to get balance: %v", "error", err); os.Exit(1)
	}

	// Step 5: Display account information
	logger.Info("")
	logger.Info("=== Account Information ===")
	fmt.Printf("💰 Account Balance: %.2f %s\n", balance.TotalValue, balance.Currency)
	fmt.Printf("📊 Cash Balance:    %.2f %s\n", balance.CashBalance, balance.Currency)
	logger.Info("")

	// Step 6: Check authentication status using interface method
	logger.Info("Checking authentication status...")
	if authClient.IsAuthenticated() {
		logger.Info("✅ Session is authenticated and active")
	} else {
		logger.Info("⚠️  Session is not authenticated")
	}

	logger.Info("")
	logger.Info("=== Authentication Example Complete ===")
	logger.Info("")
	logger.Info("Key Takeaways:")
	logger.Info("  - Application code uses generic AuthClient interface")
	logger.Info("  - AuthClient.Login() works with any broker")
	logger.Info("  - BrokerClient interface provides all core methods")
	logger.Info("  - GetBalance() is part of the generic interface")
	logger.Info("  - Easy to swap Saxo for another broker implementation")
	logger.Info("")
	logger.Info("Next steps:")
	logger.Info("  - Run examples/place_order to place a test order")
	logger.Info("  - Run examples/websocket_prices to stream real-time prices")
	logger.Info("  - Run examples/historical_data to fetch market data")
}
