package db

import (
	"strconv"
	"time"

	"com.lc.go.codepush/server/config"

	_ "github.com/newrelic/go-agent/v3/integrations/nrmysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ormDB *gorm.DB

func GetUserDB() (odb *gorm.DB, err error) {
	if ormDB != nil {
		odb = ormDB
		return
	}
	dbConfig := config.GetConfig().DBUser
	dsnSource := dbConfig.Write.UserName + ":" + dbConfig.Write.Password + "@tcp(" + dbConfig.Write.Host + ":" + strconv.Itoa(int(dbConfig.Write.Port)) + ")/" + dbConfig.Write.DBname + "?charset=utf8mb4&parseTime=True&loc=Local"

	// New Relic datastore instrumentation:
	// Using the "nrmysql" driver enables MySQL metrics. To associate DB segments with the
	// current HTTP transaction, queries must run with the request context
	// (e.g. via `db.WithContext(c.Request.Context())` in handlers).
	db, err := gorm.Open(mysql.New(mysql.Config{
		DriverName: "nrmysql",
		DSN:        dsnSource,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		return
	}
	sqlDb, err := db.DB()
	if err != nil {
		return
	}
	sqlDb.SetMaxIdleConns(int(dbConfig.MaxIdleConns))
	sqlDb.SetMaxOpenConns(int(dbConfig.MaxOpenConns))
	sqlDb.SetConnMaxIdleTime(time.Duration(dbConfig.MaxIdleConns))

	ormDB = db
	odb = ormDB
	return
}
