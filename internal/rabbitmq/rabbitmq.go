package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ellezio/Chat-app-with-Go/internal/log"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Queue struct {
	Name                                   string
	Durable, AutoDelete, Exclusive, NoWait bool
	Args                                   amqp.Table
}

type Exchange struct {
	Name, Kind                            string
	Durable, AutoDelete, Internal, NoWait bool
	Args                                  amqp.Table
}

type Consumer struct {
	Tag                                 string
	AutoAck, Exclusive, NoLocal, NoWait bool
	Args                                amqp.Table

	queue      *Queue
	exchange   *Exchange
	routingKey string

	Consume func(msg amqp.Delivery)
}

func (c *Consumer) setup(conn *amqp.Connection) error {
	log.DefaultContextLogger.Info("Creating consumer channel")
	if conn == nil {
		return fmt.Errorf("consumer: failed to open a channel: connection is not configured")
	}

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("consumer: failed to open a channel: %w", err)
	}

	if c.queue == nil {
		return fmt.Errorf("consumer: queue not specified")
	}

	q, err := ch.QueueDeclare(c.queue.Name, c.queue.Durable, c.queue.AutoDelete, c.queue.Exclusive, c.queue.NoWait, c.queue.Args)
	if err != nil {
		return fmt.Errorf("consumer: failed to declare the queue: %w", err)
	}

	// if nil the exchange is the default one
	if c.exchange != nil {
		err = ch.ExchangeDeclare(c.exchange.Name, c.exchange.Kind, c.exchange.Durable, c.exchange.AutoDelete, c.exchange.Internal, c.exchange.NoWait, c.exchange.Args)
		if err != nil {
			return fmt.Errorf("consumer: failed to declare the exchange: %w", err)
		}

		err = ch.QueueBind(q.Name, c.routingKey, c.exchange.Name, false, nil)
		if err != nil {
			return fmt.Errorf("consumer: failed to bind the queue: %w", err)
		}
	}

	queueChan, err := ch.Consume(q.Name, c.Tag, c.AutoAck, c.Exclusive, c.NoLocal, c.NoWait, c.Args)
	if err != nil {
		ch.Close()
		return fmt.Errorf("consumer: failed to register: %w", err)
	}

	go func() {
		for d := range queueChan {
			c.Consume(d)
		}
		log.DefaultContextLogger.Info("Closing consumer channel")
	}()

	return nil
}

type Publisher struct {
	ch        *amqp.Channel
	exchanges []Exchange
}

func (p *Publisher) Publish(exchangeName string, routingKey string, msg amqp.Publishing) error {
	log.DefaultContextLogger.Info("sending message")
	err := p.ch.Publish(exchangeName, routingKey, false, false, msg)
	log.DefaultContextLogger.Info("message sent", slog.Any("error", err))
	return err
}

func (p *Publisher) setup(conn *amqp.Connection) error {
	if conn == nil {
		return fmt.Errorf("publisher: failed to open a channel: connection is not configured")
	}

	var err error
	p.ch, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("publisher: failed to open a channel: %w", err)
	}

	for _, exchange := range p.exchanges {
		err = p.ch.ExchangeDeclare(exchange.Name, exchange.Kind, exchange.Durable, exchange.AutoDelete, exchange.Internal, exchange.NoWait, exchange.Args)
		if err != nil {
			return fmt.Errorf("publisher: failed to declare the exchange: %w", err)
		}
	}

	return nil
}

type Client struct {
	connectionString string
	conn             *amqp.Connection
	publishers       []*Publisher
	consumers        []*Consumer
}

func Dial(ctx context.Context, connectionString string) (*Client, error) {
	logger := log.Ctx(ctx)

	c := &Client{connectionString: connectionString}
	if err := c.dial(); err != nil {
		return nil, err
	}

	go func() {
		for {
			reason, ok := <-c.conn.NotifyClose(make(chan *amqp.Error))
			if !ok {
				logger.Info("rabbitmq: connection closed")
				break
			}
			logger.Error("rabbitmq: lost connection", slog.String("reason", reason.Error()))

			for {
				time.Sleep(time.Second)
				err := c.dial()

				if err == nil {
					logger.Info("rabbitmq: reconnected")
					break
				}

				logger.Error("rabbitmq: reconnection failed", slog.Any("error", err))
			}

			logger.Info("rabbitmq: resetup publisher")
			for _, pub := range c.publishers {
				// there is no need for closing a channel because it will be automatically closed on connection close
				if err := pub.setup(c.conn); err != nil {
					logger.Error("rabbitmq: failed to setup publisher", slog.Any("error", err))
					break
				}
			}

			logger.Info("rabbitmq: resetup consumer")
			for _, con := range c.consumers {
				if err := con.setup(c.conn); err != nil {
					logger.Error("rabbitmq: failed to setup consumer", slog.Any("error", err))
					break
				}
			}

			logger.Info("rabbitmq: fully reconnected")
		}
	}()

	return c, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) NewPublisher(ctx context.Context, exchanges []Exchange) (*Publisher, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("rabbitmq client: failed to open a channel for publisher: connection is not configured")
	}

	publisher := Publisher{exchanges: exchanges}
	if err := publisher.setup(c.conn); err != nil {
		return nil, err
	}
	c.publishers = append(c.publishers, &publisher)

	return &publisher, nil
}

func (c *Client) RegisterConsumer(ctx context.Context, queue *Queue, routingKey string, exchange *Exchange, consumer Consumer) error {
	if c.conn == nil {
		return fmt.Errorf("rabbitmq client: failed to open a channel for consumer: connection is not configured")
	}

	consumer.queue = queue
	consumer.exchange = exchange
	consumer.routingKey = routingKey

	if err := consumer.setup(c.conn); err != nil {
		return err
	}

	c.consumers = append(c.consumers, &consumer)

	return nil
}

func (c *Client) dial() error {
	var err error
	c.conn, err = amqp.Dial(c.connectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	return nil
}
