package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiangkunzhang/EkkoVideo/internal/core"
	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online"
)

type gatewayApp struct {
	online      *online.Manager
	streamToken string
	addr        string
	mediaClient *http.Client

	moovMu      sync.Mutex
	moovCache   map[string]*moovContext
	moovLoading map[string]*moovLoad
}

func newGatewayApp(dataDir, addr string) *gatewayApp {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &gatewayApp{
		online:      online.NewManager(dataDir),
		streamToken: hex.EncodeToString(b),
		addr:        addr,
		mediaClient: &http.Client{Timeout: 25 * time.Second},
		moovCache:   make(map[string]*moovContext),
		moovLoading: make(map[string]*moovLoad),
	}
}

func (g *gatewayApp) registerProtected(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"providers": g.online.Providers()})
	})
	mux.HandleFunc("GET /v1/sections", func(w http.ResponseWriter, r *http.Request) {
		src := r.URL.Query().Get("source")
		writeJSON(w, http.StatusOK, g.online.Sections(src))
	})
	mux.HandleFunc("GET /v1/browse", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		page, err := g.online.Browse(q.Get("source"), core.Query{
			Section:  q.Get("section"),
			Category: q.Get("category"),
			Sort:     q.Get("sort"),
			Cursor:   q.Get("cursor"),
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		page, err := g.online.Search(q.Get("source"), q.Get("q"), q.Get("cursor"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /v1/detail", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		d, err := g.online.Detail(q.Get("source"), q.Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, d)
	})
	mux.HandleFunc("GET /v1/related", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items, err := g.online.Related(q.Get("source"), q.Get("id"), q.Get("title"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("GET /v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ep, _ := strconv.Atoi(q.Get("ep"))
		ps, err := g.online.ResolvePlay(g.addr, g.streamToken, q.Get("source"), q.Get("series_id"), q.Get("vid"), ep, q.Get("quality"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ps)
	})
	mux.HandleFunc("GET /v1/danmaku", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items, err := g.online.Danmaku(q.Get("source"), q.Get("series_id"), q.Get("ep_id"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

func (g *gatewayApp) registerStream(mux *http.ServeMux) {
	prefix := "/s/" + g.streamToken
	mux.HandleFunc("GET "+prefix+"/stream.mp4", g.handleCENCStream)
}

// ParseHandshakeData is a helper for JSON decoding in tests/clients.
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func isCtxErr(err error) bool {
	if err == nil {
		return false
	}
	return err == context.Canceled || err == context.DeadlineExceeded || strings.Contains(err.Error(), "context canceled")
}
