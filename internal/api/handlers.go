package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	db "github.com/Lev2307/urlCutter/internal/db"
	model "github.com/Lev2307/urlCutter/internal/model"
)

var ErrInvalidURL = errors.New("invalid url")
var ErrLengthURL = errors.New("link length is gt 2048 symbols")

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
		srv.logger.Error("reading input json", "err", err)
		http.Error(w, "Invalid json", http.StatusBadRequest)
		return
	}
	// проверка валидности URL
	normalizedOriginalUrl, err := normalizeUrl(requestBody.OriginalUrl, hostNameURL)

	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidURL):
			srv.logger.Error("invalid url", "url", requestBody.OriginalUrl)
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
			srv.logger.Error("valid till date is outdated", "validTill", validT, "datetime now", nowUTC)
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
			srv.logger.Error("create link", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(crLink)
}
