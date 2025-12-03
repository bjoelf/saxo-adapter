package main

import (
	"context"
	"log"
	"os"
	"time"

	saxo "github.com/bjoelf/saxo-adapter/adapter"
)

// This example demonstrates how to fetch historical market data using the saxo-adapter
// It shows authentication, broker client creation, and historical data retrieval
func main() {
	logger := log.New(os.Stdout, "HISTORICAL-DATA-EXAMPLE: ", log.LstdFlags)

	logger.Println("=== Saxo Adapter - Historical Data Example ===")
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

	// Step 3: Create broker services (inject authClient)
	logger.Println("Creating broker services...")

	// CreateBrokerServices returns BrokerClient interface
	brokerClient, err := saxo.CreateBrokerServices(authClient, logger)
	if err != nil {
		logger.Fatalf("Failed to create broker services: %v", err)
	}
	logger.Println("✅ Broker services created successfully")
	logger.Println()

	// Step 4: Define instrument to fetch (EURUSD as example)
	instrument := saxo.Instrument{
		Ticker:      "EURUSD",
		Description: "Euro vs US Dollar",
		AssetType:   "FxSpot",
		Uic:         21, // UIC for EURUSD
	}

	logger.Printf("Fetching historical data for %s (%s)...", instrument.Ticker, instrument.Description)
	logger.Println()

	// Step 5: Fetch 30 days of historical data
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	days := 30
	historicalData, err := brokerClient.GetHistoricalData(ctx, instrument, days)
	if err != nil {
		logger.Fatalf("Failed to fetch historical data: %v", err)
	}

	// Display results
	logger.Printf("✓ Fetched %d days of historical data", len(historicalData))
	logger.Println("\nRecent price data:")
	logger.Println("Date                 | Open      | High      | Low       | Close     ")
	logger.Println("---------------------|-----------|-----------|-----------|----------")

	// Show last 10 days
	start := len(historicalData) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(historicalData); i++ {
		data := historicalData[i]
		logger.Printf("%s | %9.2f | %9.2f | %9.2f | %9.2f",
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

		logger.Println("\nStatistics:")
		logger.Printf("  Period: %d days", len(historicalData))
		logger.Printf("  Price range: %.2f - %.2f (range: %.2f)", minPrice, maxPrice, priceRange)
		logger.Printf("  Average daily volume: %.0f", avgVolume)
		logger.Printf("  Latest close: %.2f", historicalData[len(historicalData)-1].Close)
	}

	logger.Println("\n✓ Example completed successfully")
}
