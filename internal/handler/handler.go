package handler

import (
	"embed"
	"fmt"
	"net/http"

	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
)

type Handler struct {
	db  *graph.Driver
	fs  embed.FS
	mux *http.ServeMux
}

func NewHandler(db *graph.Driver, fs embed.FS) (*Handler, error) {
	m := http.NewServeMux()
	addRoutes(m)
	return &Handler{db: db, fs: fs, mux: m}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func addRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", helloHandler)
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<h1>Hello, %s</h1>", r.RequestURI)
}
