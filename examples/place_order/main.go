package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

func main() {
	// Create a logger
	logger := log.New(os.Stdout, "[ORDER-EXAMPLE] ", log.LstdFlags)

	logger.Println("=== Saxo Adapter - Place Order Example ===")
	logger.Println()
	logger.Println("This example demonstrates broker-agnostic order placement")
	logger.Println("using generic interfaces (BrokerClient, OrderRequest)")
	logger.Println()

	// Step 1: Create broker services
	logger.Println("Creating broker services...")

	var authClient saxo.AuthClient
	var brokerClient saxo.BrokerClient
	var err error

	authClient, brokerClient, err = saxo.CreateBrokerServices(logger)
	if err != nil {
		logger.Fatalf("Failed to create broker services: %v", err)
	}

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Println("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Fatalf("Authentication failed: %v", err)
	}
	logger.Println("✅ Authenticated successfully")
	logger.Println()

	// Step 3: Get current balance using generic BrokerClient interface
	balance, err := brokerClient.GetBalance(true)
	if err != nil {
		logger.Fatalf("Failed to get balance: %v", err)
	}
	fmt.Printf("💰 Current Balance: %.2f %s\n", balance.TotalValue, balance.Currency)
	logger.Println()

	// Step 4: Prepare order using generic Instrument and OrderRequest types
	// These are broker-agnostic - same code works with any broker!
	order := saxo.OrderRequest{
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

	logger.Println("Order Details:")
	fmt.Printf("  Instrument: %s (ID: %d)\n", order.Instrument.Ticker, order.Instrument.Identifier)
	fmt.Printf("  Side:       %s\n", order.Side)
	fmt.Printf("  Size:       %d units\n", order.Size)
	fmt.Printf("  Type:       %s\n", order.OrderType)
	fmt.Printf("  Duration:   %s\n", order.Duration)
	logger.Println()

	// Step 5: Place order using generic BrokerClient.PlaceOrder()
	// This method signature is the same for ALL brokers!
	logger.Println("Placing order...")
	response, err := brokerClient.PlaceOrder(ctx, order)
	if err != nil {
		logger.Fatalf("❌ Order placement failed: %v", err)
	}

	// Generic OrderResponse type
	logger.Println("✅ Order placed successfully!")
	fmt.Printf("  Order ID:   %s\n", response.OrderID)
	fmt.Printf("  Status:     %s\n", response.Status)
	logger.Println()

	// Step 6: Wait for order to process
	logger.Println("Waiting for order to process...")
	time.Sleep(2 * time.Second)

	// Step 7: Get open orders using generic BrokerClient.GetOpenOrders()
	logger.Println("Fetching open orders...")
	openOrders, err := brokerClient.GetOpenOrders(ctx)
	if err != nil {
		logger.Printf("⚠️  Failed to get open orders: %v", err)
	} else {
		logger.Printf("📋 Open Orders: %d\n", len(openOrders))
		for i, o := range openOrders {
			// Generic LiveOrder type
			fmt.Printf("  %d. %s: %s %.0f @ %.5f (%s)\n",
				i+1, o.OrderID, o.BuySell, o.Amount, o.Price, o.Status)
		}
	}
	logger.Println()

	logger.Println("=== Place Order Example Complete ===")
	logger.Println()
	logger.Println("Key Takeaways:")
	logger.Println("  - OrderRequest is a generic, broker-agnostic type")
	logger.Println("  - BrokerClient.PlaceOrder() interface is the same for all brokers")
	logger.Println("  - OrderResponse is generic - no Saxo-specific details leaked")
	logger.Println("  - Same code works with Interactive Brokers, Alpaca, etc.")
	logger.Println()
	logger.Println("Next steps:")
	logger.Println("  - Check your broker account for the executed order")
	logger.Println("  - Run examples/websocket_prices to monitor real-time prices")
	logger.Println("  - Try different order types (Limit, Stop, etc.)")
}
