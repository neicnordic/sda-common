// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"database/sql"
	"fmt"
	"hash"
	"time"

	log "github.com/sirupsen/logrus"

	// Needed implicitly to enable Postgres driver
	_ "github.com/lib/pq"
)

// DBConf stores information about how to connect to the database backend
type DBConf struct {
	Host       string
	Port       int
	User       string
	Password   string
	Database   string
	CACert     string
	SslMode    string
	ClientCert string
	ClientKey  string
}

// SDAdb struct that acts as a receiver for the DB update methods
type SDAdb struct {
	db      *sql.DB
	Version int
	Config  DBConf
}

// FileInfo is used by ingest for file metadata (path, size, checksum)
type FileInfo struct {
	Checksum          hash.Hash
	Size              int64
	Path              string
	DecryptedChecksum hash.Hash
	DecryptedSize     int64
}

// SchemaName is the name of the remote database schema to query
var SchemaName = "sda"

// ConnectTimeout is how long to try to establish a connection to the database.
// If set to <= 0, the system will try to connect forever.
var ConnectTimeout = 1 * time.Hour

// FastConnectTimeout sets how long the system will try to connect to the
// database using the FastConnectRate.
var FastConnectTimeout = 2 * time.Minute

// FastConnectRate is how long to wait between attempts to connect to the
// database during the before FastConnectTimeout.
var FastConnectRate = 5 * time.Second

// SlowConnectRate is how long to wait between attempts to connect to the
// database during the after FastConnectTimeout.
var SlowConnectRate = 1 * time.Minute

// NewSDAdb creates a new DB connection from the given DBConf variables.
// Currently, only postgresql connections are supported.
func NewSDAdb(config DBConf) (*SDAdb, error) {

	dbs := SDAdb{db: nil, Version: -1, Config: config}

	err := dbs.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	dbs.Version, err = dbs.getVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch database schema version: %v", err)
	}

	return &dbs, nil
}

// Connect attempts to connect to the database using the given dbs.ConnInfo.
// Connection retries and timeouts are controlled by the ConnectTimeout,
// FastConnectTimeout, FastConnectRate, and SlowConnectRate variables.
func (dbs *SDAdb) Connect() error {
	start := time.Now()

	// if already connected - do nothing
	if dbs.db != nil {
		err := dbs.db.Ping()
		if err == nil {
			log.Infoln("Already connected to database")
			return nil
		}
	}

	// default error
	err := fmt.Errorf("failed to connect within reconnect time")

	log.Infoln("Connecting to database")
	log.Debugf("host: %s:%d, database: %s, user: %s", dbs.Config.Host,
		dbs.Config.Port, dbs.Config.Database, dbs.Config.User)

	for ConnectTimeout <= 0 || ConnectTimeout > time.Since(start) {
		dbs.db, err = sql.Open(dbs.Config.PgDataSource())
		if err == nil {
			log.Infoln("Connected to database")
			return nil
		}
		if time.Since(start) < FastConnectTimeout {
			log.Debug("Fast reconnect")
			time.Sleep(FastConnectRate)
		} else {
			log.Debug("Slow reconnect")
			time.Sleep(SlowConnectRate)
		}
	}

	return err
}

// PgDataSource builds a postgresql data source string to use with sql.Open().
func (config *DBConf) PgDataSource() (string, string) {
	connInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.Database, config.SslMode)

	if config.SslMode == "disable" {
		return "postgres", connInfo
	}

	if config.CACert != "" {
		connInfo += fmt.Sprintf(" sslrootcert=%s", config.CACert)
	}

	if config.ClientCert != "" {
		connInfo += fmt.Sprintf(" sslcert=%s", config.ClientCert)
	}

	if config.ClientKey != "" {
		connInfo += fmt.Sprintf(" sslkey=%s", config.ClientKey)
	}

	return "postgres", connInfo
}

// getVersion fetches the database schema version. This function return -1 when
// the version could not be fetched.
func (dbs *SDAdb) getVersion() (int, error) {

	dbs.checkAndReconnectIfNeeded()

	log.Debug("Fetching database schema version")

	query := "SELECT MAX(version) FROM local_ega.dbschema_version"

	var dbVersion = -1

	err := dbs.db.QueryRow(query).Scan(&dbVersion)
	return dbVersion, err
}

// checkAndReconnectIfNeeded validates the current connection with a ping
// and tries to reconnect if necessary
func (dbs *SDAdb) checkAndReconnectIfNeeded() {
	err := dbs.db.Ping()
	if err != nil {
		log.Errorf("Database connection problem: %v", err)
		dbs.Connect()
	}
}

// Close terminates the connection to the database
func (dbs *SDAdb) Close() {
	err := dbs.db.Ping()
	if err == nil {
		log.Info("Closing database connection")
		dbs.db.Close()
	}
}
