package api

import (
	"log/slog"
	"net/http"

	"github.com/Lev2307/urlCutter/internal/config"
	db "github.com/Lev2307/urlCutter/internal/db"
)

type Server struct {
	storage *db.SQLiteStorage
	logger  *slog.Logger
	cfg     config.Config
}

func NewServer(store *db.SQLiteStorage, logger *slog.Logger, cfg config.Config) *Server {
	return &Server{storage: store, logger: logger, cfg: cfg}
}

func (srv *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", srv.HandleServerStartPoint)
	mux.HandleFunc("POST /api/links", srv.HandleCreateLink)
	mux.HandleFunc("GET /{code}", srv.HandleRedirectLink) // также есть HEAD: это тот же GET, только без тела
	return mux
}
