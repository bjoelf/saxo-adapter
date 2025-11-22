package saxo

import "time"

// SaxoOrderRequest represents Saxo Bank order request structure
// Following legacy broker_http.go patterns - shared types package to avoid import cycles
type SaxoOrderRequest struct {
	AccountKey  string  `json:"AccountKey"`           // Account key (required)
	Uic         int     `json:"Uic"`                  // Instrument identifier
	BuySell     string  `json:"BuySell"`              // "Buy" or "Sell"
	Amount      float64 `json:"Amount"`               // Order size
	OrderType   string  `json:"OrderType"`            // "Market", "Limit", "Stop", etc.
	OrderPrice  float64 `json:"OrderPrice,omitempty"` // Price for limit/stop orders
	ManualOrder bool    `json:"ManualOrder"`          // Required: indicates if order is manual or automated

	// Order duration following Saxo patterns
	OrderDuration struct {
		DurationType       string `json:"DurationType"` // "DayOrder", "GoodTillDate", etc.
		ExpirationDateTime string `json:"ExpirationDateTime,omitempty"`
	} `json:"OrderDuration"`

	// FX-specific fields
	AssetType string `json:"AssetType"` // "FxSpot", "Future", etc.

	// Optional advanced order fields
	TakeProfitPrice *float64 `json:"TakeProfitPrice,omitempty"`
	StopLossPrice   *float64 `json:"StopLossPrice,omitempty"`
}

// SaxoOrderResponse represents Saxo Bank order response
type SaxoOrderResponse struct {
	OrderId   string `json:"OrderId"`
	Status    string `json:"Status"` // "Working", "Filled", "Rejected", etc.
	Message   string `json:"Message,omitempty"`
	Timestamp string `json:"Timestamp"`

	// Execution details
	ExecutionPrice *float64 `json:"ExecutionPrice,omitempty"`
	FilledAmount   *int     `json:"FilledAmount,omitempty"`
}

// SaxoOrderStatus represents current order status from Saxo
type SaxoOrderStatus struct {
	OrderId        string   `json:"OrderId"`
	Status         string   `json:"Status"`
	Uic            int      `json:"Uic"`
	BuySell        string   `json:"BuySell"`
	Amount         int      `json:"Amount"`
	FilledAmount   int      `json:"FilledAmount"`
	OrderPrice     *float64 `json:"OrderPrice"`
	ExecutionPrice *float64 `json:"ExecutionPrice"`
	Timestamp      string   `json:"Timestamp"`
}

// SaxoToken represents OAuth2 token following legacy pattern
type SaxoToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}

// SaxoAccountInfo represents account information
type SaxoAccountInfo struct {
	AccountKey                            string `json:"AccountKey"`
	AccountType                           string `json:"AccountType"`
	Currency                              string `json:"Currency"`
	ClientKey                             string `json:"ClientKey"`
	CanUseCashPositionsAsMarginCollateral bool   `json:"CanUseCashPositionsAsMarginCollateral"`
}

