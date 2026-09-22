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
	limiter *RateLimiter
}

const maxRequestBodyBytes = 8 << 10

func NewServer(store *db.SQLiteStorage, logger *slog.Logger, cfg config.Config, limiter *RateLimiter) *Server {
	return &Server{storage: store, logger: logger, cfg: cfg, limiter: limiter}
}

func (srv *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", srv.HandleServerStartPoint)
	mux.HandleFunc("POST /api/links", srv.HandleCreateLink)
	mux.HandleFunc("GET /{code}", srv.HandleRedirectLink) // также есть HEAD: это тот же GET, только без тела

	return ChainMiddleware(
		mux,
		srv.RequestIDMiddleware,
		MaxBytesMiddleware(maxRequestBodyBytes),
		srv.LoggingMiddleware,
		srv.RateLimitMiddleware,
		srv.RecoverMiddleware,
	)
	//	- RequestID снаружи — он кладёт логгер с rid в контекст, и всем, кто ниже, он нужен уже готовым.
	//	- MaxBytes сразу под ним — подменяет r.Body раньше, чем его кто-либо успеет прочитать.
	//	- Logging в середине — создаёт обёртку и переживает возврат Recover, поэтому видит в логе и упавшие запросы тоже.
	//	- RateLimit под Logging — тогда отказы 429 пишутся в лог наравне с обычными ответами и с тем же rid. Снаружи логгера они стали бы невидимыми, а именно их и надо видеть.
	//	  Главное в нём — return сразу после http.Error: отклонённый запрос не должен дойти ни до мукса, ни до базы, иначе дорогая работа всё равно выполнится, а статус 429 останется просто украшением.
	//	- Recover внутри — ловит панику хендлера и пишет свои 500 через обёртку, которую ему передал Logging. Значит, флаг HeaderGone работает, и лог покажет реальный статус.
}

// контекст - односвязанный список. Начало - context.Background()
// Он решает одну задачу: протащить сквозь всю цепочку вызовов две вещи, которые нужны всем и не лезут в обычные аргументы.
// Первое — «этот запрос ещё кому-то нужен?». Клиент отключился, вкладку закрыли, сработал таймаут — и нет смысла дочитывать строки из базы. Поэтому ctx идёт первым параметром до самого низа: handlers.go:99 передаёт r.Context() в CreateLink, тот — в QueryRowContext, а драйвер по отмене прерывает запрос. Без этого сквозного канала пришлось бы тащить флажок отмены руками через каждую функцию.
// Второе — пара request-scoped данных: request-id, залогиненный пользователь, трассировка. То, что относится к этому конкретному запросу и нужно на любой глубине.
