package api

// middleware - функция-обертка над http-запросом

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	strg "github.com/Lev2307/urlCutter/internal/db"
)

type Middleware func(http.Handler) http.Handler

type loggerKey struct{}
type requestIDKey struct{}
type userIDKey struct{}

func newID() string {
	b := make([]byte, 6) // 3 байта - 4 символа -> 6 байт - 8 символов
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func ChainMiddleware(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h) // mws[i] - функция удовл типу Middleware, например, func Logging(next http.Handler) http.Handler{}
	}
	return h
}

func MaxBytesMiddleware(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
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

func (srv *Server) log(r *http.Request) *slog.Logger { // метод для получения экземпляра логгера с полем - id запроса
	lg, ok := r.Context().Value(loggerKey{}).(*slog.Logger)
	if !ok {
		return srv.logger
	}
	return lg
}

func (srv *Server) RecoverMiddleware(next http.Handler) http.Handler { // Recover — страховочная сетка под багами, о которых не было предусмотрено. Он отдаёт именно 500, потому что что конкретно сломалось — неизвестно.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				lg := srv.log(r)
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
		logger := srv.log(r)
		if rww.StatusCode >= 500 {
			lvl = slog.LevelError
		}
		logger.Log(r.Context(), lvl, "request", "method", r.Method, "path", r.URL.Path, slog.Int("status", rww.StatusCode), "bytes", rww.BytesWrittenLength, "duration", time.Since(now).String())
	})
}

func (srv *Server) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srv.limiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		allowedFlag, wait := srv.limiter.allow(host)
		if !allowedFlag {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds())))) // math.Ceil - округление вверх
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authUserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey{}).(int64)
	return userID, ok
}

func (srv *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		authToken, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		hToken := hashToken(authToken)
		foundTk, err := srv.storage.GetTokenByHash(r.Context(), hToken)
		if err != nil {
			if errors.Is(err, strg.ErrTokenNotFound) {
				srv.log(r).Warn("invalid or expired token", "err", err)
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}
			srv.log(r).Error("failed to get auth token", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// проверка просроченности токена
		if time.Now().After(foundTk.ExpiresAt) {
			srv.log(r).Warn("token was expired")
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		lg := srv.log(r).With("userID", foundTk.UserID)
		ctx := context.WithValue(r.Context(), loggerKey{}, lg)
		ctx = context.WithValue(ctx, userIDKey{}, foundTk.UserID)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// пример работы middleware-ов на хэндлере /api/links (дальше уже добавляются новые хэндлеры -> схема поменяется)
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
