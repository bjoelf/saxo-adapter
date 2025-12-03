# Historical Data Example

This example demonstrates how to fetch historical market data using the saxo-adapter.

## Features

- Authenticate with Saxo API
- Create broker client
- Fetch historical price data (OHLCV - Open, High, Low, Close, Volume)
- Display recent price data
- Calculate basic statistics

## Prerequisites

1. Valid Saxo credentials (SIM or LIVE)
2. Already authenticated (run `../basic_auth` example first)
3. Valid instrument UIC (Unique Instrument Code)

## Configuration

Set environment variables in your shell:

```bash
# Navigate to project root
cd /path/to/saxo-adapter

# Create .env file with your credentials (if not already created)
cat > .env << 'EOF'
SAXO_ENVIRONMENT=sim
SAXO_CLIENT_ID="your_client_id_here"
SAXO_CLIENT_SECRET="your_client_secret_here"
EOF

# Load variables into your shell
export $(grep -v '^#' .env | xargs)

# Verify they're loaded
echo "Environment: $SAXO_ENVIRONMENT"
echo "Client ID: $SAXO_CLIENT_ID"
```

## Usage

```bash
# From project root (after exporting env variables above)
go run ./examples/historical_data/main.go
```

## Example Output

```
HISTORICAL-DATA-EXAMPLE: Using sim environment
HISTORICAL-DATA-EXAMPLE: ✓ Already authenticated
HISTORICAL-DATA-EXAMPLE: ✓ Broker client created
HISTORICAL-DATA-EXAMPLE: Fetching historical data for ES (E-mini S&P 500)...
HISTORICAL-DATA-EXAMPLE: ✓ Fetched 30 days of historical data

Recent price data:
Date                 | Open      | High      | Low       | Close     
---------------------|-----------|-----------|-----------|----------
2025-11-24 00:00     |   6020.25 |   6055.50 |   6015.00 |   6048.75
2025-11-25 00:00     |   6048.75 |   6082.00 |   6040.50 |   6075.25
...

Statistics:
  Period: 30 days
  Price range: 5875.00 - 6082.00 (range: 207.00)
  Average daily volume: 1234567
  Latest close: 6075.25

✓ Example completed successfully
```

## Notes

- Historical data is cached for 1 hour by default
- Data is fetched as daily candlesticks (OHLCV)
- The example uses E-mini S&P 500 (ES) - replace UIC with your instrument
- Requires active authentication token
