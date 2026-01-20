package saxo

import (
	"encoding/json"
	"testing"
)

func TestParsePositionsWithFieldGroups(t *testing.T) {
	// This is the actual response from the API with FieldGroups
	responseJSON := `{
  "__count": 2,
  "Data": [
    {
      "DisplayAndFormat": {
        "Currency": "USD",
        "Decimals": 2,
        "Description": "Copper - Mar 2026",
        "Format": "Normal",
        "Symbol": "HGH6"
      },
      "NetPositionId": "47316301__CF",
      "PositionBase": {
        "Amount": 1,
        "AssetType": "ContractFutures",
        "CanBeClosed": false,
        "ClientId": "18258177",
        "CloseConversionRateSettled": false,
        "ExecutionTimeOpen": "2026-01-20T09:49:20.689737Z",
        "ExpiryDate": "2026-03-27T00:00:00.000000Z",
        "IsForceOpen": false,
        "IsMarketOpen": true,
        "LockedByBackOffice": false,
        "NoticeDate": "2026-02-27T00:00:00.000000Z",
        "OpenBondPoolFactor": 1,
        "OpenPrice": 584.9,
        "OpenPriceIncludingCosts": 584.94648,
        "RelatedOpenOrders": [],
        "SourceOrderId": "5036877611",
        "Status": "Open",
        "Uic": 47316301,
        "ValueDate": "2026-01-20T00:00:00.000000Z"
      },
      "PositionId": "5025356154",
      "PositionView": {
        "CalculationReliability": "ApproximatedPrice",
        "ConversionRateCurrent": 0.8532935,
        "ConversionRateOpen": 0.8532935,
        "CurrentBondPoolFactor": 1,
        "CurrentPrice": 0,
        "CurrentPriceDelayMinutes": 10,
        "CurrentPriceType": "None",
        "Exposure": 0,
        "ExposureCurrency": "USD",
        "ExposureInBaseCurrency": 0,
        "InstrumentPriceDayPercentChange": 0,
        "MarketState": "Open",
        "MarketValue": 0,
        "MarketValueInBaseCurrency": 0,
        "OpenInterest": 140991,
        "ProfitLossOnTrade": -1650,
        "ProfitLossOnTradeInBaseCurrency": -1407.93,
        "ProfitLossOnTradeIntraday": -1650,
        "ProfitLossOnTradeIntradayInBaseCurrency": -1407.93,
        "TradeCostsTotal": -23.24,
        "TradeCostsTotalInBaseCurrency": -19.83,
        "UnderlyingCurrentPrice": 0
      }
    }
  ]
}`

	var response SaxoOpenPositionsResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify parsing
	if len(response.Data) != 1 {
		t.Errorf("Expected 1 position, got %d", len(response.Data))
	}

	pos := response.Data[0]

	// Check Amount field - this is the critical field!
	if pos.PositionBase.Amount != 1 {
		t.Errorf("Expected Amount=1, got Amount=%v", pos.PositionBase.Amount)
	}

	// Check other important fields
	if pos.DisplayAndFormat.Symbol != "HGH6" {
		t.Errorf("Expected Symbol=HGH6, got Symbol=%s", pos.DisplayAndFormat.Symbol)
	}

	if pos.DisplayAndFormat.Description != "Copper - Mar 2026" {
		t.Errorf("Expected Description='Copper - Mar 2026', got Description=%s", pos.DisplayAndFormat.Description)
	}

	if pos.PositionBase.OpenPrice != 584.9 {
		t.Errorf("Expected OpenPrice=584.9, got OpenPrice=%v", pos.PositionBase.OpenPrice)
	}

	if pos.PositionView.ProfitLossOnTradeInBaseCurrency != -1407.93 {
		t.Errorf("Expected ProfitLoss=-1407.93, got ProfitLoss=%v", pos.PositionView.ProfitLossOnTradeInBaseCurrency)
	}

	t.Logf("✓ Successfully parsed position with Amount=%v", pos.PositionBase.Amount)
	t.Logf("✓ Symbol: %s, Description: %s", pos.DisplayAndFormat.Symbol, pos.DisplayAndFormat.Description)
	t.Logf("✓ OpenPrice: %v, P/L: %v", pos.PositionBase.OpenPrice, pos.PositionView.ProfitLossOnTradeInBaseCurrency)
}
