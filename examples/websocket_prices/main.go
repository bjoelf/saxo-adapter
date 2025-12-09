package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
	"github.com/bjoelf/saxo-adapter/adapter/websocket"
)

func main() {
	// Create a logger
	logger := log.New(os.Stdout, "[WEBSOCKET-EXAMPLE] ", log.LstdFlags)

	logger.Println("=== Saxo Adapter - WebSocket Price Subscription Example ===")
	logger.Println("This example demonstrates broker-agnostic real-time data streaming")
	logger.Println("using the generic WebSocketClient interface")

	// Step 1: Create auth client
	logger.Println("Creating authentication client...")
	var authClient saxo.AuthClient
	var err error
	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Fatalf("Failed to create auth client: %v", err)
	}

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Println("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Fatalf("Authentication failed: %v", err)
	}
	logger.Println("✅ Authenticated successfully")
	logger.Println()

	// Step 3: Create WebSocket client using generic interface
	logger.Println("Creating websocket client...")

	// Note: We create using websocket package, but use via saxo.WebSocketClient interface
	wsClient := saxo.WebSocketClient(websocket.NewSaxoWebSocketClient(
		authClient,
		authClient.GetBaseURL(),
		authClient.GetWebSocketURL(),
		logger,
	))

	// Step 4: Connect using generic WebSocketClient.Connect()
	logger.Println("Connecting to WebSocket...")
	if err := wsClient.Connect(ctx); err != nil {
		logger.Fatalf("WebSocket connection failed: %v", err)
	}
	defer wsClient.Close()
	logger.Println("✅ WebSocket connected successfully")
	logger.Println()

	// Step 5: Subscribe to price feeds using generic interface method
	// Using instrument IDs for common FX pairs:
	//
	//	21 = EURUSD, 31 = USDJPY, 1 = GBPUSD
	instruments := []string{"21", "31", "1"}

	logger.Println("Subscribing to price feeds:")
	logger.Println("  - EURUSD (ID 21)")
	logger.Println("  - USDJPY (ID 31)")
	logger.Println("  - GBPUSD (ID 1)")

	// Generic interface method - same for all brokers!
	// Using "FxSpot" since these are FX pairs (EURUSD, USDJPY, GBPUSD)
	if err := wsClient.SubscribeToPrices(ctx, instruments, "FxSpot"); err != nil {
		logger.Fatalf("Price subscription failed: %v", err)
	}
	logger.Println("✅ Subscribed to price feeds")
	logger.Println()

	// Step 6: Get the price update channel
	// Returns generic <-chan saxo.PriceUpdate
	priceChannel := wsClient.GetPriceUpdateChannel()

	// Step 7: Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Step 8: Listen to price updates
	logger.Println("📊 Listening to real-time prices... (Press Ctrl+C to stop)")
	logger.Println()
	fmt.Println("Instrument | Bid      | Ask      | Spread   | Time")
	fmt.Println("-----------|----------|----------|----------|---------------------")

	// Track price counts for statistics
	priceCount := make(map[string]int)

	// Optional: Set a timeout for automatic shutdown (30 seconds)
	timeout := time.After(30 * time.Second)

	for {
		select {
		case price := <-priceChannel:
			// Generic PriceUpdate type - same for all brokers!
			// Fields: Ticker, Bid, Ask, Mid, Timestamp
			spread := price.Ask - price.Bid

			// Display price update
			fmt.Printf("%-10s | %.5f | %.5f | %.5f | %s\n",
				price.Ticker,
				price.Bid,
				price.Ask,
				spread,
				time.Now().Format("15:04:05"))

			// Track statistics
			priceCount[price.Ticker]++

		case <-sigChan:
			logger.Println()
			logger.Println("⚠️  Received interrupt signal, shutting down...")
			printStats(logger, priceCount)
			return

		case <-timeout:
			logger.Println()
			logger.Println("⏱️  30-second timeout reached, shutting down...")
			printStats(logger, priceCount)
			return
		}
	}
}

// printStats displays statistics about received price updates
func printStats(logger *log.Logger, priceCount map[string]int) {
	logger.Println()
	logger.Println("=== Price Update Statistics ===")

	total := 0
	for ticker, count := range priceCount {
		fmt.Printf("  %s: %d updates\n", ticker, count)
		total += count
	}

	fmt.Printf("  Total: %d updates\n", total)
	logger.Println()
	logger.Println("=== WebSocket Price Subscription Example Complete ===")
	logger.Println()
	logger.Println("Key Takeaways:")
	logger.Println("  - WebSocketClient is a generic, broker-agnostic interface")
	logger.Println("  - PriceUpdate is a generic type (Ticker, Bid, Ask, Mid)")
	logger.Println("  - Same code works with any broker implementing the interface")
	logger.Println("  - Easy to mock WebSocketClient for testing")
}
