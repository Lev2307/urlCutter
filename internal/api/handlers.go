package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	db "github.com/Lev2307/urlCutter/internal/db"
	model "github.com/Lev2307/urlCutter/internal/model"
	"golang.org/x/crypto/bcrypt"
)

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var ErrInvalidURL = errors.New("invalid url")
var ErrLengthURL = errors.New("link length is gt 2048 symbols")
var ErrLengthTitle = errors.New("title length is gt 64 symbols")

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func normalizeUrl(raw, ownHost string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return "", ErrLengthURL
	}
	parsedUrl, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidURL
	}

	if parsedUrl.Scheme != "https" && parsedUrl.Scheme != "http" {
		return "", ErrInvalidURL
	}
	if parsedUrl.Host == "" {
		return "", ErrInvalidURL
	}
	if parsedUrl.User != nil {
		return "", ErrInvalidURL
	}

	if strings.ToLower(parsedUrl.Hostname()) == ownHost {
		return "", ErrInvalidURL
	}

	parsedUrl.Host = strings.ToLower(parsedUrl.Host)

	return parsedUrl.String(), nil
}

func (srv *Server) HandleServerStartPoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (srv *Server) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	hostNameURL := strings.ToLower(srv.cfg.HostNameURL)
	var requestBody struct {
		OriginalUrl string     `json:"originalUrl"`
		Title       *string    `json:"title"`
		ValidTill   *time.Time `json:"validTill"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			srv.log(r).Warn("request body size", "limit", maxBytesError.Limit, "status", http.StatusRequestEntityTooLarge)
			http.Error(w, "request body size is larger than 8kb", http.StatusRequestEntityTooLarge)
			return
		}
		srv.log(r).Warn("reading input json", "err", err)
		http.Error(w, "Invalid json", http.StatusBadRequest)
		return
	}

	if requestBody.Title != nil && utf8.RuneCountInString(*requestBody.Title) > 64 {
		srv.log(r).Warn("request data - field title", "err", ErrLengthTitle.Error())
		http.Error(w, ErrLengthTitle.Error(), http.StatusBadRequest)
		return
	}
	// проверка валидности URL
	normalizedOriginalUrl, err := normalizeUrl(requestBody.OriginalUrl, hostNameURL)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidURL):
			srv.log(r).Warn("invalid url", "url", requestBody.OriginalUrl)
			http.Error(w, "invalid url", http.StatusBadRequest)
			return
		case errors.Is(err, ErrLengthURL):
			http.Error(w, ErrLengthURL.Error(), http.StatusBadRequest)
			return
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	// проверка на outdate ValidTill дэйттайма
	nowUTC := time.Now().UTC()
	if requestBody.ValidTill != nil {
		validT := *requestBody.ValidTill
		if validT.Before(nowUTC) {
			srv.log(r).Warn("valid till date is outdated", "validTill", validT, "datetime now", nowUTC)
			http.Error(w, "valid till datetime is outdated", http.StatusBadRequest)
			return
		}
	}

	link := model.Link{
		Title:       requestBody.Title,
		OriginalUrl: normalizedOriginalUrl,
		ValidTill:   requestBody.ValidTill,
		CreatedAt:   nowUTC,
	}
	crLink, err := srv.storage.CreateLink(r.Context(), link)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCodeTaken):
			http.Error(w, db.ErrCodeTaken.Error(), http.StatusInternalServerError)
			return
		default:
			srv.log(r).Error("create link", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(crLink)
}

func (srv *Server) HandleRedirectLink(w http.ResponseWriter, r *http.Request) {
	// проверка валидности code
	code := r.PathValue("code")
	if len(code) != 8 {
		srv.log(r).Warn("invalid code length")
		http.Error(w, "link length should equal 8", http.StatusNotFound)
		return
	}
	for _, run := range code {
		if !strings.Contains(base64Alphabet, string(run)) {
			srv.log(r).Warn("inapropriate symbol in url", "symb", string(run))
			http.Error(w, "invalid code", http.StatusNotFound)
			return
		}
	}

	link, err := srv.storage.GetLinkByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, db.ErrLinkNotFound) {
			http.Error(w, db.ErrLinkNotFound.Error(), http.StatusNotFound)
			return
		}
		srv.log(r).Error("get link by code", "err", err.Error())
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// http.HandlerFunc() Смысл ровно один: заставить обычную функцию удовлетворять интерфейсу Handler.
	http.Redirect(w, r, link.OriginalUrl, http.StatusFound)
}

func (srv *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var rBody struct {
		Rusername string  `json:"username"`
		Remail    *string `json:"email"`
		Rpassword string  `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&rBody); err != nil {
		var maxbytesErr *http.MaxBytesError
		if errors.As(err, &maxbytesErr) {
			srv.log(r).Warn("request body size", "limit", maxbytesErr.Limit, "status", http.StatusRequestEntityTooLarge)
			http.Error(w, "", http.StatusRequestEntityTooLarge)
			return
		}
		srv.log(r).Warn("invalid json", "err", err)
		http.Error(w, "invalid input json", http.StatusBadRequest)
		return
	}

	// проверка username
	rBody.Rusername = strings.ToLower(strings.TrimSpace(rBody.Rusername))
	if utf8.RuneCountInString(rBody.Rusername) > 12 || utf8.RuneCountInString(rBody.Rusername) < 3 {
		srv.log(r).Warn("invalid username length", "username", rBody.Rusername)
		http.Error(w, "invalid username length", http.StatusBadRequest)
		return
	}
	bad := strings.IndexFunc(rBody.Rusername, func(r rune) bool {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		return !allowed
	})
	if bad >= 0 {
		srv.log(r).Warn("invalid username characters", "username", rBody.Rusername)
		http.Error(w, "invalid username characters", http.StatusBadRequest)
		return
	}

	// проверка email
	if rBody.Remail != nil {
		email := *rBody.Remail
		email = strings.ToLower(strings.TrimSpace(email))
		if utf8.RuneCountInString(email) > 100 {
			srv.log(r).Warn("invalid email length")
			http.Error(w, "invalid email length", http.StatusBadRequest)
			return
		}

		if email == "" {
			rBody.Remail = nil
		} else {
			validatedEmail, err := mail.ParseAddress(email)
			if err != nil {
				srv.log(r).Warn("Invalid email", "err", err)
				http.Error(w, "invalid user email", http.StatusBadRequest)
				return
			}
			rBody.Remail = &validatedEmail.Address
		}
	}

	// проверка пароля
	if utf8.RuneCountInString(rBody.Rpassword) < 8 || len(rBody.Rpassword) > 72 {
		srv.log(r).Warn("password invalid length")
		http.Error(w, "password invalid length", http.StatusBadRequest)
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(rBody.Rpassword), bcrypt.DefaultCost)
	if err != nil {
		srv.log(r).Error("failed to hash password", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var startUser model.User
	startUser.Username = rBody.Rusername
	startUser.Email = rBody.Remail
	startUser.CreatedAt = time.Now().UTC()
	startUser.PasswordHash = string(passwordHash)

	crUser, err := srv.storage.CreateUser(r.Context(), startUser)
	if err != nil {
		if errors.Is(err, db.ErrUserExists) {
			srv.log(r).Warn("user already exists", "username", rBody.Rusername)
			http.Error(w, db.ErrUserExists.Error(), http.StatusConflict)
			return
		}
		srv.log(r).Error("failed to create user", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(crUser)
}

func (srv *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var rbody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&rbody); err != nil {
		var maxbytesErr *http.MaxBytesError
		if errors.As(err, &maxbytesErr) {
			srv.log(r).Warn("request body size", "limit", maxbytesErr.Limit, "status", http.StatusRequestEntityTooLarge)
			http.Error(w, "", http.StatusRequestEntityTooLarge)
			return
		}
		srv.log(r).Warn("invalid json", "err", err)
		http.Error(w, "invalid input json", http.StatusBadRequest)
		return
	}

	// проверка username
	rbody.Username = strings.ToLower(strings.TrimSpace(rbody.Username))
	if utf8.RuneCountInString(rbody.Username) > 12 || utf8.RuneCountInString(rbody.Username) < 3 {
		srv.log(r).Warn("invalid username length", "username", rbody.Username)
		http.Error(w, "invalid username length", http.StatusBadRequest)
		return
	}
	bad := strings.IndexFunc(rbody.Username, func(r rune) bool {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		return !allowed
	})
	if bad >= 0 {
		srv.log(r).Warn("invalid username characters", "username", rbody.Username)
		http.Error(w, "invalid username characters", http.StatusBadRequest)
		return
	}

	if len(rbody.Password) > 72 {
		srv.log(r).Warn("login: password too long")
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	lgUser, err := srv.storage.GetUserByUsername(r.Context(), rbody.Username)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			// пустой вызов CompareHashAndPassword для того, чтобы замедлить скорость получения ответа: без неё вместо 50мс, было 5мс. Что легко опреедляло где неверный пароль, а где нет пользователя с таким именем
			_ = bcrypt.CompareHashAndPassword(srv.dummyHash, []byte(rbody.Password))

			srv.log(r).Warn("invalid username or password", "username", rbody.Username)
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		srv.log(r).Error("failed to get user by username", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(lgUser.PasswordHash), []byte(rbody.Password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		srv.log(r).Error("corrupted hash")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	token := generateToken()
	h := sha256.Sum256([]byte(token))
	hashToken := hex.EncodeToString(h[:])

	var startToken model.Token
	startToken.UserID = lgUser.ID
	startToken.TokenHash = hashToken
	startToken.CreatedAt = time.Now().UTC()
	startToken.ExpiresAt = time.Now().UTC().Add(srv.cfg.TokenTTL)

	tk, err := srv.storage.CreateToken(r.Context(), startToken)
	if err != nil {
		if errors.Is(err, db.ErrTokenExists) {
			srv.log(r).Warn("token exists")
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var output struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	output.Token = token
	output.ExpiresAt = tk.ExpiresAt

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(output)
}
