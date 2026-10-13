package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lev2307/urlCutter/internal/config"
	model "github.com/Lev2307/urlCutter/internal/model"
)

func TestLinkCreatingHandler(t *testing.T) {
	storage := newTestStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv, err := NewServer(storage, logger, config.Config{TokenTTL: 168 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	tit := "new link for twitter"
	inputLink := model.Link{
		Title:       &tit,
		OriginalUrl: "https://x.com/daskmfkjdahfkjdhsahjdfgsakdjhfgdashjgfdsakhjdgsaf",
	}

	token := newTestUser(t, storage, "test1")

	outputLink := createLinkResponse(t, srv, inputLink, token)

	if *outputLink.Title != *inputLink.Title {
		t.Fatalf("failed to fetch link data: wanted title - %s, title - %s", *inputLink.Title, *outputLink.Title)
	}
	if outputLink.UserID != 1 {
		t.Fatalf("failed to get userID: wanted userID - %d, givenUserID - %d", 1, outputLink.UserID)
	}

	t.Logf("link data: code - %s", outputLink.Code)
}

func TestLinkRedirectByCodeHandler(t *testing.T) {
	storage := newTestStore(t)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv, err := NewServer(storage, logger, config.Config{TokenTTL: 168 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	tit := "insta kai angel"
	inputLink := model.Link{
		Title:       &tit,
		OriginalUrl: "https://instagram.com/kaiAngel",
	}

	token := newTestUser(t, storage, "test1")
	outputLink := createLinkResponse(t, srv, inputLink, token)

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

func TestLinkRedirectByCodeHandlerWrongCode(t *testing.T) {
	wrongCodeLength := "abc"     // неправильная длина кода
	nonExistedCode := "abc123ui" // несуществующий код, но с правильной длиной

	storage := newTestStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv, _ := NewServer(storage, logger, config.Config{TokenTTL: 168 * time.Hour}, nil)

	routes := srv.Routes()
	rLength := httptest.NewRequest(http.MethodGet, "/"+wrongCodeLength, nil)
	wLength := httptest.NewRecorder()
	routes.ServeHTTP(wLength, rLength) // serveHTTP - фундамент всей вэб сервреной экосистемы. Он обработывает входящий HTTP-запрос и формирует ответ, который отправляется обратно клиенту
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

func TestLogoutHandler(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)
	handler := srv.Routes()

	user := createUser(t, storage, "test1", DefaultPasswordForTests)

	token := generateToken()
	_ = createToken(t, storage, user.ID, hashToken(token), time.Now().Add(srv.cfg.TokenTTL))

	token2 := generateToken()
	_ = createToken(t, storage, user.ID, hashToken(token2), time.Now().Add(srv.cfg.TokenTTL)) // создание второго токена

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("logout handler test: wanted status - %d, got - %d", http.StatusNoContent, w.Code)
	}

	// проверка удаления токена через запрос на защищенный url
	reqLinks := httptest.NewRequest(http.MethodGet, "/api/links", nil)
	reqLinks.Header.Set("Authorization", "Bearer "+token)
	wLinks := httptest.NewRecorder()
	handler.ServeHTTP(wLinks, reqLinks)

	if wLinks.Code != http.StatusUnauthorized {
		t.Errorf("logout handler test: status on auth route after logout. Wanted - %d, got - %d", http.StatusUnauthorized, wLinks.Code)
	}

	reqLinks2 := httptest.NewRequest(http.MethodGet, "/api/links", nil)
	reqLinks2.Header.Set("Authorization", "Bearer "+token2)
	wLinks2 := httptest.NewRecorder()
	handler.ServeHTTP(wLinks2, reqLinks2)

	if wLinks2.Code != http.StatusOK {
		t.Errorf("logout handler test: status on auth route with other token of same user. Wanted - %d, got - %d", http.StatusOK, wLinks2.Code)
	}
}

func TestLinkDeleteHandler(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	userAuthor := createUser(t, storage, "linkAuthor", DefaultPasswordForTests)
	userOther := createUser(t, storage, "other", DefaultPasswordForTests)

	tokenAuthor := generateToken()
	_ = createToken(t, storage, userAuthor.ID, hashToken(tokenAuthor), time.Now().Add(srv.cfg.TokenTTL)) // токен создателя ссылки

	tokenOtherUser := generateToken()
	_ = createToken(t, storage, userOther.ID, hashToken(tokenOtherUser), time.Now().Add(srv.cfg.TokenTTL)) // токен другого пользователя

	vt := time.Now().Add(time.Hour * 48)
	link := model.Link{
		OriginalUrl: "https://example.com/123",
		CreatedAt:   time.Now(),
		ValidTill:   &vt,
		UserID:      userAuthor.ID,
	}
	crLink, err := storage.CreateLink(context.TODO(), link)
	if err != nil {
		t.Fatalf("delete handler: create link: %v", err)
	}
	t.Run("Статус 403 на попытку удаления чужой ссылки", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/links/"+crLink.Code, nil)
		req.Header.Set("Authorization", "Bearer "+tokenOtherUser)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("delete handler: other user delete attempt. Wanted status - %d, got - %d", http.StatusForbidden, w.Code)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/"+crLink.Code, nil)
		w2 := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w2, req2)

		if w2.Code != http.StatusFound {
			t.Errorf("delete handler: link found after fake deletion by non-author. Wanted status - %d, got - %d", http.StatusFound, w2.Code)
		}
	})

	t.Run("Статус 404 на попытку удаления несуществующей ссылки", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/links/nonexist", nil)
		req.Header.Set("Authorization", "Bearer "+tokenAuthor)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("delete handler: non existed link. Wanted status - %d, got - %d", http.StatusNotFound, w.Code)
		}
	})
	t.Run("Статус 204 на удаление собственной ссылки", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/links/"+crLink.Code, nil)
		req.Header.Set("Authorization", "Bearer "+tokenAuthor)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("delete handler: success delete. Wanted status - %d, got - %d", http.StatusNoContent, w.Code)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/"+crLink.Code, nil)
		w2 := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w2, req2)

		if w2.Code != http.StatusNotFound {
			t.Errorf("delete handler: link not found after deletion. Wanted status - %d, got - %d", http.StatusNotFound, w2.Code)
		}
	})
}

