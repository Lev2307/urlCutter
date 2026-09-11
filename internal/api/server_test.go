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

	"github.com/Lev2307/urlCutter/internal/config"
	db "github.com/Lev2307/urlCutter/internal/db"
	model "github.com/Lev2307/urlCutter/internal/model"
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

func createLinkResponse(t *testing.T, srv *Server, inputLink model.Link) model.Link {
	t.Helper()
	jsonData, err := json.Marshal(inputLink)
	if err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/links", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()
	srv.HandleCreateLink(w, req) // вызов напрямую хэндлера
	if w.Code != http.StatusCreated {
		t.Fatalf("status: wanted %d, got %d, body: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	var newLink model.Link
	if err := json.Unmarshal(w.Body.Bytes(), &newLink); err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	return newLink
}

func TestCreatingLinkHandler(t *testing.T) {

	storage := newTestStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(storage, logger, config.Config{})

	tit := "new link for twitter"
	inputLink := model.Link{
		Title:       &tit,
		OriginalUrl: "https://x.com/daskmfkjdahfkjdhsahjdfgsakdjhfgdashjgfdsakhjdgsaf",
	}
	outputLink := createLinkResponse(t, srv, inputLink)

	if *outputLink.Title != *inputLink.Title {
		t.Fatalf("failed to fetch link data: wanted title - %s, title - %s", *inputLink.Title, *outputLink.Title)
	}

	t.Logf("link data: code - %s", outputLink.Code)
}

func TestRedirectByCodeHandler(t *testing.T) {
	storage := newTestStore(t)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(storage, logger, config.Config{})

	tit := "insta kai angel"
	inputLink := model.Link{
		Title:       &tit,
		OriginalUrl: "https://instagram.com/kaiAngel",
	}
	outputLink := createLinkResponse(t, srv, inputLink)

	handler := srv.Routes()
	req := httptest.NewRequest(http.MethodGet, "/"+outputLink.Code, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req) // вызов

	if w.Code != http.StatusFound {
		t.Errorf("status: wanted %d, got %d, body: %s", http.StatusFound, w.Code, w.Body.String())
	}

	if w.Header().Get("Location") != outputLink.OriginalUrl {
		t.Errorf("redirect url: wanted %s, got %s", outputLink.OriginalUrl, w.Header().Get("Location"))
	}
}

func TestRedirectByCodeHandlerWrongCode(t *testing.T) {
	wrongCodeLength := "abc"     // неправильная длина кода
	nonExistedCode := "abc123ui" // несуществующий код, но с правильной длиной

	storage := newTestStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(storage, logger, config.Config{})

	routes := srv.Routes()
	rLength := httptest.NewRequest(http.MethodGet, "/"+wrongCodeLength, nil)
	wLength := httptest.NewRecorder()
	routes.ServeHTTP(wLength, rLength)
	if wLength.Code != http.StatusNotFound {
		t.Errorf("status: wanted %d, got %d, body: %s", http.StatusNotFound, wLength.Code, wLength.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/"+nonExistedCode, nil)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: wanted %d, got %d, body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

// сокет по-простому - это сетевое соединение, работающее как файл через который перекидываются байты через два буфера. А буфер сам по себе - это место, где эти байты хранятся для обработки.
