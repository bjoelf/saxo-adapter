package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

// This example demonstrates how to fetch historical market data using the saxo-adapter
// It shows authentication, broker client creation, and historical data retrieval
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	logger.Info("=== Saxo Adapter - Historical Data Example ===")
	logger.Info("This example demonstrates broker-agnostic real-time data streaming")
	logger.Info("using the generic WebSocketClient interface")

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

	// Step 4: Define instrument to fetch (EURUSD as example)
	instrument := saxo.Instrument{
		Ticker:      "EURUSD",
		Description: "Euro vs US Dollar",
		AssetType:   "FxSpot",
		Uic:         21, // UIC for EURUSD
	}

	logger.Info("Fetching historical data for instrument", "ticker", instrument.Ticker, "description", instrument.Description)
	logger.Info("")

	// Step 5: Fetch 30 days of historical data
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	days := 30
	historicalData, err := brokerClient.GetHistoricalData(ctx, instrument, days)
	if err != nil {
		logger.Error("Failed to fetch historical data: %v", "error", err)
		os.Exit(1)
	}

	// Display results
	logger.Info("Fetched historical data", "days", len(historicalData))
	logger.Info("\nRecent price data:")
	logger.Info("Date                 | Open      | High      | Low       | Close     ")
	logger.Info("---------------------|-----------|-----------|-----------|----------")

	// Show last 10 days
	start := len(historicalData) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(historicalData); i++ {
		data := historicalData[i]
		fmt.Printf("%s | %9.2f | %9.2f | %9.2f | %9.2f\n",
			data.Time.Format("2006-01-02 15:04"),
			data.Open,
			data.High,
			data.Low,
			data.Close,
		)
	}

	// Calculate some basic statistics
	if len(historicalData) > 0 {
		var totalVolume float64
		var minPrice, maxPrice float64
		minPrice = historicalData[0].Low
		maxPrice = historicalData[0].High

		for _, data := range historicalData {
			totalVolume += data.Volume
			if data.Low < minPrice {
				minPrice = data.Low
			}
			if data.High > maxPrice {
				maxPrice = data.High
			}
		}

		avgVolume := totalVolume / float64(len(historicalData))
		priceRange := maxPrice - minPrice

		logger.Info("\nStatistics:")
		fmt.Printf("  Period: %d days\n", len(historicalData))
		fmt.Printf("  Price range: %.2f - %.2f (range: %.2f)\n", minPrice, maxPrice, priceRange)
		fmt.Printf("  Average daily volume: %.0f\n", avgVolume)
		fmt.Printf("  Latest close: %.2f\n", historicalData[len(historicalData)-1].Close)
	}

	logger.Info("\n✓ Example completed successfully")
}
