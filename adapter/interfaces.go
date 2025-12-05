package saxo

import (
	"context"
	"net/http"
	"time"
)

// ============================================================================
// INTERFACES - These define the contracts this adapter implements
// ============================================================================

// AuthClient defines OAuth authentication interface for broker connections
type AuthClient interface {
	GetHTTPClient(ctx context.Context) (*http.Client, error)
	IsAuthenticated() bool
	GetAccessToken() (string, error)
	Login(ctx context.Context) error
	Logout() error
	RefreshToken(ctx context.Context) error
	StartAuthenticationKeeper(provider string)
	StartTokenEarlyRefresh(ctx context.Context, wsConnected <-chan bool, wsContextID <-chan string)
	GetBaseURL() string
	GetWebSocketURL() string
	SetRedirectURL(provider string, redirectURL string) error
	BuildRedirectURL(host string, provider string) string
	GenerateAuthURL(provider string, state string) (string, error)
	ExchangeCodeForToken(ctx context.Context, code string, provider string) error
}

// BrokerClient defines the interface for direct broker operations
// This is a generic, broker-agnostic interface that any broker can implement
type BrokerClient interface {
	// Core trading operations
	PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error)
	DeleteOrder(ctx context.Context, orderID string) error
	ModifyOrder(ctx context.Context, req OrderModificationRequest) (*OrderResponse, error)
	GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)
	CancelOrder(ctx context.Context, req CancelOrderRequest) error
	ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResponse, error)

	// Order and position queries
	GetOpenOrders(ctx context.Context) ([]LiveOrder, error)
	GetOpenPositions(ctx context.Context) (*SaxoOpenPositionsResponse, error)
	GetNetPositions(ctx context.Context) (*SaxoNetPositionsResponse, error)
	GetClosedPositions(ctx context.Context) (*SaxoClosedPositionsResponse, error)

	// Account and balance queries - generic, broker-agnostic
	GetBalance(ctx context.Context) (*Balance, error)
	GetAccounts(ctx context.Context) (*Accounts, error)
	GetMarginOverview(ctx context.Context, clientKey string) (*MarginOverview, error)
	GetClientInfo(ctx context.Context) (*ClientInfo, error)
	GetTradingSchedule(ctx context.Context, params TradingScheduleParams) (*TradingSchedule, error)

	// Instrument search and metadata (Tier 2 - The Usual Suspects)
	SearchInstruments(ctx context.Context, params InstrumentSearchParams) ([]Instrument, error)
	GetInstrumentDetails(ctx context.Context, uics []int) ([]InstrumentDetail, error)
	GetInstrumentPrices(ctx context.Context, uics []int, fieldGroups string) ([]InstrumentPriceInfo, error)

	// Market data operations (consolidated from MarketDataClient)
	Subscribe(ctx context.Context, instruments []string) (<-chan PriceUpdate, error)
	Unsubscribe(ctx context.Context, instruments []string) error
	GetInstrumentPrice(ctx context.Context, instrument Instrument) (*PriceData, error)
	GetHistoricalData(ctx context.Context, instrument Instrument, days int) ([]HistoricalDataPoint, error)
	GetAccountInfo(ctx context.Context) (*AccountInfo, error)
}

// WebSocketClient defines real-time data streaming interface
type WebSocketClient interface {
	Connect(ctx context.Context) error
	SubscribeToPrices(ctx context.Context, instruments []string) error
	SubscribeToOrders(ctx context.Context) error
	SubscribeToPortfolio(ctx context.Context) error
	SubscribeToSessionEvents(ctx context.Context) error
	GetPriceUpdateChannel() <-chan PriceUpdate
	GetOrderUpdateChannel() <-chan OrderUpdate
	GetPortfolioUpdateChannel() <-chan PortfolioUpdate
	SetStateChannels(stateChannel chan<- bool, contextIDChannel chan<- string)
	Close() error
}

// ============================================================================
// GENERIC DATA TYPES - Simple types for broker-agnostic operations
// ============================================================================

// Instrument represents a tradeable instrument
// This is a minimal broker-agnostic structure - clients may use enriched domain types
// The adapter layer translates between domain-specific and broker-specific types
type Instrument struct {
	Ticker      string
	Exchange    string
	AssetType   string
	Identifier  int // UIC for Saxo
	Uic         int // Alias for Identifier (for backward compatibility)
	Symbol      string
	Description string
	Currency    string
	TickSize    float64
	Decimals    int
}

// OrderRequest represents a broker order request
type OrderRequest struct {
	Instrument Instrument
	AccountKey string // Account identifier (required for most brokers)
	Side       string // "Buy" or "Sell"
	Size       int
	Price      float64
	OrderType  string // "Limit", "Market", "StopIfTraded", etc.
	Duration   string // "GoodTillDate", "DayOrder", etc.
}

// OrderResponse represents broker order response
type OrderResponse struct {
	OrderID      string
	Status       string
	Timestamp    string
	ExtendedData interface{} // For complex/OCO order responses
}

// OrderModificationRequest represents order modification parameters
type OrderModificationRequest struct {
	OrderID       string
	AccountKey    string
	OrderPrice    string
	OrderType     string
	AssetType     string
	OrderDuration struct {
		DurationType string
	}
}

// CancelOrderRequest represents a request to cancel an order
type CancelOrderRequest struct {
	OrderID    string
	AccountKey string
}

// ClosePositionRequest represents a request to close a position
type ClosePositionRequest struct {
	PositionID    string
	NetPositionID string
	AccountKey    string
	Uic           int
	AssetType     string
	Amount        float64
	BuySell       string
}

