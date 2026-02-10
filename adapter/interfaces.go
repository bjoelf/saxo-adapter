package saxo

import (
	"context"
	"net/http"
	"time"
)

// ============================================================================
// INTERFACES - These define the contracts this adapter implements
// ============================================================================
// INTERFACE SEGREGATION: Focused interfaces enable multi-broker support
// - Brokers implement only what they support (e.g., IB without historical data)
// - Services depend on specific interfaces for better testability
// - Composite BrokerClient maintains backward compatibility
//
// See: https://github.com/bjoelf/pivot-web2/blob/main/docs/api/INTERFACE_REMAPPING_TLDR.md
// ============================================================================

// AuthClient defines OAuth authentication interface for broker connections
type AuthClient interface {
	GetHTTPClient(ctx context.Context) (*http.Client, error)
	IsAuthenticated() bool
	GetAccessToken() (string, error)
	Login(ctx context.Context) error
	Logout() error
	RefreshToken(ctx context.Context) error
	ReauthorizeWebSocket(ctx context.Context, contextID string) error
	StartAuthenticationKeeper(provider string)
	GetBaseURL() string
	GetWebSocketURL() string
	SetRedirectURL(provider string, redirectURL string) error
	BuildRedirectURL(host string, provider string) string
	GenerateAuthURL(provider string, state string) (string, error)
	ExchangeCodeForToken(ctx context.Context, code string, provider string) error
}

// ============================================================================
// SEGREGATED INTERFACES - Enable incomplete implementations
// ============================================================================

// OrderClient defines order management operations
// All brokers that support trading must implement this interface
type OrderClient interface {
	// Order placement and modification
	PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error)
	ModifyOrder(ctx context.Context, req OrderModificationRequest) (*OrderResponse, error)
	DeleteOrder(ctx context.Context, orderID string) error
	CancelOrder(ctx context.Context, req CancelOrderRequest) error

	// Order queries
	GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)
	GetOpenOrders(ctx context.Context) ([]LiveOrder, error)

	// Position closing (market order to close)
	ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResponse, error)
}

// AccountClient defines account and balance operations
// All brokers must implement this interface
type AccountClient interface {
	// Account information
	GetAccounts(ctx context.Context) (*Accounts, error)
	GetAccountInfo(ctx context.Context) (*AccountInfo, error)
	GetClientInfo(ctx context.Context) (*ClientInfo, error)

	// Balance and margin
	GetBalance(ctx context.Context) (*Balance, error)
	GetMarginOverview(ctx context.Context, clientKey string) (*MarginOverview, error)
}

// MarketDataClient defines market data operations
// OPTIONAL: Not all brokers provide historical data (e.g., Interactive Brokers)
// Services should check capability: if mdClient, ok := broker.(MarketDataClient); ok { ... }
type MarketDataClient interface {
	// Instrument pricing (HTTP REST - for on-demand queries)
	GetInstrumentPrice(ctx context.Context, instrument Instrument) (*PriceData, error)

	// Historical data (OHLC bars)
	// Note: IB does not provide this - use Saxo or third-party data vendor
	GetHistoricalData(ctx context.Context, instrument Instrument, days int) ([]HistoricalDataPoint, error)

	// Trading schedule (market hours)
	GetTradingSchedule(ctx context.Context, params TradingScheduleParams) (*TradingSchedule, error)
}

// PositionClient defines position query operations
// All brokers that support trading must implement this interface
type PositionClient interface {
	GetOpenPositions(ctx context.Context) (*OpenPositionsResponse, error)
	GetClosedPositions(ctx context.Context) (*ClosedPositionsResponse, error)
	GetNetPositions(ctx context.Context) (*NetPositionsResponse, error)
}

// InstrumentClient defines instrument search and metadata operations
// OPTIONAL: Brokers may have different instrument lookup capabilities
type InstrumentClient interface {
	// Instrument search and metadata
	SearchInstruments(ctx context.Context, params InstrumentSearchParams) ([]Instrument, error)
	GetInstrumentDetails(ctx context.Context, uics []int) ([]InstrumentDetail, error)
	GetInstrumentPrices(ctx context.Context, uics []int, fieldGroups string, assetType string) ([]InstrumentPriceInfo, error)
}

