// Package paypal 提供 PayPal REST API 的 HTTP 客户端。
//
// 这是 acquiring 上下文的适配器实现细节，封装 PayPal OAuth 认证、HTTP 通信、序列化。
// PayPal GatewayAdapter 通过 typed 方法调用此客户端，
// 只需做 ACL 翻译（PayPal 响应 → 领域模型），不关心 HTTP 细节。
//
// Demo 环境通过内嵌 mock server 模拟 PayPal 端点（见 mock_server.go）。
package paypal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// Client PayPal HTTP 客户端。
type Client struct {
	httpClient *http.Client
	baseURL    string
	closer     func()
}

func NewClient(baseURL string) *Client {
	return NewClientWithBaseURL(baseURL)
}

func NewClientWithBaseURL(baseURL string) *Client {
	log.Printf("[PayPal] Client initialized (baseURL=%s)", baseURL)
	return &Client{
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

func NewMockClient() *Client {
	mockURL, closeFn := startMockServer()
	log.Printf("[PayPal] MockClient initialized (baseURL=%s)", mockURL)
	return &Client{
		httpClient: &http.Client{},
		baseURL:    mockURL,
		closer:     closeFn,
	}
}

func (c *Client) Close() {
	if c.closer != nil {
		c.closer()
	}
}

// ── PayPal API DTO ────────────────────────────────────────────────

type AuthorizeOrderParams struct {
	OrderID      string
	PayerID      string
	ClientID     string
	ClientSecret string
}

type AuthorizeOrderResult struct {
	CaptureID  string
	PayerEmail string
}

type CaptureParams struct {
	Amount       int64
	Currency     string
	ClientID     string
	ClientSecret string
}

type RefundParams struct {
	Amount       int64
	Currency     string
	ClientID     string
	ClientSecret string
}

// ── PayPal API 方法 ───────────────────────────────────────────────

func (c *Client) AuthorizeOrder(params AuthorizeOrderParams) (*AuthorizeOrderResult, error) {
	resp, err := c.post("/v2/checkout/orders/"+params.OrderID+"/authorize", map[string]string{
		"payer_id":      params.PayerID,
		"client_id":     params.ClientID,
		"client_secret": params.ClientSecret,
	})
	if err != nil {
		return nil, err
	}
	return &AuthorizeOrderResult{
		CaptureID:  resp["capture_id"].(string),
		PayerEmail: resp["payer_email"].(string),
	}, nil
}

func (c *Client) CapturePayment(captureID string, params CaptureParams) error {
	_, err := c.post("/v2/payments/captures/"+captureID+"/capture", map[string]string{
		"amount":        fmt.Sprintf("%d", params.Amount),
		"currency":      params.Currency,
		"client_id":     params.ClientID,
		"client_secret": params.ClientSecret,
	})
	return err
}

func (c *Client) RefundPayment(captureID string, params RefundParams) error {
	_, err := c.post("/v2/payments/captures/"+captureID+"/refund", map[string]string{
		"amount":        fmt.Sprintf("%d", params.Amount),
		"currency":      params.Currency,
		"client_id":     params.ClientID,
		"client_secret": params.ClientSecret,
	})
	return err
}

// ── 私有 HTTP 方法 ────────────────────────────────────────────────

func (c *Client) post(path string, params map[string]string) (map[string]any, error) {
	body, _ := json.Marshal(params)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("paypal: create request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	log.Printf("[PayPal] POST %s", path)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paypal: request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.parseResponse(resp)
}

func (c *Client) parseResponse(resp *http.Response) (map[string]any, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("paypal: read response failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("paypal: API error %d: %s", resp.StatusCode, string(respBody))
	}
	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("paypal: parse response failed: %w", err)
	}
	return data, nil
}
