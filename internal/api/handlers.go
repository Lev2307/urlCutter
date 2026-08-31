package api

import (
	"net/http"
	"time"
)

func (srv *Server) HandleServerStartPoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (srv *Server) HandleSleepThreeSecond(w http.ResponseWriter, r *http.Request) {
	time.Sleep(15 * time.Second)
}
