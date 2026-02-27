package handler

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	iofs "io/fs"
	"log/slog"
	"net/http"
	"strconv"

	"golang.org/x/time/rate"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
	mw "github.com/mark-c-hall/degrees-of-separation/internal/middleware"
)

const searchLimit = 15

type pathResult struct {
	Steps      []graph.PathStep
	Degrees    int
	SameActor  bool
}

type Handler struct {
	db      *graph.Driver
	tmpl    *template.Template
	logger  *slog.Logger
	handler http.Handler
}

func commify(n int) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func NewHandler(db *graph.Driver, fs embed.FS, cfg config.ServerConfig, logger *slog.Logger) (*Handler, error) {
	funcs := template.FuncMap{"commify": commify}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(fs, "templates/base.html", "templates/fragments/*.html")
	if err != nil {
		return nil, err
	}

	staticFS, err := iofs.Sub(fs, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to create static sub-filesystem: %w", err)
	}

	h := &Handler{db: db, tmpl: tmpl, logger: logger}

	mux := http.NewServeMux()
	addRoutes(mux, h, staticFS)

	var wrapped http.Handler = mux
	wrapped = mw.Timeout(cfg.RequestTimeout)(wrapped)
	wrapped = mw.RateLimit(rate.Limit(cfg.RateLimitPerSec), cfg.RateBurst, logger)(wrapped)
	wrapped = mw.Recovery(logger)(wrapped)
	wrapped = mw.Logging(logger)(wrapped)
	wrapped = mw.CORS(cfg.CORSOrigin)(wrapped)

	h.handler = wrapped
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}

func addRoutes(mux *http.ServeMux, h *Handler, staticFS iofs.FS) {
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", h.indexHandler)
	mux.HandleFunc("/search", h.searchHandler)
	mux.HandleFunc("/degrees", h.degreesHandler)
	mux.HandleFunc("/stats", h.statsHandler)
	mux.HandleFunc("/healthz", h.healthHandler)
	mux.HandleFunc("/readyz", h.readyHandler)
}

func (h *Handler) indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.renderFragment(w, "base.html", nil)
}

func (h *Handler) searchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		h.renderFragment(w, "search.html", nil)
		return
	}

	actors, err := h.db.SearchActors(r.Context(), query, searchLimit)
	if err != nil {
		h.logger.Error("failed to search actors", "query", query, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.renderFragment(w, "search.html", actors)
}

func (h *Handler) degreesHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("a") == "" || r.URL.Query().Get("b") == "" {
		h.renderFragment(w, "degrees.html", nil)
		return
	}

	idA, err := strconv.Atoi(r.URL.Query().Get("a"))
	if err != nil {
		h.logger.Error("invalid actor id", "a", r.URL.Query().Get("a"), "err", err)
		http.Error(w, "invalid actor id", http.StatusBadRequest)
		return
	}
	idB, err := strconv.Atoi(r.URL.Query().Get("b"))
	if err != nil {
		h.logger.Error("invalid actor id", "b", r.URL.Query().Get("b"), "err", err)
		http.Error(w, "invalid actor id", http.StatusBadRequest)
		return
	}

	if idA == idB {
		h.renderFragment(w, "degrees.html", pathResult{Degrees: 0, SameActor: true})
		return
	}

	pathStep, err := h.db.ShortestPath(r.Context(), idA, idB)
	if err != nil {
		h.logger.Error("failed to get shortest path", "a", idA, "b", idB, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	deg := 0
	if len(pathStep) > 1 {
		deg = (len(pathStep) - 1) / 2
	}
	h.renderFragment(w, "degrees.html", pathResult{Steps: pathStep, Degrees: deg})
}

func (h *Handler) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.GetStats(r.Context())
	if err != nil {
		h.logger.Error("failed to get stats", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.renderFragment(w, "stats.html", stats)
}

func (h *Handler) renderFragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		h.logger.Error("failed to render fragment", "template", name, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (h *Handler) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) readyHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.db.VerifyConnectivity(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