// SaxoBalance represents account balance from /port/v1/balances/me
// Complete structure matching Saxo Bank API response
type SaxoBalance struct {
	CalculationReliability           string  `json:"CalculationReliability"`
	CashAvailableForTrading          float64 `json:"CashAvailableForTrading"`
	CashBalance                      float64 `json:"CashBalance"`
	CashBlocked                      float64 `json:"CashBlocked"`
	ChangesScheduled                 bool    `json:"ChangesScheduled"`
	ClosedPositionsCount             int     `json:"ClosedPositionsCount"`
	CollateralAvailable              float64 `json:"CollateralAvailable"`
	CorporateActionUnrealizedAmounts float64 `json:"CorporateActionUnrealizedAmounts"`
	CostToClosePositions             float64 `json:"CostToClosePositions"`
	Currency                         string  `json:"Currency"`
	CurrencyDecimals                 int     `json:"CurrencyDecimals"`
	InitialMargin                    struct {
		CollateralAvailable          float64 `json:"CollateralAvailable"`
		MarginAvailable              float64 `json:"MarginAvailable"`
		MarginCollateralNotAvailable float64 `json:"MarginCollateralNotAvailable"`
		MarginUsedByCurrentPositions float64 `json:"MarginUsedByCurrentPositions"`
		MarginUtilizationPct         float64 `json:"MarginUtilizationPct"`
		NetEquityForMargin           float64 `json:"NetEquityForMargin"`
		OtherCollateralDeduction     float64 `json:"OtherCollateralDeduction"`
	} `json:"InitialMargin"`
	IntradayMarginDiscount            float64 `json:"IntradayMarginDiscount"`
	IsPortfolioMarginModelSimple      bool    `json:"IsPortfolioMarginModelSimple"`
	MarginAndCollateralUtilizationPct float64 `json:"MarginAndCollateralUtilizationPct"`
	MarginAvailableForTrading         float64 `json:"MarginAvailableForTrading"`
	MarginCollateralNotAvailable      float64 `json:"MarginCollateralNotAvailable"`
	MarginExposureCoveragePct         float64 `json:"MarginExposureCoveragePct"`
	MarginNetExposure                 float64 `json:"MarginNetExposure"`
	MarginUsedByCurrentPositions      float64 `json:"MarginUsedByCurrentPositions"`
	MarginUtilizationPct              float64 `json:"MarginUtilizationPct"`
	NetEquityForMargin                float64 `json:"NetEquityForMargin"`
	NetPositionsCount                 int     `json:"NetPositionsCount"`
	NonMarginPositionsValue           float64 `json:"NonMarginPositionsValue"`
	OpenIpoOrdersCount                int     `json:"OpenIpoOrdersCount"`
	OpenPositionsCount                int     `json:"OpenPositionsCount"`
	OptionPremiumsMarketValue         float64 `json:"OptionPremiumsMarketValue"`
	OrdersCount                       int     `json:"OrdersCount"`
	OtherCollateral                   float64 `json:"OtherCollateral"`
	SettlementValue                   float64 `json:"SettlementValue"`
	SpendingPowerDetail               struct {
		Current float64 `json:"Current"`
		Maximum float64 `json:"Maximum"`
	} `json:"SpendingPowerDetail"`
	TotalValue                       float64 `json:"TotalValue"`
	TransactionsNotBooked            float64 `json:"TransactionsNotBooked"`
	TriggerOrdersCount               int     `json:"TriggerOrdersCount"`
	UnrealizedMarginClosedProfitLoss float64 `json:"UnrealizedMarginClosedProfitLoss"`
	UnrealizedMarginOpenProfitLoss   float64 `json:"UnrealizedMarginOpenProfitLoss"`
	UnrealizedMarginProfitLoss       float64 `json:"UnrealizedMarginProfitLoss"`
	UnrealizedPositionsValue         float64 `json:"UnrealizedPositionsValue"`
}

// SaxoMarginOverview represents margin breakdown by instrument group
// Endpoint: /port/v1/balances/marginoverview?ClientKey={clientKey}
type SaxoMarginOverview struct {
	Groups []struct {
		Contributors []struct {
			AssetTypes            []string `json:"AssetTypes"`
			InstrumentDescription string   `json:"InstrumentDescription"`
			InstrumentSpecifier   string   `json:"InstrumentSpecifier"`
			Margin                float64  `json:"Margin"`
			Uic                   int      `json:"Uic"`
		} `json:"Contributors"`
		GroupType   string  `json:"GroupType"`
		TotalMargin float64 `json:"TotalMargin"`
	} `json:"Groups"`
}

