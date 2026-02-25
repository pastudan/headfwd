package config

import "os"

type Config struct {
	Port         string
	HeadscaleURL string
	APIKey       string
	DevMode      bool
}

func Load() *Config {
	return &Config{
		Port:         envOr("PORT", "3001"),
		HeadscaleURL: envOr("HEADSCALE_URL", "http://localhost:8080"),
		APIKey:       os.Getenv("HEADSCALE_API_KEY"),
		DevMode:      os.Getenv("DEV") == "1",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
