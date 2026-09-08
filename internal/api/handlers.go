package api

import (
	"encoding/json"
	"net/http"
	"time"

	model "github.com/Lev2307/urlCutter/internal/model"
)

func (srv *Server) HandleServerStartPoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (srv *Server) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	var link model.Link
	if err := json.NewDecoder(r.Body).Decode(&link); err != nil {
		srv.logger.Error("read json", "err", err)
		http.Error(w, "Invalid json", http.StatusBadRequest)
		return
	}
	link.CreatedAt = time.Now()
	crLink, err := srv.storage.CreateLink(r.Context(), link)
	if err != nil {
		srv.logger.Error("create link", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(crLink)
}
