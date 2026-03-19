package paypal

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
)

// startMockServer 启动本地 HTTP 服务模拟 PayPal REST API 端点。
// 仅用于 Demo / 测试环境。生产环境使用 NewClientWithBaseURL 指向真实 PayPal API。
func startMockServer() (string, func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoutes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("[PayPal MockServer] Failed to listen: %v", err)
	}
	addr := fmt.Sprintf("http://%s", listener.Addr().String())
	log.Printf("[PayPal MockServer] Started on %s", addr)

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	return addr, func() { server.Close() }
}

func handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path

	switch {
	// POST /v2/checkout/orders/{id}/authorize
	case strings.Contains(path, "/checkout/orders/") && strings.HasSuffix(path, "/authorize"):
		handleAuthorize(w, r)

	// POST /v2/payments/captures/{id}/capture
	case strings.Contains(path, "/payments/captures/") && strings.HasSuffix(path, "/capture"):
		handleCapture(w, r)

	// POST /v2/payments/captures/{id}/refund
	case strings.Contains(path, "/payments/captures/") && strings.HasSuffix(path, "/refund"):
		handleRefund(w, r)

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	params := parseBody(r)

	// 从 URL 中提取 Order ID
	parts := strings.Split(r.URL.Path, "/")
	orderID := ""
	for i, p := range parts {
		if p == "orders" && i+1 < len(parts) {
			orderID = parts[i+1]
			break
		}
	}

	// EC-DECLINE 前缀模拟无效/过期的 PayPal Order
	if strings.HasPrefix(orderID, "EC-DECLINE") {
		writeError(w, http.StatusUnprocessableEntity, "ORDER_NOT_APPROVED")
		return
	}

	_ = params // payer_id 等在 body 中，Demo 不校验

	writeJSON(w, map[string]any{
		"capture_id":  fmt.Sprintf("CAPTURE-%s", randHex(8)),
		"payer_email": "buyer@example.com",
	})
}

func handleCapture(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"status": "COMPLETED"})
}

func handleRefund(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"status": "COMPLETED"})
}

// ── 工具函数 ──────────────────────────────────────────────────────

func parseBody(r *http.Request) map[string]string {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	var params map[string]string
	json.Unmarshal(body, &params)
	if params == nil {
		params = make(map[string]string)
	}
	return params
}

func writeJSON(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func randHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		v, _ := rand.Int(rand.Reader, big.NewInt(256))
		b[i] = byte(v.Int64())
	}
	return fmt.Sprintf("%x", b)
}
