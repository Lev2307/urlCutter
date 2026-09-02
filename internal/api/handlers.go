package api

import (
	"encoding/json"
	"net/http"
)

func (srv *Server) HandleServerStartPoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (srv *Server) HandleCreateLink(w http.ResponseWriter, r *http.Request) {
	type Body struct {
		Name string `json:"name"`
	}
	var b Body
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "Invalid json", http.StatusBadRequest)
		return
	}
	generatedCode, err := srv.storage.CreateLink(r.Context(), b.Name)
	if err != nil {
		srv.logger.Error("create link", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(generatedCode)
}
