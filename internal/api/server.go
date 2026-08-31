package api

import (
	"net/http"
)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (srv *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", srv.HandleServerStartPoint)
	mux.HandleFunc("GET /slow", srv.HandleSleepThreeSecond)
	return mux
}
