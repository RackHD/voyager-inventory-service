package mysql

import (
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql" // Blank import because the library says to.
	"github.com/jinzhu/gorm"
	"github.com/RackHD/voyager-utilities/models"
)

// DBconn is a struct for maintaining a connection to the MySQL Database
type DBconn struct {
	DB *gorm.DB
}

// Initialize attempts to open and verify a connection to the DB
func (d *DBconn) Initialize(address string) error {
	var err error

	for i := 0; i < 18; i++ {
		d.DB, err = gorm.Open("mysql", address)
		if err != nil {
			log.Printf("Could not connect. Sleeping for 10s %s\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		err = d.DB.DB().Ping()
		if err != nil {
			log.Printf("Could not Ping. Sleeping for 10s %s\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		d.createIfNotExist(&models.NodeEntity{})
		d.createIfNotExist(&models.IPEntity{})

		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("Could not connect to database: %s\n", err)
}

func (d *DBconn) createIfNotExist(entity interface{}) {
	if hasTable := d.DB.HasTable(entity); !hasTable {
		if err := d.DB.CreateTable(entity).Error; err != nil {
			log.Printf("Error creating table: %s\n", err)
		}
	}
}
