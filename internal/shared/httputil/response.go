// Package httputil 提供 HTTP handler 层通用的响应工具函数。
package httputil

import (
	"encoding/json"
	"log"
	"net/http"
)

// OK 写入 200 JSON 响应。
func OK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[httputil] OK encode error: %v", err)
	}
}

// Created 写入 201 JSON 响应。
func Created(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[httputil] Created encode error: %v", err)
	}
}

// Error 写入带状态码的 JSON 错误响应。
func Error(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("[httputil] Error encode error: %v", err)
	}
}

// UseCaseError 将业务错误映射为 HTTP 响应。
// mapStatus 由各 handler 提供，将自己上下文的领域错误映射为 HTTP 状态码。
func UseCaseError(w http.ResponseWriter, err error, mapStatus func(error) int) {
	status := mapStatus(err)
	msg := err.Error()
	if status >= http.StatusInternalServerError {
		msg = "internal server error"
	}
	Error(w, msg, status)
}
