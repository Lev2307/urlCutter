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
	mux.HandleFunc("GET /panic", srv.HandlePanic)

	return ChainMiddleware(mux, srv.RequestIDMiddleware, srv.RecoverMiddleware)
}

// контекст - односвязанный список. Начало - context.Background()
// Он решает одну задачу: протащить сквозь всю цепочку вызовов две вещи, которые нужны всем и не лезут в обычные аргументы.
// Первое — «этот запрос ещё кому-то нужен?». Клиент отключился, вкладку закрыли, сработал таймаут — и нет смысла дочитывать строки из базы. Поэтому ctx идёт первым параметром до самого низа: handlers.go:99 передаёт r.Context() в CreateLink, тот — в QueryRowContext, а драйвер по отмене прерывает запрос. Без этого сквозного канала пришлось бы тащить флажок отмены руками через каждую функцию.
// Второе — пара request-scoped данных: request-id, залогиненный пользователь, трассировка. То, что относится к этому конкретному запросу и нужно на любой глубине.
