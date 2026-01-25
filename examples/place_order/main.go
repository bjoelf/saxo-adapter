package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("=== Saxo Adapter - Place Order Example ===")
	logger.Info("This example demonstrates broker-agnostic order placement")
	logger.Info("using generic interfaces (BrokerClient, OrderRequest)")

	// Step 1: Create auth client
	logger.Info("Creating authentication client...")
	var authClient saxo.AuthClient
	var err error
	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Error("Failed to create auth client: %v", "error", err)
		os.Exit(1)
	}

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Info("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Error("Authentication failed: %v", "error", err)
		os.Exit(1)
	}
	logger.Info("✅ Authenticated successfully")
	logger.Info("")

	// Step 3: Create broker services (inject authClient)
	logger.Info("Creating broker services...")

	// CreateBrokerServices returns BrokerClient interface
	brokerClient, err := saxo.CreateBrokerServices(authClient, logger)
	if err != nil {
		logger.Error("Failed to create broker services: %v", "error", err)
		os.Exit(1)
	}
	logger.Info("✅ Broker services created successfully")
	logger.Info("")

	// Step 4: Get accounts to retrieve AccountKey
	accounts, err := brokerClient.GetAccounts(ctx)
	if err != nil {
		logger.Error("Failed to get accounts: %v", "error", err)
		os.Exit(1)
	}
	if len(accounts.Data) == 0 {
		logger.Error("No accounts found")
		os.Exit(1)
	}
	accountKey := accounts.Data[0].AccountKey
	logger.Info("Using account", "account_key", accountKey)
	logger.Info("")

	// Step 5: Get current balance
	balance, err := brokerClient.GetBalance(ctx)
	if err != nil {
		logger.Error("Failed to get balance: %v", "error", err)
		os.Exit(1)
	}
	fmt.Printf("💰 Current Balance: %.2f %s\n", balance.TotalValue, balance.Currency)
	logger.Info("")

	// Step 6: Prepare order using generic Instrument and OrderRequest types
	// These are broker-agnostic - same code works with any broker!
	order := saxo.OrderRequest{
		AccountKey: accountKey, // Required: account identifier
		Instrument: saxo.Instrument{
			Ticker:     "EURUSD",
			Identifier: 21, // UIC for EURUSD (broker-specific ID)
			AssetType:  "FxSpot",
		},
		Side:      "Buy",
		Size:      1000,     // Small test size (1,000 units)
		OrderType: "Market", // Market order for immediate execution
		Duration:  "DayOrder",
	}

	logger.Info("Order Details:")
	fmt.Printf("  Instrument: %s (ID: %d)\n", order.Instrument.Ticker, order.Instrument.Identifier)
	fmt.Printf("  Side:       %s\n", order.Side)
	fmt.Printf("  Size:       %d units\n", order.Size)
	fmt.Printf("  Type:       %s\n", order.OrderType)
	fmt.Printf("  Duration:   %s\n", order.Duration)
	logger.Info("")

	// Step 7: Place order using generic BrokerClient.PlaceOrder()
	// This method signature is the same for ALL brokers!
	logger.Info("Placing order...")
	response, err := brokerClient.PlaceOrder(ctx, order)
	if err != nil {
		logger.Error("❌ Order placement failed: %v", "error", err)
		os.Exit(1)
	}

	// Generic OrderResponse type
	logger.Info("✅ Order placed successfully!")
	fmt.Printf("  Order ID:   %s\n", response.OrderID)
	fmt.Printf("  Status:     %s\n", response.Status)
	logger.Info("")

	// Step 8: Wait for order to process
	logger.Info("Waiting for order to process...")
	time.Sleep(2 * time.Second)

	// Step 9: Get open orders using generic BrokerClient.GetOpenOrders()
	logger.Info("Fetching open orders...")
	openOrders, err := brokerClient.GetOpenOrders(ctx)
	if err != nil {
		logger.Warn("Failed to get open orders", "error", err)
	} else {
		fmt.Printf("📋 Open Orders: %d\n", len(openOrders))
		for i, o := range openOrders {
			// Generic LiveOrder type
			fmt.Printf("  %d. %s: %s %.0f @ %.5f (%s)\n",
				i+1, o.OrderID, o.BuySell, o.Amount, o.Price, o.Status)
		}
	}
	logger.Info("")

	logger.Info("=== Place Order Example Complete ===")
	logger.Info("")
	logger.Info("Key Takeaways:")
	logger.Info("  - OrderRequest is a generic, broker-agnostic type")
	logger.Info("  - BrokerClient.PlaceOrder() interface is the same for all brokers")
	logger.Info("  - OrderResponse is generic - no Saxo-specific details leaked")
	logger.Info("  - Same code works with Interactive Brokers, Alpaca, etc.")
	logger.Info("")
	logger.Info("Next steps:")
	logger.Info("  - Check your broker account for the executed order")
	logger.Info("  - Run examples/websocket_prices to monitor real-time prices")
	logger.Info("  - Try different order types (Limit, Stop, etc.)")
}
