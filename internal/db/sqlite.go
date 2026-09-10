package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	model "github.com/Lev2307/urlCutter/internal/model"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var ErrCodeTaken = errors.New("url shorten code was already taken")

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
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		email TEXT,
		passwordHash TEXT NOT NULL,
		createdAt DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY,
		title TEXT,
		originalUrl TEXT NOT NULL, 
		code TEXT NOT NULL UNIQUE,
		createdAt DATETIME NOT NULL,
		validTill DATETIME,
		clicks INTEGER NOT NULL DEFAULT 0,
		userID INTEGER REFERENCES users(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_links_userID_title ON links(userID, title);
	` // title - я сделал unique для отдельного юзера: title может повторяться у множества юзеров, но title у одного пользователя должен быть уникальным
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return err
	}
	return nil
}

func NewDatabase(ctx context.Context, path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_time_format=datetime&_timezone=UTC", path)
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

func (strg *SQLiteStorage) CreateLink(ctx context.Context, link model.Link) (model.Link, error) {
	query := `
		INSERT INTO links (title, originalUrl, code, createdAt, validTill) VALUES (?, ?, ?, ?, ?)
		RETURNING id, title, originalUrl, code, createdAt, validTill, clicks;
	`
	const maxAttempts = 5
	for range maxAttempts { // попытки на генерацию уникального кода
		var out model.Link
		err := strg.db.QueryRowContext(ctx, query, link.Title, link.OriginalUrl, newCode(), link.CreatedAt, link.ValidTill).Scan(&out.ID, &out.Title, &out.OriginalUrl, &out.Code, &out.CreatedAt, &out.ValidTill, &out.Clicks) // database/sql - сам делает разыменовывание указателей
		if err == nil {
			return out, nil
		}
		if isUniqueViolation(err) { // проверка на уже существующую запись в таблице links с таким же code
			continue
		}
		return model.Link{}, fmt.Errorf("create link: %w", err)
	}
	return model.Link{}, ErrCodeTaken
}
