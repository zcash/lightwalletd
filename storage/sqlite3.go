package storage

import "database/sql"

func createBlockTable(conn *sql.DB) error {
	tableCreation := `
		CREATE TABLE IF NOT EXISTS blocks (
			height INTEGER,
			hash TEXT,
			has_sapling_tx BOOL,
			compact_encoding BLOB,
			PRIMARY KEY (height, hash)
		);
	`
	_, err := conn.Exec(tableCreation)
	return err
}
