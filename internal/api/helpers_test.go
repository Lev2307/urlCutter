package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lev2307/urlCutter/internal/config"
	db "github.com/Lev2307/urlCutter/internal/db"
	strg "github.com/Lev2307/urlCutter/internal/db"
	model "github.com/Lev2307/urlCutter/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func newTestStore(t *testing.T) *db.SQLiteStorage {
	t.Helper()

	dbNew, err := db.NewDatabase(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New database: %v", err)
	}
	t.Cleanup(func() { dbNew.Close() })
	return db.NewSQLiteStorage(dbNew)
}

func newServerHelper(t *testing.T, storage *strg.SQLiteStorage) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv, err := NewServer(storage, logger, config.Config{TokenTTL: time.Hour * 168}, nil)
	if err != nil {
		t.Fatalf("mdlwr test helpers: failed to init server: %v", err)
	}
	return srv
}

func newTestUser(t *testing.T, strg *db.SQLiteStorage, username string) string { // возвращает токен
	t.Helper()
	user := model.User{
		Username:     username,
		CreatedAt:    time.Now().UTC(),
		PasswordHash: "hashpasswordtest",
	}

	crUser, err := strg.CreateUser(context.Background(), user)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	genToken := generateToken()
	hToken := hashToken(genToken)
	tk := model.Token{
		UserID:    crUser.ID,
		TokenHash: hToken,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}

	if _, err := strg.CreateToken(context.Background(), tk); err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return genToken
}

func createLinkResponse(t *testing.T, srv *Server, inputLink model.Link, token string) model.Link {
	t.Helper()
	jsonData, err := json.Marshal(inputLink)
	if err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	handler := srv.Routes() // изменил с прямого вызова хэндлера на Mux
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/links", bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: wanted %d, got %d, body: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	var newLink model.Link
	if err := json.Unmarshal(w.Body.Bytes(), &newLink); err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	return newLink
}

func loginRequest(t *testing.T, srv *Server, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	var loginBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	loginBody.Username = username
	loginBody.Password = password

	jsonData, err := json.Marshal(loginBody)
	if err != nil {
		t.Fatalf("login handler: failed to serialize data: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)

	return w
}

func createUser(t *testing.T, storage *strg.SQLiteStorage, username, password string) model.User {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("create user helper: generate hash: %v", err)
	}
	testUser := model.User{
		Username:     username,
		CreatedAt:    time.Now(),
		PasswordHash: string(passwordHash),
	}
	crUser, err := storage.CreateUser(context.TODO(), testUser)
	if err != nil {
		t.Fatalf("create user helper: create user in db: %v", err)
	}
	return crUser
}

func createToken(t *testing.T, storage *strg.SQLiteStorage, userID int64, tkHash string, timeExpires time.Time) model.Token {
	t.Helper()
	tkData := model.Token{
		UserID:    userID,
		TokenHash: tkHash,
		CreatedAt: time.Now(),
		ExpiresAt: timeExpires,
	}
	crToken, err := storage.CreateToken(context.TODO(), tkData)
	if err != nil {
		t.Fatalf("mdlwr test helpers: create token - %v", err)
	}
	return crToken
}
