package main

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"example.com/app/api/internal/config"
	"example.com/app/api/internal/db"
	httpapi "example.com/app/api/internal/http"
	"example.com/app/api/internal/monitor"
	mqttclient "example.com/app/api/internal/mqtt"
	"example.com/app/api/internal/ws"
)

func main() {
	cfg := config.Load()
	hub := ws.NewHub(cfg.CORSOrigin)

	var conn *sql.DB
	var mqtt *mqttclient.Client
	if cfg.DatabaseURL != "" {
		var err error
		conn, err = db.Open(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("db open error: %v", err)
		}
		// 起動時に疎通確認
		conn.SetConnMaxIdleTime(5 * time.Minute)
		if err := conn.Ping(); err != nil {
			log.Fatalf("db ping error: %v", err)
		}
		log.Println("db connected")

		mqtt, err = mqttclient.NewClient(cfg.MQTTBroker, conn, hub)
		if err != nil {
			log.Fatalf("mqtt client init error: %v", err)
		}

		go monitor.Start(conn, hub)
		go monitor.StartCommandTimeout(conn, hub)
		go mqtt.ConnectAndSubscribe()
	} else {
		log.Println("db disabled (DATABASE_URL is empty)")
	}

	srv := httpapi.New(conn, cfg.CORSOrigin, hub, mqtt)
	addr := ":" + cfg.HTTPPort
	log.Printf("listening on %s (env=%s)", addr, cfg.Env)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
