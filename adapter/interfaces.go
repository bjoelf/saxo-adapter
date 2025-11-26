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
type BrokerClient interface {
	PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error)
	DeleteOrder(ctx context.Context, orderID string) error
	ModifyOrder(ctx context.Context, req OrderModificationRequest) (*OrderResponse, error)
	GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)
	CancelOrder(ctx context.Context, req CancelOrderRequest) error
	ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResponse, error)
	GetOpenOrders(ctx context.Context) ([]LiveOrder, error)
	GetBalance(force bool) (*SaxoPortfolioBalance, error)
	GetAccounts(force bool) (*SaxoAccounts, error)
	GetTradingSchedule(params SaxoTradingScheduleParams) (SaxoTradingSchedule, error)
	GetOpenPositions(ctx context.Context) (*SaxoOpenPositionsResponse, error)
	GetNetPositions(ctx context.Context) (*SaxoNetPositionsResponse, error)
	GetClosedPositions(ctx context.Context) (*SaxoClosedPositionsResponse, error)
}

// MarketDataClient defines interface for market data operations
type MarketDataClient interface {
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
// This is a minimal structure - clients using this adapter should provide these fields
type Instrument struct {
	Ticker      string
	Exchange    string
	AssetType   string
	Identifier  int // UIC for Saxo
	Uic         int // Alias for Identifier (for backward compatibility)
	Symbol      string
	Description string
	Currency    string
	TickSize    float32
	Decimals    int
}

// OrderRequest represents a broker order request
type OrderRequest struct {
	Instrument Instrument
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
	Date   time.Time
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

// ============================================================================
// SAXO-SPECIFIC TYPES - Used internally and returned to clients
// These are in types.go but referenced here for interface completeness
// ============================================================================

// Note: Saxo-specific types like SaxoPortfolioBalance, SaxoAccounts, etc.
// are defined in types.go. These are Saxo Bank API response structures
// that this adapter returns directly to clients.
