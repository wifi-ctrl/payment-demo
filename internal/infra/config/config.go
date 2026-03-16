package config

import "os"

// Config 应用配置
type Config struct {
	Port        string
	DatabaseURL string
	GatewayKey  string
	Env         string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		GatewayKey:  getEnv("GATEWAY_SECRET_KEY", ""),
		Env:         getEnv("APP_ENV", "dev"),
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
