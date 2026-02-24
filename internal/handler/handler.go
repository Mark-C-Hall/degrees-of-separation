package handler

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/time/rate"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
	mw "github.com/mark-c-hall/degrees-of-separation/internal/middleware"
)

type Handler struct {
	db      *graph.Driver
	fs      embed.FS
	handler http.Handler
}

func NewHandler(db *graph.Driver, fs embed.FS, cfg config.ServerConfig, logger *slog.Logger) (*Handler, error) {
	mux := http.NewServeMux()
	addRoutes(mux)

	var h http.Handler = mux
	h = mw.Timeout(cfg.RequestTimeout)(h)
	h = mw.RateLimit(rate.Limit(cfg.RateLimitPerSec), cfg.RateBurst, logger)(h)
	h = mw.Recovery(logger)(h)
	h = mw.Logging(logger)(h)
	h = mw.CORS(cfg.CORSOrigin)(h)

	return &Handler{db: db, fs: fs, handler: h}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}

func addRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", helloHandler)
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<h1>Hello, %s</h1>", r.RequestURI)
}
