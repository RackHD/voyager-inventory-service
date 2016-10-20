package main

import (
	"flag"
	"log"

	"github.com/RackHD/voyager-inventory-service/controller"
	"github.com/RackHD/voyager-utilities/random"
	"github.com/streadway/amqp"
)

var (
	uri          = flag.String("uri", "amqp://guest:guest@rabbitmq:5672/", "AMQP URI")
	exchange     = flag.String("exchange", "voyager-inventory-service", "Durable, non-auto-deleted AMQP exchange name")
	exchangeType = flag.String("exchange-type", "topic", "Exchange type - direct|fanout|topic|x-custom")
	queue        = flag.String("queue", "voyager-inventory-service-queue", "Ephemeral AMQP queue name")
	bindingKey   = flag.String("key", "requests", "AMQP binding key")
	consumerTag  = flag.String("consumer-tag", "simple-consumer", "AMQP consumer tag (should not be blank)")
	dbAddress    = flag.String("db-address", "root@(mysql:3306)/mysql", "Address of the MySQL database")
)

const (
	onEvents           = "on.events"
	onEventsBindingKey = "event.#" // no *s*
)

func init() {
	flag.Parse()
}

func main() {
	inventory := controller.NewInventory(*uri, *dbAddress)
	defer inventory.MQ.Close()

	inventoryQueue := random.RandQueue()
	_, err := StartListening(*exchange, *exchangeType, inventoryQueue, *bindingKey, *consumerTag, inventory)
	if err != nil {
		log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
	}

	onEventsQueue := random.RandQueue()
	_, err = StartListening(onEvents, *exchangeType, onEventsQueue, onEventsBindingKey, *consumerTag, inventory)
	if err != nil {
		log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
	}

	select {}
}

// StartListening begins listening for AMQP messages in the background and kicks of a processor for each
func StartListening(exchangeName, exchangeType, queueName, bindingKey, consumerTag string, inventory *controller.Inventory) (<-chan amqp.Delivery, error) {
	_, messageChannel, err := inventory.MQ.Listen(exchangeName, exchangeType, queueName, bindingKey, consumerTag)
	// Listen for messages in the background in infinite loop
	go func() {
		// Listen for messages in the background in infinite loop
		for m := range messageChannel {
			log.Printf(
				"got %dB delivery: [%v] %s",
				len(m.Body),
				m.DeliveryTag,
				m.Body,
			)
			m.Ack(true)

			go inventory.ProcessMessage(&m)
		}
	}()

	return messageChannel, err
}
