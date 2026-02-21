package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/config"
	"github.com/ellezio/Chat-app-with-Go/internal/log"
	"github.com/ellezio/Chat-app-with-Go/internal/rabbitmq"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
	amqp "github.com/rabbitmq/amqp091-go"
)

type chat struct {
	id    string
	users map[string]bool
	mu    sync.Mutex
}

// NOTE:
// chats stored only in handler doesn't scale
// this will be move to probably Redis
type handler struct {
	store internal.Store
	chats map[string]*chat
	mu    sync.Mutex
}

func assertAndCall[T any](eventName string, fn func(evt internal.ChatEvent, arg T) (any, error), evt internal.ChatEvent, arg any) (any, error) {
	a, ok := arg.(T)
	if !ok {
		return nil, fmt.Errorf("invalid type %T for event %s", arg, eventName)
	}

	return fn(evt, a)
}

func (h *handler) handle(d *amqp.Delivery, pub *rabbitmq.Publisher) error {
	var event internal.ChatEvent
	err := event.UnmarshalJSON(d.Body)
	if err != nil {
		// TODO: find a way to handle unprocessable messages - now lets just omit them
		d.Ack(false)
		return fmt.Errorf("Cannot process message: %v", err)
	}

	var broadcastDetails any

	switch event.Type {
	case internal.Event_NewMessage:
		broadcastDetails, err = assertAndCall("NewMessage", h.newMessage, event, event.Details)
	case internal.Event_UpdateMessage:
		broadcastDetails, err = assertAndCall("UpdateMessage", h.updateMessage, event, event.Details)
	case internal.Event_EditMessage:
		broadcastDetails, err = assertAndCall("EditMessage", h.editMessage, event, event.Details)
	case internal.Event_HideMessage:
		broadcastDetails, err = assertAndCall("HideMessage", h.hideMessage, event, event.Details)
	case internal.Event_DeleteMessage:
		broadcastDetails, err = assertAndCall("DeleteMessage", h.deleteMessage, event, event.Details)
	// case internal.Event_PinMessage:
	// broadcastDetails, err = assertAndCall("PinMessage", h.pinMessage, event, event.Details)
	case internal.Event_NewChat:
		broadcastDetails, err = assertAndCall("NewChat", h.newChat, event, event.Details)
	default:
		err = fmt.Errorf("Unknown event type %v", event.Type)
	}

	if err != nil {
		d.Ack(false)
		return err
	}

	if broadcastDetails != nil {
		event.Details = broadcastDetails
		err = h.broadcast(d, pub, event)
	}

	d.Ack(false)
	return nil
}

func (h *handler) newMessage(evt internal.ChatEvent, details internal.MessageEventDetails) (any, error) {
	msg := internal.New(evt.ChatId, evt.UserId, details.Content, details.Type)
	msg.Status = internal.Sent

	err := h.store.SaveMessage(msg)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func (h *handler) updateMessage(evt internal.ChatEvent, details internal.Message) (any, error) {
	h.store.SaveMessage(&details)
	return details, nil
}

func (h *handler) editMessage(evt internal.ChatEvent, details internal.MessageEventDetails) (any, error) {
	return h.store.UpdateMessageContent(details.Id, details.Content)
}

func (h *handler) hideMessage(evt internal.ChatEvent, details internal.MessageEventDetails) (any, error) {
	return h.store.SetHideMessage(details.Id, evt.UserId, details.Hidden)
}

func (h *handler) deleteMessage(evt internal.ChatEvent, details internal.MessageEventDetails) (any, error) {
	return h.store.DeleteMessage(details.Id)
}

// func (h *handler) pinMessage(d *amqp.Delivery, ch *amqp.Channel, event internal.ChatEvent) {}

func (h *handler) newChat(evt internal.ChatEvent, details *internal.Chat) (any, error) {
	if err := h.store.SaveChat(details); err != nil {
		return nil, fmt.Errorf("error when creating chats: %v", err)
	}

	return details, nil
}

func (h *handler) broadcast(d *amqp.Delivery, pub *rabbitmq.Publisher, event internal.ChatEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		d.Ack(false)
		return fmt.Errorf("Failed to broadcast message: %v", err)
	}

	err = pub.Publish(
		"chat_notifications", // exchange
		"",                   // routing key
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        body,
		})

	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With("service", "chat-server")
	log.DefaultContextLogger = logger

	b, err := os.ReadFile("config.json")
	if err != nil {
		logger.Error("failed to read config file", slog.Any("error", err))
		return
	}

	var cfg config.Configuration
	err = json.Unmarshal(b, &cfg)
	if err != nil {
		logger.Error("failed to laod config", slog.Any("error", err))
		return
	}

	err = store.InitConn(cfg.MongoDB, cfg.Redis)
	if err != nil {
		logger.Error("failed to establish store connection", slog.Any("error", err))
		return
	}

	client, err := rabbitmq.Dial(context.Background(), cfg.RabbitMQ.ConnectionString)
	if err != nil {
		logger.Error("failed to connect to RabbitMQ", slog.Any("error", err))
		return
	}
	defer client.Close()

	publisher, err := client.NewPublisher(
		context.Background(),
		[]rabbitmq.Exchange{{
			Name:    "chat_notifications",
			Kind:    "fanout",
			Durable: true,
		}},
	)
	if err != nil {
		logger.Error("creating publisher: %w", err)
		return
	}

	dbstore := store.MongodbStore{}
	h := handler{store: &dbstore, chats: make(map[string]*chat), mu: sync.Mutex{}}
	consume := func(d amqp.Delivery) {
		msgLogger := logger.With("correlation_id", d.CorrelationId)
		msgLogger.Debug("Received a message", slog.String("body", string(d.Body)))

		if err = h.handle(&d, publisher); err != nil {
			msgLogger.Error("failed to handle message", slog.Any("error", err))
		}
	}

	client.RegisterConsumer(
		context.Background(),
		&rabbitmq.Queue{Name: "chat_messages", Durable: true},
		"",
		nil,
		rabbitmq.Consumer{Consume: consume},
	)

	forever := make(chan struct{})
	logger.Debug("[*] Waiting for messages. To exit press CTRL+C")
	<-forever
}