func TestLoginHandler(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	username := "testlogin"
	password := "testPassword"
	_ = createUser(t, storage, username, password)

	t.Run("Успешный логин", func(t *testing.T) {
		w := loginRequest(t, srv, username, password)
		if w.Code != http.StatusOK {
			t.Errorf("login handler: success login. Wanted status - %d, got - %d", http.StatusOK, w.Code)
		}

		var output struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if err := json.NewDecoder(w.Body).Decode(&output); err != nil {
			t.Fatalf("login handler: success login. Failed to get json data from response: %v", err)
		}

		// проверка доступа на защищенный маршрут с полученным токеном
		reqLinks := httptest.NewRequest(http.MethodGet, "/api/links", nil)
		reqLinks.Header.Set("Authorization", "Bearer "+output.Token)
		wLinks := httptest.NewRecorder()
		srv.Routes().ServeHTTP(wLinks, reqLinks)

		if wLinks.Code != http.StatusOK {
			t.Errorf("login handler: success login. Failed to get access to route after login. Wanted status - %d, got - %d", http.StatusOK, wLinks.Code)
		}
	})
	t.Run("Неверный пароль: 401", func(t *testing.T) {
		w := loginRequest(t, srv, username, "WrongPassword")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("login handler: failed login - wrong password. Wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
		}

		if strings.TrimSpace(w.Body.String()) != UnAuthorizedStatusErrorMessage {
			t.Errorf("login handler: failed login - wrong password. Wanted message - %s, got - %s", UnAuthorizedStatusErrorMessage, strings.TrimSpace(w.Body.String()))
		}
	})
	t.Run("Несуществующий пользователь: 401", func(t *testing.T) {
		w := loginRequest(t, srv, "newUser", "WrongPassword")

		if w.Code != http.StatusUnauthorized {
			t.Errorf("login handler: failed login - unknown user. Wanted status - %d, got - %d", http.StatusUnauthorized, w.Code)
		}
		if strings.TrimSpace(w.Body.String()) != UnAuthorizedStatusErrorMessage {
			t.Errorf("login handler: failed login - unknown user. Wanted message - %s, got - %s", UnAuthorizedStatusErrorMessage, strings.TrimSpace(w.Body.String()))
		}
	})
}

