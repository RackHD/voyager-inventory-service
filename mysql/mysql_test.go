package mysql_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/RackHD/voyager-inventory-service/mysql"
)

var _ = Describe("Mysql", func() {
	var dbUrl string

	BeforeEach(func() {

		dbUrl = "root@(localhost:3306)/mysql"
	})

	Describe("Initilize", func() {
		Context("when the database url is valid", func() {
			It("INTEGRATION should connect without error", func() {
				db := mysql.DBconn{}
				err := db.Initialize(dbUrl)
				Expect(err).ToNot(HaveOccurred())
				db.DB.DropTableIfExists("node_entities")
				db.DB.DropTableIfExists("ip_entities")
				db.DB.Close()
			})
		})
	})
})
