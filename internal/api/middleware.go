package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"runtime/debug"
)

type Middleware func(http.Handler) http.Handler

type loggerKey struct{}
type requestIDKey struct{}

func newID() string {
	b := make([]byte, 6) // 3 байта - 4 символа -> 6 байт - 8 символов
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func loggerFrom(ctx context.Context) *slog.Logger {
	lg, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return lg
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func ChainMiddleware(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h) // mws[i] - функция удовл типу Middleware, например, func Logging(next http.Handler) http.Handler{}
	}
	return h
}

func (srv *Server) RecoverMiddleware(next http.Handler) http.Handler { // Recover — страховочная сетка под багами, о которых не было предусмотрено. Он отдаёт именно 500, потому что что конкретно сломалось — неизвестно.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				lg := loggerFrom(r.Context())
				lg.Error("PANIC RECOVERED", "err", err, "stack", string(debug.Stack()))
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (srv *Server) RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newID()
		lg := srv.logger.With("rid", id) // With возвращает новый логгер с уже приклеенными атрибутами, и дальше о rid можно не думать вообще.
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ctx = context.WithValue(ctx, loggerKey{}, lg) // loggerKey{} — композитный литерал пустой структуры. Памяти ноль, смысла внутри ноль; вся ценность в том, что такой тип уникален внутри программы
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

// Надевается последним = оказывается снаружи = выполняется первым.
// ChainMiddleware(mux, Recover, B, A)
// Recover(B(A(mux))) - так вызов чейна визуально выглядит, где Recover - функция для ловли паник
