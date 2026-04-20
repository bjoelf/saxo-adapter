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
// SEGREGATED INTERFACES - Enable multi-broker support with incomplete implementations
// ============================================================================
// These focused interfaces allow brokers to implement only what they support.
// Example: Interactive Brokers adapter can implement OrderClient + AccountClient
// without MarketDataClient (IB doesn't provide historical data API).
//
// Services can check capabilities at runtime and fall back to alternative providers:
//   if mdClient, ok := broker.(MarketDataClient); ok {
//       // Use broker's market data
//   } else {
//       // Fall back to dedicated data provider
//   }
//
// This is a LIBRARY ARCHITECTURE - consumers should depend on specific interfaces,
// not the monolithic BrokerClient, for better testability and clearer dependencies.
// ============================================================================

// OrderClient defines order management operations
// All brokers that support trading must implement this interface
type OrderClient interface {
	// Order placement and modification
	PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResponse, error)
	ModifyOrder(ctx context.Context, req OrderModificationRequest) (*OrderResponse, error)
	GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error)

	// Order cancellation and position closing
	CancelOrder(ctx context.Context, req CancelOrderRequest) error
	ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResponse, error)

	// Order queries
	GetOpenOrders(ctx context.Context) ([]LiveOrder, error)
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

	// Session management
	// SetSessionCapabilities requests a trade level upgrade (e.g., "FullTradingAndChat" for real-time data).
	// Call this when GetSessionEventChannel() delivers an event with TradeLevel != "FullTradingAndChat".
	// Reference: Saxo API PATCH /root/v1/sessions/capabilities
	SetSessionCapabilities(ctx context.Context, tradeLevel string) error
}

// MarketDataClient defines market data operations (HTTP REST)
// OPTIONAL: Not all brokers provide historical data (e.g., Interactive Brokers)
// Services should check capability: if mdClient, ok := broker.(MarketDataClient); ok { ... }
//
// For real-time streaming, use WebSocketClient (see below)
type MarketDataClient interface {
	// Instrument pricing (HTTP REST - for on-demand queries)
	GetInstrumentPrice(ctx context.Context, instrument Instrument) (*PriceData, error)

	// Historical data (OHLC bars with cutoff time support)
	// Note: IB does not provide this - use Saxo or third-party data vendor
	GetHistoricalData(ctx context.Context, instrument Instrument, days int, cutoffTime time.Time) ([]HistoricalDataPoint, error)

	// Trading schedule (market hours)
	GetTradingSchedule(ctx context.Context, params TradingScheduleParams) (*TradingSchedule, error)

	// Instrument search and metadata
	// These are typically only available from full-service brokers
	SearchInstruments(ctx context.Context, params InstrumentSearchParams) ([]Instrument, error)
	GetInstrumentDetails(ctx context.Context, uics []int) ([]InstrumentDetail, error)
	GetInstrumentPrices(ctx context.Context, uics []int, fieldGroups string, assetType string) ([]InstrumentPriceInfo, error)
}

// PositionClient defines position query operations
// All brokers that support trading must implement this interface
type PositionClient interface {
	GetOpenPositions(ctx context.Context) (*OpenPositionsResponse, error)
	GetClosedPositions(ctx context.Context) (*ClosedPositionsResponse, error)
	GetNetPositions(ctx context.Context) (*NetPositionsResponse, error)
	GetHistoricalPositions(ctx context.Context, clientKey, fromDate, toDate string) (*HistoricalPositionsResponse, error)
}

// ============================================================================
// COMPOSITE INTERFACE - Backward compatibility
// ============================================================================
// BrokerClient combines all focused interfaces for backward compatibility.
// Existing code using BrokerClient continues to work unchanged.
//
// New services should depend on specific interfaces (OrderClient, AccountClient, etc.)
// for better testability and clearer dependencies.
//
// Example migration:
//   Old: func NewTradingService(broker BrokerClient) *TradingService
//   New: func NewTradingService(orders OrderClient, accounts AccountClient, positions PositionClient) *TradingService
//
// The composite pattern allows:
// 1. Full implementation (Saxo implements all interfaces)
// 2. Partial implementation (IB implements only OrderClient + AccountClient + PositionClient)
// 3. Composite broker (Router that delegates to multiple providers based on capability)
// ============================================================================

// BrokerClient defines the complete interface for broker operations (composite)
// This is the full interface that Saxo adapter implements.
// Future brokers may implement only a subset (e.g., IB without MarketDataClient).
type BrokerClient interface {
	OrderClient
	AccountClient
	MarketDataClient
	PositionClient
}

// WebSocketClient defines real-time data streaming interface
type WebSocketClient interface {
	Connect(ctx context.Context) error
	SubscribeToPrices(ctx context.Context, instruments []string, assetType string) error // assetType: "FxSpot", "ContractFutures", etc.
	SubscribeToOrders(ctx context.Context) error
	SubscribeToPortfolio(ctx context.Context) error
	// SubscribeToSessionEvents subscribes to session state events.
	// The snapshot from the HTTP POST response is pushed as the first event to the session channel.
	// Consumers should read GetSessionEventChannel() and call SetSessionCapabilities("FullTradingAndChat") when needed.
	SubscribeToSessionEvents(ctx context.Context) error
	GetPriceUpdateChannel() <-chan PriceUpdate
	GetOrderUpdateChannel() <-chan OrderUpdate
	GetPortfolioUpdateChannel() <-chan PortfolioUpdate
	GetSessionEventChannel() <-chan SessionUpdate
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
// Supports both single-leg orders and multi-leg orders (complex/OCO)
type OrderRequest struct {
	Instrument Instrument
	AccountKey string // Account identifier (required for most brokers)
	Side       string // "Buy" or "Sell"
	Size       int
	Price      float64
	OrderType  string // "Limit", "Market", "StopIfTraded", "StopLimit", etc.
	Duration   string // "GoodTillDate", "DayOrder", etc.

	// Multi-leg order support (for complex/OCO orders)
	// Related orders inherit AccountKey, Uic, and AssetType from main order
	RelatedOrders []RelatedOrderRequest

	// Optional fields for specific order types
	StopLimitPrice float64 // For StopLimit orders (futures)
}

// RelatedOrderRequest represents a related order in multi-leg order structures
// Used for complex orders (entry + OCO exit) and OCO orders (target + stop)
// Per Saxo API: Related orders inherit AccountKey, Uic, AssetType from parent order
type RelatedOrderRequest struct {
	Side      string  // "Buy" or "Sell"
	OrderType string  // "Limit", "StopIfTraded", etc.
	Price     float64 // Order price
	Duration  string  // "DayOrder", "GoodTillDate", etc.
}

// OrderResponse represents broker order response
type OrderResponse struct {
	OrderID         string
	Status          string
	Timestamp       string
	RelatedOrderIDs []string // Child order IDs in placement sequence: [0]=Target(Limit), [1]=Stop
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

// HistoricalPositionsResponse represents the account-history positions response
// Type alias to SaxoHistoricalPositionsResponse - broker-agnostic naming
type HistoricalPositionsResponse = SaxoHistoricalPositionsResponse

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
	OrderRelation string  `json:"OrderRelation,omitempty"` // "IfDoneMaster", "IfDoneSlaveOco", "Oco", "StandAlone"

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
