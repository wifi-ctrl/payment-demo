package stripe

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

// startMockServer 启动本地 HTTP 服务模拟 Stripe API 端点。
// 仅用于 Demo / 测试环境。生产环境使用 NewClientWithBaseURL 指向真实 Stripe API。
//
// 模拟逻辑集中在此文件 — Client 本身是纯粹的 HTTP 客户端，不包含任何模拟代码。
// 替换为真实 Stripe 时，只需删除此文件并使用 NewClientWithBaseURL。
func startMockServer() (string, func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/tokens", handleTokenize)
	mux.HandleFunc("/v1/payment_intents", handlePaymentIntents)
	mux.HandleFunc("/v1/refunds", handleRefunds)
	// 通配：capture 和 delete 路由
	mux.HandleFunc("/", handleFallback)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("[Stripe MockServer] Failed to listen: %v", err)
	}
	addr := fmt.Sprintf("http://%s", listener.Addr().String())
	log.Printf("[Stripe MockServer] Started on %s", addr)

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	return addr, func() { server.Close() }
}

// ── 模拟端点 ──────────────────────────────────────────────────────

func handleTokenize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params := parseBody(r)
	token := params["token"]

	if strings.HasPrefix(token, "tok_fail") {
		writeError(w, http.StatusPaymentRequired, "card rejected")
		return
	}

	brand := "Visa"
	last4 := "4242"
	if strings.HasPrefix(token, "tok_mc") {
		brand = "Mastercard"
		last4 = "5353"
	} else if strings.HasPrefix(token, "tok_up") {
		brand = "UnionPay"
		last4 = "6200"
	}

	writeJSON(w, map[string]any{
		"id":         fmt.Sprintf("tok_%s", randHex(8)),
		"card_brand": brand,
		"card_last4": last4,
		"exp_month":  12,
		"exp_year":   2028,
	})
}

func handlePaymentIntents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params := parseBody(r)
	tokenID := params["token"]

	if strings.HasPrefix(tokenID, "tok_decline") {
		writeError(w, http.StatusPaymentRequired, "card declined: insufficient funds")
		return
	}

	writeJSON(w, map[string]any{
		"id":              fmt.Sprintf("pi_%s", randHex(8)),
		"auth_code":       fmt.Sprintf("AUTH_%s", randHex(3)),
		"recurring_token": fmt.Sprintf("pm_mock_%s", randHex(8)),
	})
}

func handleRefunds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"status": "refunded"})
}

func handleFallback(w http.ResponseWriter, r *http.Request) {
	// /v1/payment_intents/{id}/capture
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/capture") {
		writeJSON(w, map[string]any{"status": "captured"})
		return
	}
	// /v1/tokens/{id} DELETE
	if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/tokens/") {
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
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
