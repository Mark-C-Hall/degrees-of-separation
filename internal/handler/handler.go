package handler

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"

	"golang.org/x/time/rate"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
	mw "github.com/mark-c-hall/degrees-of-separation/internal/middleware"
)

const searchLimit = 15

type Handler struct {
	db      *graph.Driver
	fs      embed.FS
	tmpl    *template.Template
	logger  *slog.Logger
	handler http.Handler
}

func NewHandler(db *graph.Driver, fs embed.FS, cfg config.ServerConfig, logger *slog.Logger) (*Handler, error) {
	tmpl, err := template.ParseFS(fs, "templates/base.html", "templates/fragments/*.html")
	if err != nil {
		return nil, err
	}

	h := &Handler{db: db, fs: fs, tmpl: tmpl, logger: logger}

	mux := http.NewServeMux()
	addRoutes(mux, h)

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

func addRoutes(mux *http.ServeMux, h *Handler) {
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
	if err := h.tmpl.ExecuteTemplate(w, "base.html", nil); err != nil {
		h.logger.Error("failed to render index", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
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

	pathStep, err := h.db.ShortestPath(r.Context(), idA, idB)
	if err != nil {
		h.logger.Error("failed to get shortest path", "a", idA, "b", idB, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.renderFragment(w, "degrees.html", pathStep)
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
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error("failed to render fragment", "template", name, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
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
