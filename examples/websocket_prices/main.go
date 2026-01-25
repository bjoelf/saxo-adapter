package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
	"github.com/bjoelf/saxo-adapter/adapter/websocket"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("=== Saxo Adapter - WebSocket Price Subscription Example ===")
	logger.Info("This example demonstrates broker-agnostic real-time data streaming")
	logger.Info("using the generic WebSocketClient interface")

	// Step 1: Create auth client
	logger.Info("Creating authentication client...")
	var authClient saxo.AuthClient
	var err error
	authClient, err = saxo.CreateSaxoAuthClient(logger)
	if err != nil {
		logger.Error("Failed to create auth client: %v", "error", err); os.Exit(1)
	}

	// Step 2: Authenticate using generic AuthClient interface
	ctx := context.Background()
	logger.Info("Authenticating...")
	if err := authClient.Login(ctx); err != nil {
		logger.Error("Authentication failed: %v", "error", err); os.Exit(1)
	}
	logger.Info("✅ Authenticated successfully")
	logger.Info("")

	// Step 3: Create WebSocket client using generic interface
	logger.Info("Creating websocket client...")

	// Note: We create using websocket package, but use via saxo.WebSocketClient interface
	wsClient := saxo.WebSocketClient(websocket.NewSaxoWebSocketClient(
		authClient,
		authClient.GetBaseURL(),
		authClient.GetWebSocketURL(),
		logger,
	))

	// Step 4: Connect using generic WebSocketClient.Connect()
	logger.Info("Connecting to WebSocket...")
	if err := wsClient.Connect(ctx); err != nil {
		logger.Error("WebSocket connection failed: %v", "error", err); os.Exit(1)
	}
	defer wsClient.Close()
	logger.Info("✅ WebSocket connected successfully")
	logger.Info("")

	// Step 5: Subscribe to price feeds using generic interface method
	// Using instrument IDs for common FX pairs:
	//
	//	21 = EURUSD, 31 = USDJPY, 1 = GBPUSD
	instruments := []string{"21", "31", "1"}

	logger.Info("Subscribing to price feeds:")
	logger.Info("  - EURUSD (ID 21)")
	logger.Info("  - USDJPY (ID 31)")
	logger.Info("  - GBPUSD (ID 1)")

	// Generic interface method - same for all brokers!
	// Using "FxSpot" since these are FX pairs (EURUSD, USDJPY, GBPUSD)
	if err := wsClient.SubscribeToPrices(ctx, instruments, "FxSpot"); err != nil {
		logger.Error("Price subscription failed: %v", "error", err); os.Exit(1)
	}
	logger.Info("✅ Subscribed to price feeds")
	logger.Info("")

	// Step 6: Get the price update channel
	// Returns generic <-chan saxo.PriceUpdate
	priceChannel := wsClient.GetPriceUpdateChannel()

	// Step 7: Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Step 8: Listen to price updates
	logger.Info("📊 Listening to real-time prices... (Press Ctrl+C to stop)")
	logger.Info("")
	fmt.Println("UIC        | Bid      | Ask      | Spread   | Time")
	fmt.Println("-----------|----------|----------|----------|---------------------")

	// Track price counts for statistics
	priceCount := make(map[int]int)

	// Optional: Set a timeout for automatic shutdown (30 seconds)
	timeout := time.After(30 * time.Second)

	for {
		select {
		case price := <-priceChannel:
			// Generic PriceUpdate type - same for all brokers!
			// Fields: Uic, Bid, Ask, Mid, Timestamp
			spread := price.Ask - price.Bid

			// Display price update
			fmt.Printf("%-10d | %.5f | %.5f | %.5f | %s\n",
				price.Uic,
				price.Bid,
				price.Ask,
				spread,
				time.Now().Format("15:04:05"))

			// Track statistics
			priceCount[price.Uic]++

		case <-sigChan:
			logger.Info("")
			logger.Info("⚠️  Received interrupt signal, shutting down...")
			printStats(logger, priceCount)
			return

		case <-timeout:
			logger.Info("")
			logger.Info("⏱️  30-second timeout reached, shutting down...")
			printStats(logger, priceCount)
			return
		}
	}
}

// printStats displays statistics about received price updates
func printStats(logger *slog.Logger, priceCount map[int]int) {
	logger.Info("")
	logger.Info("=== Price Update Statistics ===")

	total := 0
	for uic, count := range priceCount {
		fmt.Printf("  UIC %d: %d updates\n", uic, count)
		total += count
	}

	fmt.Printf("  Total: %d updates\n", total)
	logger.Info("")
	logger.Info("=== WebSocket Price Subscription Example Complete ===")
	logger.Info("")
	logger.Info("Key Takeaways:")
	logger.Info("  - WebSocketClient is a generic, broker-agnostic interface")
	logger.Info("  - PriceUpdate uses native broker identifiers (Uic, Bid, Ask, Mid)")
	logger.Info("  - Same code works with any broker implementing the interface")
	logger.Info("  - Easy to mock WebSocketClient for testing")
}
