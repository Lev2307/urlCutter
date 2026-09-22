package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	db "github.com/Lev2307/urlCutter/internal/db"
	model "github.com/Lev2307/urlCutter/internal/model"
)

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var ErrInvalidURL = errors.New("invalid url")
var ErrLengthURL = errors.New("link length is gt 2048 symbols")
var ErrLengthTitle = errors.New("title length is gt 64 symbols")

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