// SaxoClientInfo represents client/user information from /port/v1/users/me
type SaxoClientInfo struct {
	Active                            bool      `json:"Active"`
	ClientKey                         string    `json:"ClientKey"`
	Culture                           string    `json:"Culture"`
	Language                          string    `json:"Language"`
	LastLoginStatus                   string    `json:"LastLoginStatus"`
	LastLoginTime                     time.Time `json:"LastLoginTime"`
	LegalAssetTypes                   []string  `json:"LegalAssetTypes"`
	MarketDataViaOpenAPITermsAccepted bool      `json:"MarketDataViaOpenApiTermsAccepted"`
	Name                              string    `json:"Name"`
	TimeZoneID                        int       `json:"TimeZoneId"`
	UserID                            string    `json:"UserId"`
	UserKey                           string    `json:"UserKey"`
}

// SaxoErrorResponse represents Saxo API error response
type SaxoErrorResponse struct {
	ErrorCode string `json:"ErrorCode"`
	Message   string `json:"Message"`
	Details   string `json:"Details,omitempty"`
}

// SaxoPriceResponse represents Saxo Bank price/chart response
// Following legacy broker_http.go price retrieval patterns
type SaxoPriceResponse struct {
	Data []SaxoChartData `json:"Data"`
}

// SaxoChartData represents individual chart data point
// Following legacy broker_http.go pattern with different fields for different asset types
type SaxoChartData struct {
	// Futures fields (ContractFutures)
	Close    float64 `json:"Close"`
	High     float64 `json:"High"`
	Interest float64 `json:"Interest"`
	Low      float64 `json:"Low"`
	Open     float64 `json:"Open"`
	Volume   float64 `json:"Volume"`

	// FX fields (FxSpot)
	CloseAsk float64 `json:"CloseAsk"`
	CloseBid float64 `json:"CloseBid"`
	HighAsk  float64 `json:"HighAsk"`
	HighBid  float64 `json:"HighBid"`
	LowAsk   float64 `json:"LowAsk"`
	LowBid   float64 `json:"LowBid"`
	OpenAsk  float64 `json:"OpenAsk"`
	OpenBid  float64 `json:"OpenBid"`

	Time string `json:"Time"`
}

// SaxoAccountResponse represents account information response wrapper
type SaxoAccountResponse struct {
	Data []SaxoAccountInfo `json:"Data"`
}

// SaxoInfoPriceResponse represents Saxo Bank InfoPrice response
// Following legacy broker/broker_http.go current pricing patterns
type SaxoInfoPriceResponse struct {
	Data []SaxoInfoPrice `json:"Data"`
}

// SaxoInfoPrice represents current instrument pricing
// This is better than chart data for real-time quotes
type SaxoInfoPrice struct {
	Uic         int     `json:"Uic"`
	AssetType   string  `json:"AssetType"`
	Bid         float64 `json:"Bid"`
	Ask         float64 `json:"Ask"`
	Mid         float64 `json:"Mid"`
	LastUpdated string  `json:"LastUpdated"`
	MarketState string  `json:"MarketState"`
}

// SaxoOpenOrdersResponse represents response from GET /port/v1/orders/me
// Used by recovery system to fetch all open orders
type SaxoOpenOrdersResponse struct {
	Data  []SaxoOpenOrder `json:"Data"`
	Count int             `json:"__count"`
}

// SaxoOpenOrder represents a single open order from Saxo API
// Complete structure matching Saxo Bank API response
type SaxoOpenOrder struct {
	OrderID       string  `json:"OrderId"`
	Uic           int     `json:"Uic"`
	BuySell       string  `json:"BuySell"`
	Amount        float64 `json:"Amount"`
	OrderPrice    float64 `json:"OrderPrice"`
	OrderType     string  `json:"OpenOrderType"` // "StopIfTraded", "Limit", etc.
	AssetType     string  `json:"AssetType"`
	OrderTime     string  `json:"OrderTime"` // ISO 8601 format
	Status        string  `json:"Status"`    // "Working", "Parked", etc.
	AccountKey    string  `json:"AccountKey"`
	ClientKey     string  `json:"ClientKey"`
	OrderRelation string  `json:"OrderRelation"` // "StandAlone", "IfDone", "Oco"

	// Related orders (for OCO/IfDone relationships)
	RelatedOpenOrders []SaxoRelatedOrder `json:"RelatedOpenOrders,omitempty"`

	// Display information
	DisplayAndFormat struct {
		Symbol      string `json:"Symbol"`
		Description string `json:"Description"`
	} `json:"DisplayAndFormat"`

	// Market conditions
	DistanceToMarket float64 `json:"DistanceToMarket"`
	IsMarketOpen     bool    `json:"IsMarketOpen"`
	MarketPrice      float64 `json:"MarketPrice"`

	// Order configuration
	OrderDuration struct {
		DurationType       string `json:"DurationType"`
		ExpirationDateTime string `json:"ExpirationDateTime,omitempty"`
	} `json:"OrderDuration"`
}

