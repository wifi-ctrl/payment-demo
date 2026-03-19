// Package paypal 提供 PayPal REST API 的共享 HTTP 客户端。
//
// 与 infra/stripe 对称 — 纯技术组件，封装 PayPal OAuth 认证、HTTP 通信、序列化。
// payment 上下文的 PayPalGatewayAdapter 通过 typed 方法调用此客户端，
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

// ── Client ────────────────────────────────────────────────────────

// Client PayPal HTTP 客户端。
// 封装 OAuth 认证（Client ID + Secret）、请求序列化、响应反序列化。
type Client struct {
	httpClient *http.Client
	baseURL    string
	closer     func()
}

// NewClient 创建指向真实 PayPal API 的客户端（生产环境）。
func NewClient(baseURL string) *Client {
	return NewClientWithBaseURL(baseURL)
}

// NewClientWithBaseURL 创建指向指定 Base URL 的 PayPal 客户端。
// sandbox: "https://api-m.sandbox.paypal.com"
// production: "https://api-m.paypal.com"
func NewClientWithBaseURL(baseURL string) *Client {
	log.Printf("[PayPal] Client initialized (baseURL=%s)", baseURL)
	return &Client{
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

// NewMockClient 创建带内嵌 mock server 的 PayPal 客户端（Demo / 测试环境）。
// 调用 Close() 关闭 mock server 释放端口。
func NewMockClient() *Client {
	mockURL, closeFn := startMockServer()
	log.Printf("[PayPal] MockClient initialized (baseURL=%s)", mockURL)
	return &Client{
		httpClient: &http.Client{},
		baseURL:    mockURL,
		closer:     closeFn,
	}
}

// Close 关闭 mock server（如有）。生产环境调用无副作用。
func (c *Client) Close() {
	if c.closer != nil {
		c.closer()
	}
}

// ── PayPal API 入参/响应 DTO ──────────────────────────────────────

// AuthorizeOrderParams 授权 Order 的请求参数。
type AuthorizeOrderParams struct {
	OrderID      string // 前端 JS SDK 返回的 Order ID
	PayerID      string // 付款方 ID
	ClientID     string // 商户 PayPal Client ID
	ClientSecret string // 商户 PayPal Client Secret
}

// AuthorizeOrderResult 授权 Order 的响应。
type AuthorizeOrderResult struct {
	CaptureID  string // PayPal Capture ID（ProviderRef）
	PayerEmail string // 付款方邮箱
}

// CaptureParams 扣款请求参数。
type CaptureParams struct {
	Amount       int64
	Currency     string
	ClientID     string
	ClientSecret string
}

// RefundParams 退款请求参数。
type RefundParams struct {
	Amount       int64
	Currency     string
	ClientID     string
	ClientSecret string
}

// ── PayPal API 方法 ───────────────────────────────────────────────

// AuthorizeOrder 调用 PayPal Orders API 授权订单。
// POST /v2/checkout/orders/{id}/authorize
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

// CapturePayment 调用 PayPal Captures API 扣款。
// POST /v2/payments/captures/{id}/capture
func (c *Client) CapturePayment(captureID string, params CaptureParams) error {
	_, err := c.post("/v2/payments/captures/"+captureID+"/capture", map[string]string{
		"amount":        fmt.Sprintf("%d", params.Amount),
		"currency":      params.Currency,
		"client_id":     params.ClientID,
		"client_secret": params.ClientSecret,
	})
	return err
}

// RefundPayment 调用 PayPal Refunds API 退款。
// POST /v2/payments/captures/{id}/refund
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

	// PayPal 使用 OAuth Bearer Token（生产环境需先 /v1/oauth2/token 换取）
	// Demo 中直接传 client_id 在 body 中
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
