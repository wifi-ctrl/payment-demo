package config

import "os"

// Config 应用配置
type Config struct {
	Port                   string
	DatabaseURL            string
	GatewayKey             string
	StripeAPIKey           string
	Env                    string
	RecurringWebhookSecret string // 非空时校验 POST /webhooks/recurring-token 的 X-Webhook-Secret（Demo）；生产请改用 PSP 签名
}

func Load() *Config {
	return &Config{
		Port:                   getEnv("PORT", "8080"),
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		GatewayKey:             getEnv("GATEWAY_SECRET_KEY", ""),
		StripeAPIKey:           getEnv("STRIPE_API_KEY", "sk_test_demo"),
		Env:                    getEnv("APP_ENV", "dev"),
		RecurringWebhookSecret: getEnv("RECURRING_WEBHOOK_SECRET", ""),
	}
}

func (c *Config) IsDev() bool {
	return c.Env == "dev"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