func TestLinkListHandler(t *testing.T) {
	storage := newTestStore(t)
	srv := newServerHelper(t, storage)

	user := createUser(t, storage, "test", "password123")
	token := generateToken()
	_ = createToken(t, storage, user.ID, hashToken(token), time.Now().Add(srv.cfg.TokenTTL))

	for i := range 5 {
		vt := time.Now().Add(time.Hour * 48)
		title := strconv.Itoa(i)
		link := model.Link{
			OriginalUrl: "https://example.com/" + title,
			Title:       &title,
			CreatedAt:   time.Now(),
			ValidTill:   &vt,
			UserID:      user.ID,
		}
		_, err := storage.CreateLink(context.TODO(), link)
		if err != nil {
			t.Fatalf("link list: init link: %v", err)
		}
	}

	otherUser := createUser(t, storage, "other", "other123") // другой пользователь
	vtOther := time.Now().Add(time.Minute * 30)
	titleOther := "other link for me hehe"
	linkOther := model.Link{ // его ссылка
		OriginalUrl: "https://google.com/",
		Title:       &titleOther,
		CreatedAt:   time.Now(),
		ValidTill:   &vtOther,
		UserID:      otherUser.ID,
	}
	_, err := storage.CreateLink(context.TODO(), linkOther)
	if err != nil {
		t.Fatalf("link list: init link: %v", err)
	}

	t.Run("limit = 2", func(t *testing.T) {
		limit := "2"
		r := httptest.NewRequest(http.MethodGet, "/api/links?limit="+limit, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("link list: status wanted - %d, got - %d", http.StatusOK, w.Code)
		}

		var response struct {
			Links  []model.Link `json:"links"`
			Limit  int          `json:"limit"`
			Offset int          `json:"offset"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("link list: failed to get json data from response: %v", err)
		}

		lm, _ := strconv.Atoi(limit)
		if len(response.Links) != lm {
			t.Errorf("link list: links count wanted - %d, got - %d", lm, len(response.Links))
		}
	})
	t.Run("offset = 4", func(t *testing.T) {
		offset := "4"
		r := httptest.NewRequest(http.MethodGet, "/api/links?offset="+offset, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("link list: status wanted - %d, got - %d", http.StatusOK, w.Code)
		}

		var response struct {
			Links  []model.Link `json:"links"`
			Limit  int          `json:"limit"`
			Offset int          `json:"offset"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("link list: failed to get json data from response: %v", err)
		}
		if len(response.Links) != 1 {
			t.Errorf("link list: links count wanted - %d, got - %d", 1, len(response.Links))
		}
	})
	t.Run("offset больше количества ссылок", func(t *testing.T) {
		offset := "20"
		r := httptest.NewRequest(http.MethodGet, "/api/links?offset="+offset, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("link list: status wanted - %d, got - %d", http.StatusOK, w.Code)
		}

		// декодируем не в структуру, а в "сырые" куски JSON: так видно, что именно лежит в поле links - [] или null
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
			t.Fatalf("link list: failed to get json data from response: %v", err)
		}

		if got := string(raw["links"]); got != "[]" {
			t.Errorf("link list: links wanted %q, got %q", "[]", got)
		}
	})

	t.Run("offset < 0", func(t *testing.T) {
		offset := "-50"
		r := httptest.NewRequest(http.MethodGet, "/api/links?offset="+offset, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusBadRequest {
			t.Errorf("link list: status wanted - %d, got - %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("limit = 1000", func(t *testing.T) {
		limit := "1000"
		r := httptest.NewRequest(http.MethodGet, "/api/links?limit="+limit, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("link list: status wanted - %d, got - %d", http.StatusOK, w.Code)
		}

		var response struct {
			Links  []model.Link `json:"links"`
			Limit  int          `json:"limit"`
			Offset int          `json:"offset"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("link list: failed to get json data from response: %v", err)
		}

		if response.Limit != maxLinksLimit {
			t.Errorf("link list: wanted limit - %d, got - %d", maxLinksLimit, response.Limit)
		}
	})
	t.Run("ссылки другого пользователя в выдачу не попадают", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/links", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("link list: status wanted - %d, got - %d", http.StatusOK, w.Code)
		}

		var response struct {
			Links  []model.Link `json:"links"`
			Limit  int          `json:"limit"`
			Offset int          `json:"offset"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("link list: failed to get json data from response: %v", err)
		}

		if len(response.Links) != 5 {
			t.Errorf("link list: links count wanted - %d, got - %d", 5, len(response.Links))
		}
	})
}
