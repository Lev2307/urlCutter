package api

// middleware - функция-обертка над http-запросом

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
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

// Надевается последним = оказывается снаружи = выполняется первым.
// ChainMiddleware(mux, Recover, B, A)
// Recover(B(A(mux))) - так вызов чейна визуально выглядит, где Recover - функция для ловли паник

type ResponseWriterWrapper struct { // кастом ResponseWriter. Нужен для того чтобы внешние middleware (Logging), чтобы после возврата ServeHTTP знать, чем кончился запрос: сам интерфейс отдаёт только три метода записи, а ServeHTTP ничего не возвращает — статус и размер иначе не достать.
	http.ResponseWriter
	StatusCode         int
	HeaderGone         bool
	BytesWrittenLength int
}

func (rww *ResponseWriterWrapper) WriteHeader(statusCode int) {
	if rww.HeaderGone { // второй вызов WriteHeader вызовет ошибку superfluous response.WriteHeader call, эта проверка нужна для её предотвращения
		return
	}
	rww.StatusCode = statusCode
	rww.HeaderGone = true
	rww.ResponseWriter.WriteHeader(statusCode) // делегирование вниз на уровень http.ResponseWriter
}

func (rww *ResponseWriterWrapper) Write(b []byte) (int, error) {
	if !rww.HeaderGone {
		rww.WriteHeader(http.StatusOK)
	}
	bytesN, err := rww.ResponseWriter.Write(b) // делегирование вниз на уровень http.ResponseWriter
	if err != nil {
		return bytesN, err
	}
	rww.BytesWrittenLength += bytesN // кусками берет, поэтому нельзя присваивание
	return bytesN, nil
}

func (rww *ResponseWriterWrapper) Unwrap() http.ResponseWriter {
	return rww.ResponseWriter
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

func (srv *Server) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		rww := &ResponseWriterWrapper{ResponseWriter: w, StatusCode: http.StatusOK} // Дефолт 200 — для хендлера, который не написал ни байта:
		next.ServeHTTP(rww, r)

		lvl := slog.LevelInfo
		logger := loggerFrom(r.Context())
		if rww.StatusCode >= 500 {
			lvl = slog.LevelError
		}
		logger.Log(r.Context(), lvl, "request", "method", r.Method, "path", r.URL.Path, slog.Int("status", rww.StatusCode), "bytes", rww.BytesWrittenLength, "duration", time.Since(now).String())
	})
}

// пример работы middleware-ов на хэндлере /api/links
//Q (RequestID):  id, логгер в ctx, X-Request-Id
//  L (Logging):  start := time.Now(); rww := &ResponseWriterWrapper{...}
//    R (Recover): поставил defer
//      mux → HandleCreateLink:
//         rww.WriteHeader(201)      ← методы обёртки: StatusCode=201, HeaderGone=true
//          json.Encode(rww)          ← BytesWrittenLength += n
//      ← вернулся
//    R: deferred отработал, recover() вернул nil — тихо вышел
//    ← вернулся
//  L: rww.StatusCode = 201, rww.BytesWrittenLength, time.Since(start) → lg.Info(...)
//  ← вернулся
//Q: ← вернулся
