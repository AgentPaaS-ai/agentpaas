package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// SQLiteIndexer manages an SQLite index of audit records that is rebuildable
// from the JSONL audit file. The index supports fast queries for record
// lookup, chain inspection, and export.
type SQLiteIndexer struct {
	db *sql.DB
}

// NewSQLiteIndexer opens or creates an SQLite index at the given path.
// If the index already exists, it is opened without re-importing data.
// Callers should call Rebuild to ensure the index is current.
func NewSQLiteIndexer(dbPath string) (*SQLiteIndexer, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	ix := &SQLiteIndexer{db: db}

	// Create tables if they don't exist
	if err := ix.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return ix, nil
}

// ensureSchema creates the audit_records table if it doesn't exist.
func (ix *SQLiteIndexer) ensureSchema() error {
	_, err := ix.db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_records (
			seq INTEGER PRIMARY KEY,
			prev_hash TEXT NOT NULL,
			record_hash TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			event_type TEXT NOT NULL,
			deployment_mode TEXT NOT NULL,
			actor TEXT NOT NULL,
			payload TEXT,
			hosted_context TEXT
		);
	`)
	return err
}

// Rebuild drops all existing data and re-imports from the JSONL file.
// The audit chain is read and verified during import; if the chain is
// broken, the records up to the break point are imported and the error
// is returned so the caller can decide how to handle it.
func (ix *SQLiteIndexer) Rebuild(auditPath string) error {
	// Read and verify the audit chain
	records, err := readAuditChain(auditPath)

	// Begin transaction for bulk insert
	tx, txErr := ix.db.Begin()
	if txErr != nil {
		return fmt.Errorf("begin tx: %w", txErr)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear existing data
	if _, execErr := tx.Exec("DELETE FROM audit_records"); execErr != nil {
		return fmt.Errorf("clear table: %w", execErr)
	}

	// Prepare insert statement
	stmt, prepErr := tx.Prepare(`
		INSERT INTO audit_records (seq, prev_hash, record_hash, timestamp, event_type, deployment_mode, actor, payload, hosted_context)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if prepErr != nil {
		return fmt.Errorf("prepare insert: %w", prepErr)
	}
	defer func() { _ = stmt.Close() }()

	for _, rec := range records {
		payloadJSON := marshalJSON(rec.Payload)
		var hostedCtxJSON string
		if rec.HostedContext != nil {
			hostedCtxJSON = marshalJSON(rec.HostedContext)
		}

		if _, execErr := stmt.Exec(
			rec.Seq, rec.PrevHash, rec.RecordHash,
			rec.Timestamp, rec.EventType, rec.DeploymentMode, rec.Actor,
			payloadJSON, hostedCtxJSON,
		); execErr != nil {
			return fmt.Errorf("insert seq=%d: %w", rec.Seq, execErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit: %w", commitErr)
	}

	return err // return the chain error if any (records up to break were imported)
}

// marshalJSON safely converts a value to a JSON string for SQLite storage.
func marshalJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	return string(b)
}

// RecordCount returns the number of records in the SQLite index.
func (ix *SQLiteIndexer) RecordCount() (int, error) {
	var count int
	err := ix.db.QueryRow("SELECT COUNT(*) FROM audit_records").Scan(&count)
	return count, err
}

// QueryBySeq retrieves a single record by sequence number.
func (ix *SQLiteIndexer) QueryBySeq(seq int64) (*AuditRecord, error) {
	var rec AuditRecord
	var payloadJSON, hostedCtxJSON sql.NullString

	err := ix.db.QueryRow(`
		SELECT seq, prev_hash, record_hash, timestamp, event_type, deployment_mode, actor, payload, hosted_context
		FROM audit_records WHERE seq = ?
	`, seq).Scan(
		&rec.Seq, &rec.PrevHash, &rec.RecordHash,
		&rec.Timestamp, &rec.EventType, &rec.DeploymentMode, &rec.Actor,
		&payloadJSON, &hostedCtxJSON,
	)
	if err != nil {
		return nil, err
	}

	if payloadJSON.Valid && payloadJSON.String != "" {
		if err := json.Unmarshal([]byte(payloadJSON.String), &rec.Payload); err != nil {
			rec.Payload = map[string]interface{}{"unmarshal_error": err.Error()}
		}
	} else {
		rec.Payload = map[string]interface{}{}
	}

	if hostedCtxJSON.Valid && hostedCtxJSON.String != "" {
		var hc HostedContext
		if err := json.Unmarshal([]byte(hostedCtxJSON.String), &hc); err == nil {
			rec.HostedContext = &hc
		}
	}

	return &rec, nil
}

// QueryByEventType retrieves all records matching the given event type,
// ordered by seq ascending. Limit bounds the result set (0 = no limit).
func (ix *SQLiteIndexer) QueryByEventType(eventType string, limit int) ([]AuditRecord, error) {
	query := "SELECT seq, prev_hash, record_hash, timestamp, event_type, deployment_mode, actor FROM audit_records WHERE event_type = ? ORDER BY seq"
	args := []interface{}{eventType}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := ix.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []AuditRecord
	for rows.Next() {
		var rec AuditRecord
		if err := rows.Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash, &rec.Timestamp, &rec.EventType, &rec.DeploymentMode, &rec.Actor); err != nil {
			return records, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// Close closes the SQLite database connection.
func (ix *SQLiteIndexer) Close() error {
	return ix.db.Close()
}

// RebuildSQLiteIndex is a convenience function that opens a SQLite index,
// rebuilds it from the given audit JSONL path, and closes it.
func RebuildSQLiteIndex(auditPath, dbPath string) error {
	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()
	return ix.Rebuild(auditPath)
}

// readAuditChainFromFile is a helper that reads all audit records from a
// JSONL file. It is provided by verifier.go (readAuditChain).