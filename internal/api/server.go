package api

import (
	"log/slog"
	"net/http"

	db "github.com/Lev2307/urlCutter/internal/db"
)

type Server struct {
	storage *db.SQLiteStorage
	logger  *slog.Logger
}

func NewServer(store *db.SQLiteStorage, logger *slog.Logger) *Server {
	return &Server{storage: store, logger: logger}
}

func (srv *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", srv.HandleServerStartPoint)
	mux.HandleFunc("POST /", srv.HandleCreateLink)
	return mux
}
