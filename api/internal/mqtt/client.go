package mqtt

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"example.com/app/api/internal/ingest"
	"example.com/app/api/internal/ws"
)

type Client struct {
	raw paho.Client
	db  *sql.DB
	hub *ws.Hub
}

func NewClient(broker string, db *sql.DB, hub *ws.Hub) (*Client, error) {
	c := &Client{db: db, hub: hub}

	opts := paho.NewClientOptions().
		AddBroker(broker).
		SetClientID(fmt.Sprintf("api-server-%d", time.Now().UnixNano())).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(raw paho.Client) {
			log.Printf("mqtt: connected to %s", broker)
			c.subscribe(raw, "iot/+/telemetry")
			c.subscribe(raw, "iot/+/heartbeat")
			c.subscribe(raw, "iot/+/cmd_ack")
		}).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			log.Printf("mqtt: connection lost: %v", err)
		})

	c.raw = paho.NewClient(opts)
	return c, nil
}

func (c *Client) ConnectAndSubscribe() {
	for {
		token := c.raw.Connect()
		token.Wait()
		if err := token.Error(); err != nil {
			log.Printf("mqtt: connect failed (%v), retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		return
	}
}

func (c *Client) PublishCommand(deviceKey string, payload any) error {
	if c == nil || c.raw == nil || !c.raw.IsConnected() {
		return fmt.Errorf("mqtt client is not connected")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	token := c.raw.Publish(fmt.Sprintf("iot/%s/cmd", deviceKey), 1, false, body)
	token.Wait()
	return token.Error()
}

func (c *Client) subscribe(raw paho.Client, topic string) {
	token := raw.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		ingest.Handle(c.db, msg.Topic(), msg.Payload(), c.hub)
	})
	token.Wait()
	if err := token.Error(); err != nil {
		log.Printf("mqtt: subscribe error topic=%s: %v", topic, err)
		return
	}
	log.Printf("mqtt: subscribed to %s", topic)
}
