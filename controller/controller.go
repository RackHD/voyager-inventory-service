package controller

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/RackHD/voyager-inventory-service/mysql"
	"github.com/RackHD/voyager-utilities/amqp"
	"github.com/RackHD/voyager-utilities/models"

	samqp "github.com/streadway/amqp"
)

// Inventory is a representation of Voyager's nodes and switches
type Inventory struct {
	MQ    *amqp.Client
	MySQL *mysql.DBconn
}

// NewInventory creates and initializes a new inventory
func NewInventory(amqpAddress, dbAddress string) *Inventory {
	inventory := Inventory{}

	inventory.MQ = amqp.NewClient(amqpAddress)
	if inventory.MQ == nil {
		log.Fatalf("Could not connect to RabbitMQ: %s\n", amqpAddress)
	}

	inventory.MySQL = &mysql.DBconn{}
	err := inventory.MySQL.Initialize(dbAddress)
	if err != nil {
		log.Fatalf("Error connecting to DB: %s\n", err)
	}

	return &inventory
}

// ProcessMessage processes a message
func (i *Inventory) ProcessMessage(m *samqp.Delivery) error {
	switch m.Exchange {

	case "voyager-inventory-service":
		return i.processInventoryService(m)

	case "on.events":
		return i.processOnEvents(m)

	default:
		err := fmt.Errorf("Unknown exchange name: %s\n", m.Exchange)
		log.Printf("Error: %s", err)
		return err

	}
}

// processInventoryService processes a message from the processInventoryService exchange
func (i *Inventory) processInventoryService(d *samqp.Delivery) error {
	var cmd models.CmdMessage
	err := json.Unmarshal(d.Body, &cmd)
	if err != nil {
		log.Printf("Error: %s\n", err)
		return err
	}

	switch cmd.Command {

	case "get_nodes":
		// Process any Args
		// No args right Now

		// Call to DB for node_entities
		allNodes := make([]models.NodeEntity, 25)
		i.MySQL.DB.Find(&allNodes)

		// Marshal into json
		var responseBody []byte
		responseBody, err = json.Marshal(allNodes)
		if err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}

		// amqp.send back to requestor
		return i.MQ.Send(d.Exchange, "topic", d.ReplyTo, string(responseBody), d.CorrelationId, d.RoutingKey)

	case "insert_ip_entry":
		var insertCmd models.CmdMessage
		insertCmd.Args = &models.IPEntity{}
		err = json.Unmarshal(d.Body, &insertCmd)
		if err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}

		ipEntity := insertCmd.Args.(*models.IPEntity)
		if err = i.MySQL.DB.Create(&ipEntity).Error; err != nil {
			log.Printf("Error: %s\n", err)

			failure := fmt.Sprintf("ERROR: %s", err.Error())
			i.MQ.Send(d.Exchange, "topic", d.ReplyTo, failure, d.CorrelationId, d.RoutingKey)

			return err
		}

		log.Printf("Node %s recorded with IP %s\n", ipEntity.NodeID, ipEntity.IPAddress)

		// amqp.send back to requestor
		return i.MQ.Send(d.Exchange, "topic", d.ReplyTo, "SUCCESS", d.CorrelationId, d.RoutingKey)

	case "update_node":
		nJSON, errMarshal := json.Marshal(cmd.Args)
		if errMarshal != nil {
			return errMarshal
		}
		var node models.NodeEntity
		err = json.Unmarshal(nJSON, &node)

		if err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}

		log.Printf("Updating node to: %+v\n", node)
		if err = i.MySQL.DB.Model(&node).Update("status", node.Status).Error; err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}

		// amqp.send back to requestor
		return i.MQ.Send(d.Exchange, "topic", d.ReplyTo, "SUCCESS", d.CorrelationId, d.RoutingKey)

	default:
		err = fmt.Errorf("Unknown command: %s\n", cmd.Command)
		log.Printf("Error: %s\n", err)
		return err

	}

}

// processOnEvents processes a message from the OnEvents exchange
func (i *Inventory) processOnEvents(d *samqp.Delivery) error {
	log.Printf("Start to process rackhd on.events message")
	var node models.NodeMessage
	err := json.Unmarshal(d.Body, &node)
	if err != nil {
		log.Printf("Error: %s\n", err)
		return err
	}

	log.Printf("Node Message:\n	%+v\n", node)

	switch node.Action {

	case "added":
		n := models.NodeEntity{
			ID:     node.NodeID,
			Type:   node.NodeType,
			Status: models.StatusAdded,
		}
		log.Printf("Adding node to DB: %+v\n", n)

		if err = i.MySQL.DB.Create(&n).Error; err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}
		return nil

	case "discovered":
		n := models.NodeEntity{
			ID:     node.NodeID,
			Type:   node.NodeType,
			Status: models.StatusDiscovered,
		}
		var nJSON []byte
		nJSON, err = json.Marshal(models.NodeEntityJSON{
			ID:     node.NodeID,
			Type:   node.NodeType,
			Status: models.StatusDiscovered,
		})
		if err != nil {
			return err
		}

		log.Printf("Updating node to Discovered: %+v\n", n)
		if err = i.MySQL.DB.Model(&n).Update("status", "Discovered").Error; err != nil {
			log.Printf("Error: %s\n", err)
			return err
		}

		err = i.MQ.Send("voyager-inventory-service", "topic", "node.discovered.#", string(nJSON), "", "")
		if err != nil {
			log.Fatalf("Error sending to RabbitMQ: %s\n", err)
		}

		return nil

	default:
		err = fmt.Errorf("Unknown node action: %s\n", node.Action)
		log.Printf("Error: %s\n", err)
		return err
	}

}
