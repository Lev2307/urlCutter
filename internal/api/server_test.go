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

func TestCreatingLinkHandler(t *testing.T) {

	storage := newTestStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(storage, logger)

	tit := "new link for twitter"
	link := model.Link{
		Title:       &tit,
		OriginalUrl: "https://x.com/daskmfkjdahfkjdhsahjdfgsakdjhfgdashjgfdsakhjdgsaf",
	}
	jsonData, err := json.Marshal(link)
	if err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/", bytes.NewBuffer(jsonData))
	w := httptest.NewRecorder()
	srv.HandleCreateLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: wanted %d, got %d, body: %s", http.StatusCreated, w.Code, w.Body.String())
	}
	var newLink model.Link
	if err := json.Unmarshal(w.Body.Bytes(), &newLink); err != nil {
		t.Fatalf("failed to encode json: %v", err)
	}
	if *newLink.Title != *link.Title {
		t.Fatalf("failed to fetch link data: wanted title - %s, title - %s", *link.Title, *newLink.Title)
	}

	t.Logf("link data: code - %s", newLink.Code)
}
