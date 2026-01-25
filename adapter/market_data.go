package saxo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

// tomorrowMidnightRFC3339 returns tomorrow's midnight time in RFC3339 format
// Following legacy strategies/strategy.go TomorrowMidnightRFC3339() pattern
func tomorrowMidnightRFC3339() string {
	// Calculate tomorrow's date
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	// Set the time components to midnight
	tomorrowMidnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)
	// Format the date as a string
	formattedTomorrow := tomorrowMidnight.Format(time.RFC3339)
	return formattedTomorrow
}

// RoundTickSize rounds a value to the nearest tick size
// Following legacy strategies/strategy.go RoundTickSize() pattern
// This is exported for use by other packages that need generic trading math.
func RoundTickSize(value, rounding float64) float64 {
	if rounding == 0 {
		return math.Round(value)
	}
	return math.Round(value/rounding) * rounding
}

// SetDecimals rounds a value to the specified number of decimal places
// Following legacy strategies/strategy.go SetDecimals() pattern
// This is exported for use by other packages that need generic trading math.
func SetDecimals(value float64, decimals int, modernFractions bool, numeratorDecimals int) float64 {
	if modernFractions {
		// Handle modern fractions rounding - add numerator decimals
		decimals = decimals + numeratorDecimals
	}
	shift := math.Pow(10, float64(decimals))
	return math.Round(value*shift) / shift
}

// GetDecimalsFromTickSize calculates the number of decimal places from a tick size
// For example: 0.25 -> 2 decimals, 0.1 -> 1 decimal, 1.0 -> 0 decimals
// This is exported for use by other packages that need generic trading math.
func GetDecimalsFromTickSize(tickSize float64) int {
	if tickSize <= 0 {
		return 2 // Default fallback
	}

	// Convert to string to count decimal places
	tickStr := fmt.Sprintf("%.10f", tickSize)

	// Remove trailing zeros
	tickStr = strings.TrimRight(tickStr, "0")

	// Find decimal point
	decimalIndex := strings.Index(tickStr, ".")
	if decimalIndex == -1 {
		return 0 // No decimal point, whole number
	}

	// Count digits after decimal point
	decimals := len(tickStr) - decimalIndex - 1

	// Debug: Let's see what we're calculating
	// Uncomment for debugging: fmt.Printf("TickSize %.4f -> '%s' -> %d decimals\n", tickSize, tickStr, decimals)

	return decimals
}

// GetInstrumentPrice fetches current market price using enriched instrument data
// Following legacy broker/broker_http.go patterns for price retrieval
func (sbc *SaxoBrokerClient) GetInstrumentPrice(ctx context.Context, instrument Instrument) (*PriceData, error) {
	sbc.logger.Debug("Fetching instrument price",
		"function", "GetInstrumentPrice",
		"ticker", instrument.Ticker)

	// Validate enriched instrument data
	if instrument.Uic == 0 {
		return nil, fmt.Errorf("instrument %s is not enriched - Identifier (UIC) is missing. Run instrument enrichment first", instrument.Ticker)
	}
	if instrument.AssetType == "" {
		return nil, fmt.Errorf("instrument %s is missing AssetType. This should be loaded from futures.json", instrument.Ticker)
	}

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Build request URL using enriched UIC and AssetType
	requestURL := fmt.Sprintf("%s/chart/v1/charts?Uic=%d&AssetType=%s&Horizon=60",
		sbc.baseURL, instrument.Uic, instrument.AssetType)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse price data
	var saxoPrice SaxoPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoPrice); err != nil {
		return nil, fmt.Errorf("failed to decode price response: %w", err)
	}

	// Convert to generic format
	priceData := sbc.convertFromSaxoPrice(saxoPrice, instrument.Ticker)

	sbc.logger.Info("Price fetched successfully",
		"function", "GetInstrumentPrice",
		"ticker", instrument.Ticker,
		"bid", priceData.Bid,
		"ask", priceData.Ask)

	return priceData, nil
}