// SaxoRelatedOrder represents a related order in OCO/IfDone relationships
type SaxoRelatedOrder struct {
	OrderID       string  `json:"OrderId"`
	OpenOrderType string  `json:"OpenOrderType"`
	OrderPrice    float64 `json:"OrderPrice"`
	Amount        float64 `json:"Amount"`
	Status        string  `json:"Status"`
}

// SaxoOpenPositionsResponse represents response from GET /port/v1/positions/me
type SaxoOpenPositionsResponse struct {
	Data  []SaxoOpenPosition `json:"Data"`
	Count int                `json:"__count"`
}

// SaxoOpenPosition represents an open position from Saxo Bank API
type SaxoOpenPosition struct {
	DisplayAndFormat struct {
		Currency    string `json:"Currency"`
		Decimals    int    `json:"Decimals"`
		Description string `json:"Description"`
		Format      string `json:"Format"`
		Symbol      string `json:"Symbol"`
	} `json:"DisplayAndFormat"`
	NetPositionID string `json:"NetPositionId"`
	PositionBase  struct {
		AccountID                  string        `json:"AccountId"`
		AccountKey                 string        `json:"AccountKey"`
		Amount                     float64       `json:"Amount"`
		AssetType                  string        `json:"AssetType"`
		CanBeClosed                bool          `json:"CanBeClosed"`
		ClientID                   string        `json:"ClientId"`
		CloseConversionRateSettled bool          `json:"CloseConversionRateSettled"`
		CorrelationKey             string        `json:"CorrelationKey"`
		ExecutionTimeOpen          time.Time     `json:"ExecutionTimeOpen"`
		ExpiryDate                 time.Time     `json:"ExpiryDate"`
		IsForceOpen                bool          `json:"IsForceOpen"`
		IsMarketOpen               bool          `json:"IsMarketOpen"`
		LockedByBackOffice         bool          `json:"LockedByBackOffice"`
		NoticeDate                 time.Time     `json:"NoticeDate"`
		OpenPrice                  float64       `json:"OpenPrice"`
		OpenPriceIncludingCosts    float64       `json:"OpenPriceIncludingCosts"`
		RelatedOpenOrders          []interface{} `json:"RelatedOpenOrders"`
		SourceOrderID              string        `json:"SourceOrderId"`
		Status                     string        `json:"Status"`
		Uic                        int           `json:"Uic"`
		ValueDate                  time.Time     `json:"ValueDate"`
	} `json:"PositionBase"`
	PositionID   string `json:"PositionId"`
	PositionView struct {
		Ask                                     float64   `json:"Ask"`
		Bid                                     float64   `json:"Bid"`
		CalculationReliability                  string    `json:"CalculationReliability"`
		ConversionRateCurrent                   float64   `json:"ConversionRateCurrent"`
		ConversionRateOpen                      float64   `json:"ConversionRateOpen"`
		CurrentPrice                            float64   `json:"CurrentPrice"`
		CurrentPriceDelayMinutes                int       `json:"CurrentPriceDelayMinutes"`
		CurrentPriceLastTraded                  time.Time `json:"CurrentPriceLastTraded"`
		CurrentPriceType                        string    `json:"CurrentPriceType"`
		Exposure                                float64   `json:"Exposure"`
		ExposureCurrency                        string    `json:"ExposureCurrency"`
		ExposureInBaseCurrency                  float64   `json:"ExposureInBaseCurrency"`
		InstrumentPriceDayPercentChange         float64   `json:"InstrumentPriceDayPercentChange"`
		MarketState                             string    `json:"MarketState"`
		MarketValue                             float64   `json:"MarketValue"`
		MarketValueInBaseCurrency               float64   `json:"MarketValueInBaseCurrency"`
		OpenInterest                            float64   `json:"OpenInterest"`
		ProfitLossOnTrade                       float64   `json:"ProfitLossOnTrade"`
		ProfitLossOnTradeInBaseCurrency         float64   `json:"ProfitLossOnTradeInBaseCurrency"`
		ProfitLossOnTradeIntraday               float64   `json:"ProfitLossOnTradeIntraday"`
		ProfitLossOnTradeIntradayInBaseCurrency float64   `json:"ProfitLossOnTradeIntradayInBaseCurrency"`
		TradeCostsTotal                         float64   `json:"TradeCostsTotal"`
		TradeCostsTotalInBaseCurrency           float64   `json:"TradeCostsTotalInBaseCurrency"`
	} `json:"PositionView"`
}

