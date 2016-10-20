package controller_test

import (
	"encoding/json"
	"os"

	"github.com/RackHD/voyager-inventory-service/controller"
	"github.com/RackHD/voyager-utilities/models"
	"github.com/RackHD/voyager-utilities/random"
	"github.com/streadway/amqp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Handler", func() {

	Describe("AMQP Message Handling", func() {
		var rabbitMQURL string
		var inventory *controller.Inventory
		var testExchange string
		var testExchangeType string
		var testQueueName string
		var testRoutingKey string
		var testConsumerTag string
		var testMessage string
		var testCorrelationID string
		var dbUrl string
		var err error

		BeforeEach(func() {
			rabbitMQURL = os.Getenv("RABBITMQ_URL")
			dbUrl = "root@(localhost:3306)/mysql"
			testExchange = "voyager-inventory-service"
			testExchangeType = "topic"
			testQueueName = random.RandQueue()
			testRoutingKey = "#"
			testConsumerTag = "testTag"
			testCorrelationID = "testID"

			inventory = controller.NewInventory(rabbitMQURL, dbUrl)
			Expect(inventory).ToNot(Equal(nil))

		})
		AfterEach(func() {
			inventory.MQ.Close()
		})

		Context("When a message comes in to on.events", func() {

			var deliveries <-chan amqp.Delivery
			var err error

			BeforeEach(func() {
				testExchange = "on.events"
				_, deliveries, err = inventory.MQ.Listen(testExchange, testExchangeType, testQueueName, testRoutingKey, testConsumerTag)
				Expect(err).ToNot(HaveOccurred())

			})
			AfterEach(func() {
				inventory.MySQL.DB.DropTableIfExists("node_entities")
				inventory.MySQL.DB.Close()

			})

			Context("When waiting on event.# binding key", func() {
				It("INTEGRATION receives event.# message from RackHD", func() {
					queueName := random.RandQueue()
					//We listen to match any binding key that have the word event.
					_, nodeMessages, err := inventory.MQ.Listen("on.events", testExchangeType, queueName, "event.#", testConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					// Pretend to be rackhd sending event.whatever message
					testMessage = `{"type":"node","action":"added","nodeId":"some_node_id","nodeType":"compute"}`
					err = inventory.MQ.Send("on.events", testExchangeType, "event.whatever", testMessage, "", "")
					Expect(err).ToNot(HaveOccurred())

					d := <-nodeMessages
					d.Ack(false)
					Expect(string(d.Body)).To(Equal(testMessage))
				})
			})

			It("INTEGRATION should add a new node to the DB if action is 'added'", func() {

				testMessage = `{"type":"node","action":"added","nodeId":"UNIQUE_ID","nodeType":"compute"}`
				err = inventory.MQ.Send(testExchange, testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
				Expect(err).ToNot(HaveOccurred())

				d := <-deliveries
				d.Ack(false)

				err = inventory.ProcessMessage(&d)
				Expect(err).ToNot(HaveOccurred())
				// Validate data is in DB
				out := models.NodeEntity{}
				inventory.MySQL.DB.Where("ID = ?", "UNIQUE_ID").Find(&out)
				Expect(out.ID).To(Equal("UNIQUE_ID"))
				Expect(out.Status).To(Equal("Added"))
			})
			It("INTEGRATION should update a node's status if action is 'discovered'", func() {

				testMessage = `{"type":"node","action":"added","nodeId":"UNIQUE_ID","nodeType":"compute"}`
				err = inventory.MQ.Send(testExchange, testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
				Expect(err).ToNot(HaveOccurred())
				d := <-deliveries
				d.Ack(false)
				err = inventory.ProcessMessage(&d)
				Expect(err).ToNot(HaveOccurred())

				testMessage = `{"type":"node","action":"discovered","nodeId":"UNIQUE_ID","nodeType":"compute"}`
				err = inventory.MQ.Send(testExchange, testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
				Expect(err).ToNot(HaveOccurred())

				d = <-deliveries
				d.Ack(false)

				err = inventory.ProcessMessage(&d)
				Expect(err).ToNot(HaveOccurred())

				// Validate data is in DB
				out := models.NodeEntity{}
				err := inventory.MySQL.DB.Where("ID = ?", "UNIQUE_ID").Find(&out).Error

				Expect(err).ToNot(HaveOccurred())
				Expect(out.ID).To(Equal("UNIQUE_ID"))
				Expect(out.Status).To(Equal("Discovered"))

			})
			It("INTEGRATION should do nothing if action is unknown", func() {
				testMessage = `{"type":"node","action":"BAD_ACTION","nodeId":"UNIQUE_ID","nodeType":"compute"}`
				err = inventory.MQ.Send(testExchange, testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
				Expect(err).ToNot(HaveOccurred())

				d := <-deliveries
				d.Ack(false)

				err = inventory.ProcessMessage(&d)
				Expect(err).To(HaveOccurred())

				// Validate data is NOT in DB
				out := models.NodeEntity{}
				err = inventory.MySQL.DB.Where("ID = ?", "UNIQUE_ID").Find(&out).Error
				Expect(err).To(HaveOccurred())
				Expect(out).To(Equal(models.NodeEntity{}))
			})

		})

		Context("When a message comes in to inventoryService exchange", func() {
			var inventoryServiceExchange, inventoryServiceExchangeType, inventoryServiceRoutingKey, inventoryServiceRecieveQueue, inventoryServiceConsumerTag, inventoryServiceCorrelationID string
			var requests <-chan amqp.Delivery
			var replies <-chan amqp.Delivery
			var nodeMessages <-chan amqp.Delivery
			var requestQueueName, repliesQueueName string
			BeforeEach(func() {

				inventoryServiceExchange = "voyager-inventory-service"
				inventoryServiceExchangeType = "topic"
				inventoryServiceRoutingKey = "request"
				inventoryServiceRecieveQueue = "reply"
				inventoryServiceCorrelationID = "test-id1"
				inventoryServiceConsumerTag = "consumer-tag1"

				requestQueueName = random.RandQueue()
				repliesQueueName = random.RandQueue()

			})
			AfterEach(func() {
				inventory.MySQL.DB.DropTableIfExists("node_entities")
				inventory.MySQL.DB.DropTableIfExists("ip_entities")
				inventory.MySQL.DB.Close()

			})

			Context("When we receive request with the command 'get_nodes'", func() {
				It("INTEGRATION should reply with a list of nodes in the DB", func() {
					// Setup for this Test
					_, requests, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, requestQueueName, inventoryServiceRoutingKey, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, replies, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, repliesQueueName, inventoryServiceRecieveQueue, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, nodeMessages, err = inventory.MQ.Listen("on.events", testExchangeType, testQueueName, testRoutingKey, testConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					testMessage = `{"type":"node","action":"added","nodeId":"UNIQUE_ID","nodeType":"compute"}`
					err = inventory.MQ.Send("on.events", testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
					Expect(err).ToNot(HaveOccurred())
					testMessage = `{"type":"node","action":"added","nodeId":"UNIQUE_ID1","nodeType":"compute"}`
					err = inventory.MQ.Send("on.events", testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
					Expect(err).ToNot(HaveOccurred())

					for i := 0; i < 2; i++ {
						nodeMsg := <-nodeMessages
						nodeMsg.Ack(false)
						err = inventory.ProcessMessage(&nodeMsg)
						Expect(err).ToNot(HaveOccurred())
					}

					// The test itself
					requestMessage := `{"command": "get_nodes", "options":""}`
					expectedReply := `[{"ID":"UNIQUE_ID","Type":"compute","Status":"Added"},{"ID":"UNIQUE_ID1","Type":"compute","Status":"Added"}]`

					err := inventory.MQ.Send(inventoryServiceExchange, inventoryServiceExchangeType, inventoryServiceRoutingKey, requestMessage, inventoryServiceCorrelationID, inventoryServiceRecieveQueue)
					Expect(err).ToNot(HaveOccurred())

					req := <-requests
					req.Ack(false)

					err = inventory.ProcessMessage(&req)
					Expect(err).ToNot(HaveOccurred())
					Expect(req.CorrelationId).To(Equal(inventoryServiceCorrelationID))

					reply := <-replies
					req.Ack(false)
					Expect(string(reply.Body)).To(Equal(expectedReply))
				})
			})

			Context("When we receive request with the command 'insert_ip_entry'", func() {
				It("INTEGRATION should insert an IP object into the DB", func() {
					// Set up test
					_, requests, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, requestQueueName, inventoryServiceRoutingKey, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, replies, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, repliesQueueName, inventoryServiceRecieveQueue, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					testIPEntity := models.IPEntity{
						IPID:      "fake-ip-id",
						IPAddress: "10.10.10.10",
						NodeID:    "fake-node-id",
						PoolID:    "fake-pool-id",
						SubnetID:  "fake-subnet-id",
					}
					testCmd := models.CmdMessage{
						Command: "insert_ip_entry",
						Args:    &testIPEntity,
					}
					testCmdMessage, err := json.Marshal(testCmd)
					Expect(err).ToNot(HaveOccurred())

					// Send request message
					err = inventory.MQ.Send(inventoryServiceExchange, inventoryServiceExchangeType, inventoryServiceRoutingKey, string(testCmdMessage), inventoryServiceCorrelationID, inventoryServiceRecieveQueue)
					Expect(err).ToNot(HaveOccurred())

					// Recieve and handle request
					req := <-requests
					req.Ack(false)
					err = inventory.ProcessMessage(&req)
					Expect(err).ToNot(HaveOccurred())

					// Handle voyager-inventory-service's reply to requestor
					reply := <-replies
					reply.Ack(false)
					Expect(string(reply.Body)).To(Equal("SUCCESS"))

					allIPs := make([]models.IPEntity, 1)
					inventory.MySQL.DB.Find(&allIPs)
					Expect(allIPs[0].IPID).To(Equal("fake-ip-id"))
					Expect(allIPs[0].IPAddress).To(Equal("10.10.10.10"))
					Expect(allIPs[0].NodeID).To(Equal("fake-node-id"))
					Expect(allIPs[0].PoolID).To(Equal("fake-pool-id"))
					Expect(allIPs[0].SubnetID).To(Equal("fake-subnet-id"))

				})

				It("INTEGRATION should fail gracefully on DB error", func() {
					// Set up test
					inventory.MySQL.DB.DropTableIfExists("ip_entities")
					_, requests, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, requestQueueName, inventoryServiceRoutingKey, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, replies, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, repliesQueueName, inventoryServiceRecieveQueue, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					testIPEntity := models.IPEntity{
						IPID:      "fake-ip-id",
						IPAddress: "10.10.10.10",
						NodeID:    "fake-node-id",
						PoolID:    "fake-pool-id",
						SubnetID:  "fake-subnet-id",
					}
					testCmd := models.CmdMessage{
						Command: "insert_ip_entry",
						Args:    &testIPEntity,
					}
					testCmdMessage, err := json.Marshal(testCmd)
					Expect(err).ToNot(HaveOccurred())

					// Send request message
					err = inventory.MQ.Send(inventoryServiceExchange, inventoryServiceExchangeType, inventoryServiceRoutingKey, string(testCmdMessage), inventoryServiceCorrelationID, inventoryServiceRecieveQueue)
					Expect(err).ToNot(HaveOccurred())

					// Recieve and handle request
					req := <-requests
					req.Ack(false)
					err = inventory.ProcessMessage(&req)
					Expect(err).To(HaveOccurred())

					// Handle voyager-inventory-service's reply to requestor
					reply := <-replies
					reply.Ack(false)
					Expect(string(reply.Body)).To(ContainSubstring("ERROR"))

				})
			})
			Context("When we receive request with the command 'update_node'", func() {
				It("INTEGRATION should Update the node with new info", func() {
					// Set up test
					// Setup for this Test
					_, requests, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, requestQueueName, inventoryServiceRoutingKey, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, replies, err = inventory.MQ.Listen(inventoryServiceExchange, inventoryServiceExchangeType, repliesQueueName, inventoryServiceRecieveQueue, inventoryServiceConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					_, nodeMessages, err = inventory.MQ.Listen("on.events", testExchangeType, testQueueName, testRoutingKey, testConsumerTag)
					Expect(err).ToNot(HaveOccurred())

					testMessage = `{"type":"node","action":"added","nodeId":"UNIQUE_ID","nodeType":"switch"}`
					err = inventory.MQ.Send("on.events", testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
					Expect(err).ToNot(HaveOccurred())
					nodeMsg := <-nodeMessages
					nodeMsg.Ack(false)
					err = inventory.ProcessMessage(&nodeMsg)
					Expect(err).ToNot(HaveOccurred())

					testMessage = `{"type":"node","action":"discovered","nodeId":"UNIQUE_ID","nodeType":"switch"}`
					err = inventory.MQ.Send("on.events", testExchangeType, testRoutingKey, testMessage, testCorrelationID, "")
					Expect(err).ToNot(HaveOccurred())
					nodeMsg = <-nodeMessages
					nodeMsg.Ack(false)
					err = inventory.ProcessMessage(&nodeMsg)
					Expect(err).ToNot(HaveOccurred())

					testNodeEntity := models.NodeEntity{
						ID:     "UNIQUE_ID",
						Type:   "switch",
						Status: "Available-Learning",
					}
					testCmd := models.CmdMessage{
						Command: "update_node",
						Args:    &testNodeEntity,
					}
					testCmdMessage, err := json.Marshal(testCmd)
					Expect(err).ToNot(HaveOccurred())

					// Send request message
					err = inventory.MQ.Send(inventoryServiceExchange, inventoryServiceExchangeType, inventoryServiceRoutingKey, string(testCmdMessage), inventoryServiceCorrelationID, inventoryServiceRecieveQueue)
					Expect(err).ToNot(HaveOccurred())

					// Recieve and handle request
					req := <-requests
					req.Ack(false)
					err = inventory.ProcessMessage(&req)
					Expect(err).ToNot(HaveOccurred())
					Expect(req.CorrelationId).To(Equal(inventoryServiceCorrelationID))

					// Handle voyager-inventory-service's reply to requestor
					reply := <-replies
					reply.Ack(false)
					Expect(string(reply.Body)).To(Equal("SUCCESS"))

					// Validate data is in DB
					out := models.NodeEntity{}
					err = inventory.MySQL.DB.Where("ID = ?", "UNIQUE_ID").Find(&out).Error

					Expect(err).ToNot(HaveOccurred())
					Expect(out.ID).To(Equal("UNIQUE_ID"))
					Expect(out.Status).To(Equal("Available-Learning"))

				})
			})
		})
	})
})
