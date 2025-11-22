package saxo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bjoelf/pivot-web2/internal/ports"
)

// SaxoInstrumentAdapter implements Saxo Bank API integration for instrument enrichment
type SaxoInstrumentAdapter struct {
	httpClient *http.Client
	authClient ports.AuthClient
	baseURL    string
}

// NewSaxoInstrumentAdapter creates a new Saxo instrument adapter
func NewSaxoInstrumentAdapter(httpClient *http.Client, authClient ports.AuthClient, baseURL string) ports.SaxoInstrumentAdapter {
	return &SaxoInstrumentAdapter{
		httpClient: httpClient,
		authClient: authClient,
		baseURL:    baseURL,
	}
}

// SearchInstruments implements legacy GetSaxoInstruments functionality
func (s *SaxoInstrumentAdapter) SearchInstruments(ctx context.Context, params ports.SaxoSearchParams) (*ports.SaxoInstrumentResponse, error) {
	// Build URL following legacy pattern: /ref/v1/instruments/?AssetType=X&ExchangeId=Y&Keywords=Z&Skip=0
	endpoint := "/ref/v1/instruments/"

	urlParams := url.Values{}
	urlParams.Set("AssetType", params.AssetType)
	urlParams.Set("ExchangeId", params.ExchangeId)
	urlParams.Set("Keywords", params.Keywords)
	urlParams.Set("Skip", "0")

	fullURL := s.baseURL + endpoint + "?" + urlParams.Encode()

	responseData, err := s.makeAPIRequest(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search instruments: %w", err)
	}

	// Parse response following legacy SaxoInstrument structure
	var response saxoInstrumentAPIResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse instrument response: %w", err)
	}

	// Convert to ports response format
	result := &ports.SaxoInstrumentResponse{
		Instruments: make([]ports.SaxoInstrument, len(response.Data)),
	}

	for i, instrument := range response.Data {
		result.Instruments[i] = ports.SaxoInstrument{
			Identifier:   instrument.Identifier,
			Symbol:       instrument.Symbol,
			Description:  instrument.Description,
			AssetType:    instrument.AssetType,
			ExchangeID:   instrument.ExchangeID,
			CurrencyCode: instrument.CurrencyCode,
		}
	}

	return result, nil
}

// GetInstrumentDetails implements legacy GetSaxoInstrumentsDetails functionality
func (s *SaxoInstrumentAdapter) GetInstrumentDetails(ctx context.Context, identifiers []string) (*ports.SaxoInstrumentDetailsResponse, error) {
	// Build URL following legacy pattern: /ref/v1/instruments/details?Uics=34776117,33143447
	endpoint := "/ref/v1/instruments/details"

	urlParams := url.Values{}
	urlParams.Set("Uics", strings.Join(identifiers, ","))

	fullURL := s.baseURL + endpoint + "?" + urlParams.Encode()

	responseData, err := s.makeAPIRequest(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get instrument details: %w", err)
	}

	// Parse response following legacy SaxoInstrumentsDetails structure
	var response saxoInstrumentDetailsAPIResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse instrument details response: %w", err)
	}

	// Convert to ports response format
	result := &ports.SaxoInstrumentDetailsResponse{
		Data: make([]ports.SaxoInstrumentDetail, len(response.Data)),
	}

	for i, detail := range response.Data {
		result.Data[i] = ports.SaxoInstrumentDetail{
			Identifier:            detail.Identifier,
			TickSize:              detail.TickSize,
			ExpiryDate:            detail.ExpiryDate,
			NoticeDate:            detail.NoticeDate,
			PriceToContractFactor: detail.PriceToContractFactor,
			Format: ports.SaxoInstrumentFormat{
				Decimals:          detail.Format.Decimals,
				OrderDecimals:     detail.Format.OrderDecimals,
				Format:            detail.Format.Format,
				NumeratorDecimals: detail.Format.NumeratorDecimals,
			},
		}
	}

	return result, nil
}

