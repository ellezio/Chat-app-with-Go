package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/ellezio/Chat-app-with-Go/internal"
	"github.com/ellezio/Chat-app-with-Go/internal/config"
	"github.com/ellezio/Chat-app-with-Go/internal/store"
	amqp "github.com/rabbitmq/amqp091-go"
)

type chat struct {
	id    string
	users map[string]bool
	mu    sync.Mutex
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
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

func (h *handler) handle(d *amqp.Delivery, ch *amqp.Channel) error {
	log.Printf("Received a message: %s", d.Body)

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
		err = h.broadcast(d, ch, event)
	}

	d.Ack(false)
	return nil
}

func (h *handler) newMessage(evt internal.ChatEvent, details internal.MessageEventDetails) (any, error) {
	msg := internal.New(evt.ChatId, evt.UserId, details.Content, details.Type)
	msg.Status = internal.Sent

	err := h.store.SaveMessage(msg)
	if err != nil {
		log.Println(err)
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

func (h *handler) broadcast(d *amqp.Delivery, ch *amqp.Channel, event internal.ChatEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		d.Ack(false)
		return fmt.Errorf("Failed to broadcast message: %v", err)
	}

	err = ch.Publish(
		"chat_notifications", // exchange
		"",                   // routing key
		false,                // mandatory
		false,                // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        body,
		})

	return nil
}

func main() {
	b, err := os.ReadFile("config.json")
	if err != nil {
		panic(err)
	}

	var cfg config.Configuration
	err = json.Unmarshal(b, &cfg)
	if err != nil {
		panic(err)
	}

	err = store.InitConn(cfg.MongoDB, cfg.Redis)
	if err != nil {
		panic(err)
	}

	conn, err := amqp.Dial(cfg.RabbitMQ.ConnectionString)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	err = ch.ExchangeDeclare(
		"chat_notifications", // name
		"fanout",             // type
		true,                 // durable
		false,                // auto-deleted
		false,                // internal
		false,                // no-wait
		nil,                  // arguments
	)
	failOnError(err, "Failed to declare an exchange")

	// TODO: read arguments from global config
	q, err := ch.QueueDeclare(
		"chat_messages", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	failOnError(err, "Failed to declare a queue")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	forever := make(chan struct{})

	dbstore := store.MongodbStore{}
	h := handler{store: &dbstore, chats: make(map[string]*chat), mu: sync.Mutex{}}

	go func() {
		for d := range msgs {
			if err = h.handle(&d, ch); err != nil {
				log.Printf("failed to handle message: %v", err)
			}
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}
