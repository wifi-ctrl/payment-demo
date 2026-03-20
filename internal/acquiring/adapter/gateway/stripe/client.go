// Package stripe 提供 Stripe API 的 HTTP 客户端。
//
// 这是 acquiring 上下文的适配器实现细节，封装 Stripe HTTP API 知识
// （URL 路径、请求/响应格式、认证头注入）。
// StripeGateway 调用 Client 的 typed 方法并做 ACL 翻译（Stripe 响应 → 领域模型），
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

// Client Stripe HTTP 客户端。
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	closer     func()
}

func NewClient(apiKey string) *Client {
	return NewClientWithBaseURL(apiKey, "https://api.stripe.com")
}

func NewClientWithBaseURL(apiKey, baseURL string) *Client {
	log.Printf("[Stripe] Client initialized (apiKey=%s..., baseURL=%s)", safePrefix(apiKey, 10), baseURL)
	return &Client{
		httpClient: &http.Client{},
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

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

func (c *Client) Close() {
	if c.closer != nil {
		c.closer()
	}
}

// ── Stripe API DTO ────────────────────────────────────────────────

type TokenParams struct {
	OneTimeToken string
}

type TokenResult struct {
	ID        string
	CardBrand string
	CardLast4 string
	ExpMonth  int
	ExpYear   int
}

type PaymentIntentParams struct {
	Token    string
	Amount   int64
	Currency string
	APIKey   string
}

type PaymentIntentResult struct {
	ID             string
	AuthCode       string
	RecurringToken string
}

type CaptureParams struct {
	Amount   int64
	Currency string
	APIKey   string
}

type RefundParams struct {
	PaymentIntentID string
	Amount          int64
	Currency        string
	APIKey          string
}

// ── Stripe API 方法 ───────────────────────────────────────────────

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

func (c *Client) DeleteToken(tokenID string) error {
	return c.delete("/v1/tokens/" + tokenID)
}

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
	result := &PaymentIntentResult{
		ID:       resp["id"].(string),
		AuthCode: resp["auth_code"].(string),
	}
	if rt, ok := resp["recurring_token"].(string); ok {
		result.RecurringToken = rt
	}
	return result, nil
}

func (c *Client) CapturePaymentIntent(paymentIntentID string, params CaptureParams) error {
	_, err := c.post("/v1/payment_intents/"+paymentIntentID+"/capture", map[string]string{
		"amount":   fmt.Sprintf("%d", params.Amount),
		"currency": params.Currency,
		"api_key":  params.APIKey,
	})
	return err
}

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