// GetAccountInfo fetches current account information
// Following legacy portfolio balance patterns
func (sbc *SaxoBrokerClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	sbc.logger.Debug("Fetching account information",
		"function", "GetAccountInfo")

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Build request URL - account info endpoint
	requestURL := fmt.Sprintf("%s/port/v1/accounts/me", sbc.baseURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse account data
	var saxoAccount SaxoAccountInfo
	if err := json.NewDecoder(resp.Body).Decode(&saxoAccount); err != nil {
		return nil, fmt.Errorf("failed to decode account response: %w", err)
	}

	sbc.logger.Info("Account info fetched",
		"function", "GetAccountInfo",
		"currency", saxoAccount.Currency,
		"account_type", saxoAccount.AccountType)

	// Return directly - AccountInfo is a type alias to SaxoAccountInfo
	return &saxoAccount, nil
}

// GetHistoricalData fetches historical OHLC data from Saxo Bank using enriched instrument data
// Following legacy SinglePivotHistory caching pattern: cache for 1 hour per instrument
func (sbc *SaxoBrokerClient) GetHistoricalData(ctx context.Context, instrument Instrument, days int) ([]HistoricalDataPoint, error) {
	sbc.logger.Debug("Fetching historical data",
		"function", "GetHistoricalData",
		"ticker", instrument.Ticker,
		"days", days)

	// Create cache key (identifier + days to ensure cache matches request)
	cacheKey := fmt.Sprintf("%d_%d", instrument.Uic, days)

	// Check cache first (following legacy findCachedOHLC pattern)
	sbc.cacheMutex.RLock()
	if cached, exists := sbc.historyCache[cacheKey]; exists {
		// Check if cache is still valid (< 1 hour old like legacy system)
		if time.Since(cached.Timestamp) < sbc.cacheExpiry && len(cached.Data) >= days {
			sbc.cacheMutex.RUnlock()
			sbc.logger.Debug("History from cache",
				"function", "GetHistoricalData",
				"ticker", instrument.Ticker,
				"cache_age", time.Since(cached.Timestamp))
			return cached.Data, nil
		}
	}
	sbc.cacheMutex.RUnlock()

	// Cache miss or expired - fetch fresh data
	sbc.logger.Debug("History from request",
		"function", "GetHistoricalData",
		"ticker", instrument.Ticker,
		"reason", "cache miss or expired")

	// Validate enriched instrument data
	if instrument.Uic == 0 {
		return nil, fmt.Errorf("instrument %s is not enriched - Identifier (UIC) is missing. Run instrument enrichment first", instrument.Ticker)
	}
	if instrument.AssetType == "" {
		return nil, fmt.Errorf("instrument %s is missing AssetType. This should be loaded from futures.json", instrument.Ticker)
	}

	// Check authentication
	if !sbc.authClient.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated with broker")
	}

	// Build request URL for historical chart data using enriched UIC and AssetType
	// Following legacy broker/broker_http.go GetSaxoHistoricBars pattern
	// Using daily horizon (1440 minutes = 1 day), Mode=UpTo, and FieldGroups=Data
	requestURL := fmt.Sprintf("%s/chart/v3/charts?AssetType=%s&FieldGroups=Data&Count=%d&Horizon=1440&Mode=UpTo&Time=%s&Uic=%d",
		sbc.baseURL, instrument.AssetType, days, tomorrowMidnightRFC3339(), instrument.Uic)

	sbc.logger.Debug("Saxo API request",
		"function", "GetHistoricalData",
		"url", requestURL)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Execute request with OAuth2 auto-refresh
	resp, err := sbc.doRequest(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode != http.StatusOK {
		return nil, sbc.handleErrorResponse(resp)
	}

	// Parse chart data
	var saxoResponse SaxoPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&saxoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode chart response: %w", err)
	}

	sbc.logger.Debug("Received data points",
		"function", "GetHistoricalData",
		"ticker", instrument.Ticker,
		"count", len(saxoResponse.Data))

	// Debug: Log first data point to see what we're getting
	/*
		if len(saxoResponse.Data) > 0 {
			first := saxoResponse.Data[0]
			if strings.ToLower(instrument.AssetType) == "contractfutures" {
				sbc.logger.Debug("First data point (Futures)",
					"function", "GetHistoricalData",
					"ticker", instrument.Ticker,
					"time", first.Time,
					"open", first.Open,
					"high", first.High,
					"low", first.Low,
					"close", first.Close,
					"volume", first.Volume)
			} else {
				sbc.logger.Debug("First data point (FX)",
					"function", "GetHistoricalData",
					"ticker", instrument.Ticker,
					"time", first.Time,
					"open_bid", first.OpenBid,
					"open_ask", first.OpenAsk,
					"high_bid", first.HighBid,
					"high_ask", first.HighAsk)
			}
		} // Convert to standardized format based on asset type
	*/
	historicalData := make([]HistoricalDataPoint, len(saxoResponse.Data))
	for i, chartPoint := range saxoResponse.Data {
		var open, high, low, close float64

		// Handle different asset types following legacy broker_http.go pattern
		switch strings.ToLower(instrument.AssetType) {
		case "contractfutures":
			// Futures have direct OHLC values
			open = chartPoint.Open
			high = chartPoint.High
			low = chartPoint.Low
			close = chartPoint.Close
		case "fxspot":
			// FX uses bid/ask spreads - calculate mid prices
			open = (chartPoint.OpenBid + chartPoint.OpenAsk) / 2
			high = (chartPoint.HighBid + chartPoint.HighAsk) / 2
			low = (chartPoint.LowBid + chartPoint.LowAsk) / 2
			close = (chartPoint.CloseBid + chartPoint.CloseAsk) / 2
		default:
			sbc.logger.Warn("Unknown asset type, using futures format",
				"function", "GetHistoricalData",
				"asset_type", instrument.AssetType,
				"ticker", instrument.Ticker)
			open = chartPoint.Open
			high = chartPoint.High
			low = chartPoint.Low
			close = chartPoint.Close
		}

		// Simple conversion following legacy ConvertFuturesData pattern
		// No rounding here - rounding happens in strategy layer following legacy pattern

		// Parse timestamp
		date, err := time.Parse(time.RFC3339, chartPoint.Time)
		if err != nil {
			sbc.logger.Warn("Failed to parse timestamp",
				"function", "GetHistoricalData",
				"time", chartPoint.Time,
				"error", err)
			date = time.Now().AddDate(0, 0, -days+i) // Fallback
		}

		historicalData[i] = HistoricalDataPoint{
			Ticker: instrument.Ticker,
			Time:   date,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  close,
			Volume: 0, // Saxo doesn't provide volume for FX
		}
	}

	// Store in cache following legacy pattern (cache for 1 hour)
	sbc.cacheMutex.Lock()
	sbc.historyCache[cacheKey] = &cachedHistoricalData{
		Data:      historicalData,
		Timestamp: time.Now(),
	}
	sbc.cacheMutex.Unlock()

	sbc.logger.Debug("Historical data cached",
		"function", "GetHistoricalData",
		"ticker", instrument.Ticker,
		"cache_expiry", sbc.cacheExpiry)

	return historicalData, nil
}