// GetContractPrices implements legacy GetSaxoInfoPrices functionality for futures contract selection
func (s *SaxoInstrumentAdapter) GetContractPrices(ctx context.Context, params ports.SaxoPriceParams) (*ports.SaxoPriceResponse, error) {
	// Build URL following legacy pattern - using the correct legacy endpoint
	endpoint := "/trade/v1/infoprices/list"

	urlParams := url.Values{}
	urlParams.Set("AssetType", params.AssetType)
	urlParams.Set("Uics", strings.Join(params.Uics, ","))
	urlParams.Set("FieldGroups", params.FieldGroups)

	fullURL := s.baseURL + endpoint + "?" + urlParams.Encode()

	// Debug logging for API request
	fmt.Printf("Saxo API Request: %s\n", fullURL)
	fmt.Printf("Request parameters: AssetType=%s, Uics=%s, FieldGroups=%s\n",
		params.AssetType, strings.Join(params.Uics, ","), params.FieldGroups)

	responseData, err := s.makeAPIRequest(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get contract prices: %w", err)
	}

	// Parse response following legacy SaxoInfoPrices structure
	var response saxoInfoPricesAPIResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse price response: %w", err)
	}

	// Convert to ports response format
	result := &ports.SaxoPriceResponse{
		Data: make([]ports.SaxoPriceData, len(response.Data)),
	}

	for i, priceData := range response.Data {
		result.Data[i] = ports.SaxoPriceData{
			Uic: priceData.Uic,
			InstrumentPriceDetails: ports.SaxoInstrumentPrice{
				OpenInterest: priceData.InstrumentPriceDetails.OpenInterest,
			},
		}
	}

	return result, nil
}

// makeAPIRequest handles the actual HTTP request with authentication
func (s *SaxoInstrumentAdapter) makeAPIRequest(ctx context.Context, fullURL string) ([]byte, error) {
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication headers
	token, err := s.authClient.GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// Internal API response types that match legacy broker package structures

type saxoInstrumentAPIResponse struct {
	Data []saxoInstrumentData `json:"Data"`
}

type saxoInstrumentData struct {
	Identifier   int      `json:"Identifier"`
	Symbol       string   `json:"Symbol"`
	Description  string   `json:"Description"`
	AssetType    string   `json:"AssetType"`
	ExchangeID   string   `json:"ExchangeId"`
	CurrencyCode string   `json:"CurrencyCode"`
	GroupID      int      `json:"GroupId"`
	TradableAs   []string `json:"TradableAs"`
}

type saxoInstrumentDetailsAPIResponse struct {
	Count int                        `json:"__count"`
	Data  []saxoInstrumentDetailData `json:"Data"`
}

type saxoInstrumentDetailData struct {
	Identifier            int                  `json:"Identifier"`
	TickSize              float64              `json:"TickSize"`
	ExpiryDate            string               `json:"ExpiryDate"`
	NoticeDate            string               `json:"NoticeDate"`
	PriceToContractFactor float64              `json:"PriceToContractFactor"`
	Format                saxoInstrumentFormat `json:"Format"`
	ContractSize          float64              `json:"ContractSize"`
	AmountDecimals        int                  `json:"AmountDecimals"`
}

type saxoInstrumentFormat struct {
	Decimals          int    `json:"Decimals"`
	OrderDecimals     int    `json:"OrderDecimals"`
	Format            string `json:"Format"`
	NumeratorDecimals int    `json:"NumeratorDecimals"`
}

type saxoInfoPricesAPIResponse struct {
	Data []saxoInfoPriceData `json:"Data"`
}

type saxoInfoPriceData struct {
	Uic                    int                        `json:"Uic"`
	InstrumentPriceDetails saxoInstrumentPriceDetails `json:"InstrumentPriceDetails"`
}

type saxoInstrumentPriceDetails struct {
	OpenInterest float64 `json:"OpenInterest"`
}
