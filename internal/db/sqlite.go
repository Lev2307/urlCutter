package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	model "github.com/Lev2307/urlCutter/internal/model"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var ErrCodeTaken = errors.New("url shorten code was already taken")
var ErrUserExists = errors.New("user with such username already exists")
var ErrUserNotFound = errors.New("user not found")
var ErrTokenNotFound = errors.New("token not found")
var ErrLinkNotFound = errors.New("link not found")
var ErrTokenExists = errors.New("token already exists")
var ErrLinkAccessForbidden = errors.New("link author name not match current user")

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
		createdAt DATETIME NOT NULL,
		passwordHash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS tokens (
		id INTEGER PRIMARY KEY,
		userID INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		tokenHash TEXT NOT NULL UNIQUE,
		createdAt DATETIME NOT NULL,
		expiresAt DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY,
		title TEXT,
		originalUrl TEXT NOT NULL, 
		code TEXT NOT NULL UNIQUE,
		createdAt DATETIME NOT NULL,
		validTill DATETIME,
		clicks INTEGER NOT NULL DEFAULT 0,
		userID INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE
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
		INSERT INTO links (title, originalUrl, code, createdAt, validTill, userID) VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, title, originalUrl, code, createdAt, validTill, clicks, userID;
	`
	const maxAttempts = 5
	for range maxAttempts { // попытки на генерацию уникального кода
		var out model.Link
		err := strg.db.QueryRowContext(ctx, query, link.Title, link.OriginalUrl, newCode(), link.CreatedAt, link.ValidTill, link.UserID).Scan(&out.ID, &out.Title, &out.OriginalUrl, &out.Code, &out.CreatedAt, &out.ValidTill, &out.Clicks, &out.UserID) // database/sql - сам делает разыменовывание указателей
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

func (strg *SQLiteStorage) GetLinkByCode(ctx context.Context, code string) (model.Link, error) {
	var foundLink model.Link
	queryFindLink := `
	UPDATE links
	SET clicks = clicks + 1
	WHERE code = ? AND (validTill IS NULL OR validTill > ?)
	RETURNING id, title, originalUrl, code, createdAt, validTill, clicks, userID;
	`
	if err := strg.db.QueryRowContext(ctx, queryFindLink, code, time.Now().UTC()).Scan(&foundLink.ID, &foundLink.Title, &foundLink.OriginalUrl, &foundLink.Code, &foundLink.CreatedAt, &foundLink.ValidTill, &foundLink.Clicks, &foundLink.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Link{}, ErrLinkNotFound
		}
		return model.Link{}, fmt.Errorf("make query GetLinkByCode: %w", err)
	}

	return foundLink, nil
}

func (strg *SQLiteStorage) CreateUser(ctx context.Context, user model.User) (model.User, error) {
	query := `
		INSERT INTO users (username, email, createdAt, passwordHash) VALUES (?, ?, ?, ?)
		RETURNING id, username, email, createdAt;
		`
	var crUser model.User
	if err := strg.db.QueryRowContext(ctx, query, user.Username, user.Email, user.CreatedAt, user.PasswordHash).Scan(&crUser.ID, &crUser.Username, &crUser.Email, &crUser.CreatedAt); err != nil {
		if isUniqueViolation(err) { // проверка на уникальность пользователя
			return model.User{}, ErrUserExists
		}
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	return crUser, nil
}

func (strg *SQLiteStorage) GetUserByUsername(ctx context.Context, username string) (model.LoginUser, error) {
	query := `
		SELECT id, passwordHash FROM users WHERE username=?;
	`
	var loginUser model.LoginUser
	if err := strg.db.QueryRowContext(ctx, query, username).Scan(&loginUser.ID, &loginUser.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LoginUser{}, ErrUserNotFound
		}
		return model.LoginUser{}, fmt.Errorf("failed to get user by username: %w", err)
	}
	return loginUser, nil
}

func (strg *SQLiteStorage) CreateToken(ctx context.Context, token model.Token) (model.Token, error) {
	query := `
		INSERT INTO tokens (userID, tokenHash, createdAt, expiresAt) VALUES (?, ?, ?, ?)
		RETURNING id, userID, tokenHash, createdAt, expiresAt;
	`
	var crToken model.Token
	if err := strg.db.QueryRowContext(ctx, query, token.UserID, token.TokenHash, token.CreatedAt, token.ExpiresAt).Scan(&crToken.ID, &crToken.UserID, &crToken.TokenHash, &crToken.CreatedAt, &crToken.ExpiresAt); err != nil {
		if isUniqueViolation(err) {
			return model.Token{}, ErrTokenExists
		}
		return model.Token{}, fmt.Errorf("create token: %w", err)
	}
	return crToken, nil
}

func (strg *SQLiteStorage) GetTokenByHash(ctx context.Context, hash string) (model.TokenInfo, error) {
	query := `SELECT userID, expiresAt FROM tokens WHERE tokenHash=?`
	var foundTk model.TokenInfo
	if err := strg.db.QueryRowContext(ctx, query, hash).Scan(&foundTk.UserID, &foundTk.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.TokenInfo{}, ErrTokenNotFound
		}
		return model.TokenInfo{}, fmt.Errorf("failed to get token by hash: %w", err)
	}
	return foundTk, nil
}

func (strg *SQLiteStorage) GetLinksByUserID(ctx context.Context, userID int64, limit, offset int) ([]model.Link, error) {
	links := make([]model.Link, 0)
	query := `SELECT id, title, originalUrl, code, createdAt, validTill, clicks, userID FROM links WHERE userID = ? ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := strg.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return links, fmt.Errorf("get links by user: failed to get links by userID: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var lk model.Link
		if err := rows.Scan(&lk.ID, &lk.Title, &lk.OriginalUrl, &lk.Code, &lk.CreatedAt, &lk.ValidTill, &lk.Clicks, &lk.UserID); err != nil {
			return nil, fmt.Errorf("get links by user: failed to get link: %w", err)
		}
		links = append(links, lk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get links by user: iterate rows: %w", err)
	}
	return links, nil
}

func (strg *SQLiteStorage) DeleteUserLink(ctx context.Context, userID int64, code string) error {
	tx, err := strg.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete link: failed to open tx: %w", err)
	}
	defer tx.Rollback()

	var foundUserID int64
	queryFindLink := `SELECT userID FROM links WHERE code=?`
	if err := tx.QueryRowContext(ctx, queryFindLink, code).Scan(&foundUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLinkNotFound
		}
		return fmt.Errorf("delete link: failed to find link by code: %w", err)
	}

	if foundUserID != userID {
		return ErrLinkAccessForbidden
	}

	queryDeleteLink := `DELETE FROM links WHERE code=? AND userID=?`

	if _, err := tx.ExecContext(ctx, queryDeleteLink, code, userID); err != nil {
		return fmt.Errorf("delete link: failed to exec delete query: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete link: failed to commit query affection: %w", err)
	}
	return nil
}
