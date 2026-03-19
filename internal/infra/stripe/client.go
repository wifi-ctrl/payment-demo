// Package stripe 提供 Stripe API 的共享 HTTP 客户端。
//
// 这是 infra 层的纯技术组件，无业务语义。多个上下文的 adapter（如 card 的
// StripeVaultAdapter、payment 的 StripeGatewayAdapter）共享同一个 Client 实例，
// 复用认证、连接池、序列化、日志等基础能力。
//
// Client 暴露 Stripe API 的 typed 方法（CreateToken、CreatePaymentIntent 等），
// 封装 URL 路径、请求格式、响应格式等 Stripe HTTP API 知识。
// adapter 层只需调用这些方法并做 ACL 翻译（Stripe 响应 → 领域模型），
// 不需要知道任何 HTTP/URL 细节。
//
// Demo 环境通过内嵌 mock server 模拟 Stripe 端点（见 mock_server.go），
// 生产环境替换 baseURL 为真实 Stripe API 即可。
package stripe

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

// Client Stripe HTTP 客户端。
// 封装认证头注入、请求序列化、响应反序列化、日志等纯技术职责。
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	closer     func() // mock server closer（生产环境为 nil）
}

// NewClient 创建指向真实 Stripe API 的客户端（生产环境）。
func NewClient(apiKey string) *Client {
	return NewClientWithBaseURL(apiKey, "https://api.stripe.com")
}

// NewClientWithBaseURL 创建指向指定 Base URL 的 Stripe 客户端。
func NewClientWithBaseURL(apiKey, baseURL string) *Client {
	log.Printf("[Stripe] Client initialized (apiKey=%s..., baseURL=%s)", safePrefix(apiKey, 10), baseURL)
	return &Client{
		httpClient: &http.Client{},
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

// NewMockClient 创建带内嵌 mock server 的 Stripe 客户端（Demo / 测试环境）。
// 调用 Close() 关闭 mock server 释放端口。
func NewMockClient(apiKey string) *Client {
	mockURL, closeFn := startMockServer()
	log.Printf("[Stripe] MockClient initialized (apiKey=%s..., baseURL=%s)", safePrefix(apiKey, 10), mockURL)
	return &Client{
		httpClient: &http.Client{},
		apiKey:     apiKey,
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

// ── Stripe API 入参/响应 DTO ──────────────────────────────────────

// TokenParams 创建 Token 的请求参数。
type TokenParams struct {
	OneTimeToken string // 前端一次性 Token
}

// TokenResult 创建 Token 的响应。
type TokenResult struct {
	ID        string // Vault 持久令牌 ID
	CardBrand string // 卡品牌（Visa、Mastercard 等）
	CardLast4 string // 卡号后四位
	ExpMonth  int    // 过期月
	ExpYear   int    // 过期年
}

// PaymentIntentParams 创建 PaymentIntent 的请求参数。
type PaymentIntentParams struct {
	Token    string // 支付令牌
	Amount   int64  // 金额（最小货币单位）
	Currency string // 币种（ISO 4217）
	APIKey   string // 商户专属 API Key
}

// PaymentIntentResult 创建 PaymentIntent 的响应。
type PaymentIntentResult struct {
	ID       string // PaymentIntent ID（ProviderRef）
	AuthCode string // 授权码
}

// CaptureParams 扣款请求参数。
type CaptureParams struct {
	Amount   int64
	Currency string
	APIKey   string
}

// RefundParams 退款请求参数。
type RefundParams struct {
	PaymentIntentID string
	Amount          int64
	Currency        string
	APIKey          string
}

// ── Stripe API 方法 ───────────────────────────────────────────────

// CreateToken 调用 Stripe Tokens API 创建持久令牌。
// POST /v1/tokens
func (c *Client) CreateToken(params TokenParams) (*TokenResult, error) {
	resp, err := c.post("/v1/tokens", map[string]string{
		"token": params.OneTimeToken,
	})
	if err != nil {
		return nil, fmt.Errorf("stripe CreateToken: %w", err)
	}
	return &TokenResult{
		ID:        resp["id"].(string),
		CardBrand: resp["card_brand"].(string),
		CardLast4: resp["card_last4"].(string),
		ExpMonth:  intFromAny(resp["exp_month"]),
		ExpYear:   intFromAny(resp["exp_year"]),
	}, nil
}

// DeleteToken 调用 Stripe API 删除 Token。
// DELETE /v1/tokens/{id}
func (c *Client) DeleteToken(tokenID string) error {
	return c.delete("/v1/tokens/" + tokenID)
}

// CreatePaymentIntent 调用 Stripe PaymentIntents API 创建授权。
// POST /v1/payment_intents
func (c *Client) CreatePaymentIntent(params PaymentIntentParams) (*PaymentIntentResult, error) {
	resp, err := c.post("/v1/payment_intents", map[string]string{
		"token":    params.Token,
		"amount":   fmt.Sprintf("%d", params.Amount),
		"currency": params.Currency,
		"api_key":  params.APIKey,
	})
	if err != nil {
		return nil, err
	}
	return &PaymentIntentResult{
		ID:       resp["id"].(string),
		AuthCode: resp["auth_code"].(string),
	}, nil
}

// CapturePaymentIntent 调用 Stripe PaymentIntents Capture API 扣款。
// POST /v1/payment_intents/{id}/capture
func (c *Client) CapturePaymentIntent(paymentIntentID string, params CaptureParams) error {
	_, err := c.post("/v1/payment_intents/"+paymentIntentID+"/capture", map[string]string{
		"amount":   fmt.Sprintf("%d", params.Amount),
		"currency": params.Currency,
		"api_key":  params.APIKey,
	})
	return err
}

// CreateRefund 调用 Stripe Refunds API 退款。
// POST /v1/refunds
func (c *Client) CreateRefund(params RefundParams) error {
	_, err := c.post("/v1/refunds", map[string]string{
		"payment_intent": params.PaymentIntentID,
		"amount":         fmt.Sprintf("%d", params.Amount),
		"currency":       params.Currency,
		"api_key":        params.APIKey,
	})
	return err
}

// ── 私有 HTTP 方法 ────────────────────────────────────────────────

func (c *Client) post(path string, params map[string]string) (map[string]any, error) {
	body, _ := json.Marshal(params)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("stripe: create request failed: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	log.Printf("[Stripe] POST %s (apiKey=%s...)", path, safePrefix(c.apiKey, 10))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.parseResponse(resp)
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("stripe: create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	log.Printf("[Stripe] DELETE %s (apiKey=%s...)", path, safePrefix(c.apiKey, 10))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("stripe: DELETE %s returned %d", path, resp.StatusCode)
	}
	return nil
}

func (c *Client) parseResponse(resp *http.Response) (map[string]any, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("stripe: read response failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("stripe: API error %d: %s", resp.StatusCode, string(respBody))
	}
	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("stripe: parse response failed: %w", err)
	}
	return data, nil
}

// ── 工具函数 ──────────────────────────────────────────────────────

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}
