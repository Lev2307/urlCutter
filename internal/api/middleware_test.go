package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const DefaultPasswordForTests = "dummy-password"

func TestAuthMiddlewareNoHeader(t *testing.T) {
	storage := newTestStore(t)
	server := newServerHelper(t, storage)

	var called bool
	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotUserID, _ = authUserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := server.AuthMiddleware(next)                     // оборачиваем хэндлер заглушку в middleware аутентификации
	req := httptest.NewRequest(http.MethodGet, "/", nil) // URL любой, так как mux не используется
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if called {
		t.Errorf("Auth middleware no header: next handler called = %v, want %v", called, false)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Auth middleware no header: wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
	}
	if gotUserID != 0 {
		t.Errorf("Auth middleware no header: wanted userID - %d, got - %d", 0, gotUserID)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("Auth middleware no header: WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestAuthMiddlewareNoBearerPrefix(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	var gotUserID int64
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotUserID, _ = authUserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := srv.AuthMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token abc")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if called {
		t.Errorf("Auth middleware no bearer prefix: next handler called = %v, want %v", called, false)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Auth middleware no bearer prefix: wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
	}
	if gotUserID != 0 {
		t.Errorf("Auth middleware no bearer prefix: wanted userID - %d, got - %d", 0, gotUserID)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("Auth middleware no bearer prefix: WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestAuthMiddlewareUnknownToken(t *testing.T) {
	unknownToken := generateToken()
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	var called bool
	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotUserID, _ = authUserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := srv.AuthMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+unknownToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if called {
		t.Errorf("Auth middleware unknown token: next handler called = %v, want %v", called, false)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Auth middleware unknown token: wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
	}
	if gotUserID != 0 {
		t.Errorf("Auth middleware unknown token: wanted userID - %d, got - %d", 0, gotUserID)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("Auth middleware unknown token: WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestAuthMiddlewareExpiredToken(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	expiredToken := generateToken()
	hashed := hashToken(expiredToken)
	user := createUser(t, storage, "test", DefaultPasswordForTests)
	_ = createToken(t, storage, user.ID, hashed, time.Now().Add(-(time.Hour * 3)))

	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := srv.AuthMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if called {
		t.Errorf("Auth middleware expired token: next handler called = %v, want %v", called, false)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Auth middleware expired token: wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("Auth middleware expired token: WWW-Authenticate = %q, want %q", got, "Bearer")
	}
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	token := generateToken()
	hToken := hashToken(token)
	user := createUser(t, storage, "test", DefaultPasswordForTests)
	tk := createToken(t, storage, user.ID, hToken, time.Now().Add(srv.cfg.TokenTTL))

	var userID, tokenID int64
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		userID, _ = authUserIDFromContext(r.Context())
		tokenID, _ = authTokenIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := srv.AuthMiddleware(next)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if !called {
		t.Errorf("Auth middleware valid token: next handler called = %v, want %v", called, true)
	}
	if w.Code != http.StatusOK {
		t.Errorf("Auth middleware valid token: wanted status - %d, got - %d", http.StatusOK, w.Code)
	}
	if userID != tk.UserID {
		t.Errorf("Auth middleware valid token: failed to get userID from context. Wanted ID - %d, got - %d", 1, userID)
	}
	if tokenID != tk.ID {
		t.Errorf("Auth middleware valid token: failed to get tokenID from context. Wanted ID - %d, got - %d", tk.ID, tokenID)
	}
}