// ============================================================================
// COMPOSITE INTERFACE - Backward compatibility
// ============================================================================
// BrokerClient combines all focused interfaces for backward compatibility.
// Existing code using BrokerClient continues to work unchanged.
//
// New implementations can implement specific interfaces based on capabilities:
// - Saxo: Implements all interfaces (OrderClient + AccountClient + MarketDataClient + PositionClient + InstrumentClient)
// - IB: May implement OrderClient + AccountClient + PositionClient (without MarketDataClient)
// - Data vendor: May implement only MarketDataClient
//
// Example service migration:
//   Old: func NewTradingService(broker BrokerClient)
//   New: func NewTradingService(orders OrderClient, accounts AccountClient, positions PositionClient)
// ============================================================================

// BrokerClient defines the complete interface for broker operations (composite)
// This is the full interface that Saxo adapter implements.
// Future brokers may implement only a subset (e.g., IB without MarketDataClient).
type BrokerClient interface {
	OrderClient
	AccountClient
	MarketDataClient
	PositionClient
	InstrumentClient
}

// WebSocketClient defines real-time data streaming interface
type WebSocketClient interface {
	Connect(ctx context.Context) error
	SubscribeToPrices(ctx context.Context, instruments []string, assetType string) error // assetType: "FxSpot", "ContractFutures", etc.
	SubscribeToOrders(ctx context.Context) error
	SubscribeToPortfolio(ctx context.Context) error
	SubscribeToSessionEvents(ctx context.Context) error
	GetPriceUpdateChannel() <-chan PriceUpdate
	GetOrderUpdateChannel() <-chan OrderUpdate
	GetPortfolioUpdateChannel() <-chan PortfolioUpdate
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
	OrderID        string
	Uic            int
	Ticker         string
	AssetType      string
	OrderType      string
	Amount         float64
	Price          float64
	StopLimitPrice float64
	OrderTime      time.Time
	Status         string
	RelatedOrders  []RelatedOrder
	BuySell        string
	OrderDuration  string
	OrderRelation  string
	AccountKey     string
	ClientKey      string

	// Display information
	DisplayAndFormat struct {
		Currency    string
		Decimals    int
		Description string
		Format      string
		Symbol      string
	}

	// Market conditions
	DistanceToMarket float64
	IsMarketOpen     bool
	MarketPrice      float64
	OrderAmountType  string
}

// RelatedOrder represents OCO/IfDone related order
// Used in both HTTP responses (LiveOrder.RelatedOrders) and WebSocket updates (OrderUpdate.RelatedOpenOrders)
type RelatedOrder struct {
	OrderID       string  `json:"OrderId"`
	OpenOrderType string  `json:"OpenOrderType"` // "Limit", "StopIfTraded", "Stop"
	OrderPrice    float64 `json:"OrderPrice"`
	Amount        float64 `json:"Amount,omitempty"` // WebSocket includes this
	Status        string  `json:"Status"`
	MetaDeleted   *bool   `json:"__meta_deleted,omitempty"` // WebSocket deletion marker
}

// PriceUpdate represents a price update from market data
// Uses Saxo's native UIC (Universal Instrument Code) for matching
type PriceUpdate struct {
	Uic       int // Saxo's Universal Instrument Code (matches Instrument.Identifier)
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

// Balance represents generic account balance information
// Type alias to SaxoBalance - broker-agnostic naming
type Balance = SaxoBalance

// AccountInfo represents a trading account with full details
// Type alias to SaxoAccountInfo - includes CreationDate, AccountKey, Currency, etc.
type AccountInfo = SaxoAccountInfo

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
// Enhanced to handle both Phase 1 (entry with RelatedOpenOrders) and Phase 2 (flat structure)
// Following legacy pivot-web/strategy_manager/streaming_orders.go:13-75
type OrderUpdate struct {
	// Core fields (always present)
	OrderId    string    `json:"OrderId"`
	Status     string    `json:"Status,omitempty"`
	FilledSize float64   `json:"FilledAmount,omitempty"`
	UpdatedAt  time.Time `json:"-"` // Set internally, not from JSON

	// Phase 1 & 2 tracking fields
	OpenOrderType string  `json:"OpenOrderType,omitempty"` // "StopLimit", "Limit", "StopIfTraded", "Stop"
	OrderPrice    float64 `json:"Price,omitempty"`
	Uic           *int    `json:"Uic,omitempty"`
	Amount        *int    `json:"Amount,omitempty"`

	// Phase 1: Nested structure (entry order with related exit orders)
	// Following legacy pivot-web/strategy_manager/streaming_orders.go:66-70
	RelatedOpenOrders []RelatedOrder `json:"RelatedOpenOrders,omitempty"`

	// Order deletion marker (Phase 2: when entry fills)
	MetaDeleted *bool `json:"__meta_deleted,omitempty"`
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
