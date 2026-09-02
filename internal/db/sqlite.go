package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type SQLiteStorage struct {
	db *sql.DB
}

func newCode() string {
	b := make([]byte, 6) // 3 байта - 4 символа -> 6 байт - 8 символов
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func isUniqueViolation(err error) bool {
	var sqliteError *sqlite.Error
	if errors.As(err, &sqliteError) {
		if sqliteError.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return true
		}
	}
	return false
}

func initDB(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL, 
		code TEXT NOT NULL UNIQUE
	);
	`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	return nil
}

func NewDatabase(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("create db conn: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := initDB(ctx, db); err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}

	return db, nil
}

func NewSQLiteStorage(db *sql.DB) *SQLiteStorage {
	return &SQLiteStorage{db: db}
}

func (strg *SQLiteStorage) CreateLink(ctx context.Context, linkName string) (string, error) {
	query := `
		INSERT INTO links (name, code) VALUES (?, ?)
		RETURNING code;
	`
	const maxAttempts = 5
	for range maxAttempts { // попытки на генерацию уникального кода
		var code string
		err := strg.db.QueryRowContext(ctx, query, linkName, newCode()).Scan(&code)
		if err == nil {
			return code, nil
		}
		if isUniqueViolation(err) { // проверка на уже существующую запись в таблице links с таким же code
			continue
		}
		return "", fmt.Errorf("create link: %w", err)
	}
	return "", fmt.Errorf("create link: не удалось подобрать код за %d попыток", maxAttempts)
}
