package config

import (
	"log"
	"os"
)

type Config struct {
	Env         string
	HTTPPort    string
	DatabaseURL string
	CORSOrigin  string
	MQTTBroker  string
}

func Getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func Load() Config {
	cfg := Config{
		Env:         Getenv("APP_ENV", "development"),
		HTTPPort:    Getenv("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		CORSOrigin:  Getenv("CORS_ORIGIN", "http://localhost:3000"),
		MQTTBroker:  Getenv("MQTT_BROKER", "tcp://mqtt:1883"),
	}
	if cfg.DatabaseURL == "" {
		log.Println("WARN: DATABASE_URL is empty (DB機能を使うなら設定してください)")
	}
	return cfg
}