// SaxoClosedPositionsResponse represents response from GET /port/v1/closedpositions/me
type SaxoClosedPositionsResponse struct {
	Data  []SaxoClosedPosition `json:"Data"`
	Count int                  `json:"__count"`
}

// SaxoClosedPosition represents a closed position from Saxo Bank API
type SaxoClosedPosition struct {
	ClosedPosition struct {
		AccountID                                    string    `json:"AccountId"`
		Amount                                       float64   `json:"Amount"`
		AssetType                                    string    `json:"AssetType"`
		BuyOrSell                                    string    `json:"BuyOrSell"`
		ClientID                                     string    `json:"ClientId"`
		ClosedProfitLoss                             float64   `json:"ClosedProfitLoss"`
		ClosedProfitLossInBaseCurrency               float64   `json:"ClosedProfitLossInBaseCurrency"`
		ClosingMarketValue                           float64   `json:"ClosingMarketValue"`
		ClosingMarketValueInBaseCurrency             float64   `json:"ClosingMarketValueInBaseCurrency"`
		ClosingMethod                                string    `json:"ClosingMethod"`
		ClosingPositionID                            string    `json:"ClosingPositionId"`
		ClosingPrice                                 float64   `json:"ClosingPrice"`
		ConversionRateInstrumentToBaseSettledClosing bool      `json:"ConversionRateInstrumentToBaseSettledClosing"`
		ConversionRateInstrumentToBaseSettledOpening bool      `json:"ConversionRateInstrumentToBaseSettledOpening"`
		CostClosing                                  float64   `json:"CostClosing"`
		CostClosingInBaseCurrency                    float64   `json:"CostClosingInBaseCurrency"`
		CostOpening                                  float64   `json:"CostOpening"`
		CostOpeningInBaseCurrency                    float64   `json:"CostOpeningInBaseCurrency"`
		ExecutionTimeClose                           time.Time `json:"ExecutionTimeClose"`
		ExecutionTimeOpen                            time.Time `json:"ExecutionTimeOpen"`
		ExpiryDate                                   time.Time `json:"ExpiryDate"`
		NoticeDate                                   time.Time `json:"NoticeDate"`
		OpeningPositionID                            string    `json:"OpeningPositionId"`
		OpenPrice                                    float64   `json:"OpenPrice"`
		Uic                                          int       `json:"Uic"`
	} `json:"ClosedPosition"`
	ClosedPositionUniqueID string `json:"ClosedPositionUniqueId"`
	DisplayAndFormat       struct {
		Currency          string `json:"Currency"`
		Decimals          int    `json:"Decimals"`
		Description       string `json:"Description"`
		Format            string `json:"Format"`
		NumeratorDecimals int    `json:"NumeratorDecimals"`
		Symbol            string `json:"Symbol"`
	} `json:"DisplayAndFormat"`
	NetPositionID string `json:"NetPositionId"`
}