// OrderStatus represents current order status
type OrderStatus struct {
	OrderID string
	Status  string
	Price   float64
	Size    int
}

// LiveOrder represents order fetched from broker API
type LiveOrder struct {
	OrderID          string
	Uic              int
	Ticker           string
	Symbol           string // Display symbol from broker
	Description      string // Full description from broker
	AssetType        string
	OrderType        string
	Amount           float64
	Price            float64
	StopLimitPrice   float64
	OrderTime        time.Time
	Status           string
	RelatedOrders    []RelatedOrder
	BuySell          string
	OrderDuration    string
	OrderRelation    string
	AccountKey       string
	ClientKey        string
	DistanceToMarket float64
	IsMarketOpen     bool
	MarketPrice      float64
	OrderAmountType  string
}

// RelatedOrder represents OCO related order
type RelatedOrder struct {
	OrderID       string
	OpenOrderType string
	OrderPrice    float64
	Status        string
}

// PriceUpdate represents a price update from market data
type PriceUpdate struct {
	Ticker    string
	Bid       float64
	Ask       float64
	Mid       float64
	Timestamp time.Time
}

// PriceData represents current market pricing
type PriceData struct {
	Ticker    string  `json:"ticker"`
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	Mid       float64 `json:"mid"`
	Spread    float64 `json:"spread"`
	Timestamp string  `json:"timestamp"`
}

// HistoricalDataPoint represents OHLC historical data
type HistoricalDataPoint struct {
	Ticker string
	Time   time.Time // Time of the data point (consistent with PriceUpdate)
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// AccountInfo represents broker account information
type AccountInfo struct {
	AccountKey  string  `json:"account_key"`
	AccountType string  `json:"account_type"`
	Currency    string  `json:"currency"`
	Balance     float64 `json:"balance"`
	MarginUsed  float64 `json:"margin_used"`
	MarginFree  float64 `json:"margin_free"`
}

// Balance represents generic account balance information
// Type alias to SaxoBalance - broker-agnostic naming
type Balance = SaxoBalance

// Account represents a trading account
// Type alias to SaxoAccountInfo - broker-agnostic naming
type Account = SaxoAccountInfo

// Accounts represents a collection of trading accounts
// Type alias to SaxoAccounts - broker-agnostic naming
type Accounts = SaxoAccounts

// TradingScheduleParams represents parameters for querying trading schedule
// Type alias to SaxoTradingScheduleParams - broker-agnostic naming
type TradingScheduleParams = SaxoTradingScheduleParams

// TradingSchedule represents market open/close times for an instrument
// Type alias to SaxoTradingSchedule - broker-agnostic naming
type TradingSchedule = SaxoTradingSchedule

// TradingPhase represents a trading phase (open/close times)
// Type alias to SaxoTradingPhase - broker-agnostic naming
type TradingPhase = SaxoTradingPhase

// OpenPositionsResponse represents open positions response
// Type alias to SaxoOpenPositionsResponse - broker-agnostic naming
type OpenPositionsResponse = SaxoOpenPositionsResponse

// ClosedPositionsResponse represents closed positions response
// Type alias to SaxoClosedPositionsResponse - broker-agnostic naming
type ClosedPositionsResponse = SaxoClosedPositionsResponse

// NetPositionsResponse represents net positions response
// Type alias to SaxoNetPositionsResponse - broker-agnostic naming
type NetPositionsResponse = SaxoNetPositionsResponse

// MarginOverview represents margin breakdown by instrument group
// Type alias to SaxoMarginOverview - broker-agnostic naming
type MarginOverview = SaxoMarginOverview

// ClientInfo represents client/user information
// Type alias to SaxoClientInfo - broker-agnostic naming
type ClientInfo = SaxoClientInfo

// OrderUpdate represents real-time order status changes
type OrderUpdate struct {
	OrderId    string    `json:"order_id"`
	Status     string    `json:"status"`
	FilledSize float64   `json:"filled_size"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PortfolioUpdate represents real-time balance and position changes
type PortfolioUpdate struct {
	Balance    float64   `json:"balance"`
	MarginUsed float64   `json:"margin_used"`
	MarginFree float64   `json:"margin_free"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// InstrumentSearchParams represents parameters for instrument search
type InstrumentSearchParams struct {
	Keywords  string `json:"keywords"`
	AssetType string `json:"asset_type"`
	Exchange  string `json:"exchange"`
}

// InstrumentDetail represents detailed instrument information
type InstrumentDetail struct {
	Uic                   int       `json:"uic"`
	TickSize              float64   `json:"tick_size"`
	Decimals              int       `json:"decimals"`
	OrderDecimals         int       `json:"order_decimals"`
	ExpiryDate            time.Time `json:"expiry_date"`
	NoticeDate            time.Time `json:"notice_date"`
	PriceToContractFactor float64   `json:"price_to_contract_factor"`
	Format                string    `json:"format"` // "ModernFractions", "Normal", etc.
	NumeratorDecimals     int       `json:"numerator_decimals"`
}

// InstrumentPriceInfo represents price information for instrument selection
type InstrumentPriceInfo struct {
	Uic          int     `json:"uic"`
	OpenInterest float64 `json:"open_interest"`
	LastPrice    float64 `json:"last_price"`
}

// ============================================================================
// SAXO-SPECIFIC TYPES - Used internally and returned to clients
// These are in types.go but referenced here for interface completeness
// ============================================================================

// Note: Saxo-specific types like SaxoPortfolioBalance, SaxoAccounts, etc.
// are defined in types.go. These are Saxo Bank API response structures
// that this adapter returns directly to clients.
