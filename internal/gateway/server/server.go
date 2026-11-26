// Package server implements the headless EkkoGateway HTTP server that runs in
// an isolated child process (--gateway) and handles online provider protocols
// and stream proxying.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Handshake is printed as a single JSON line to stdout once the listener is bound.
type Handshake struct {
	Addr string `json:"addr"`
	PID  int    `json:"pid"`
}

// Options configures the gateway server.
type Options struct {
	Port       int
	Token      string
	DataDir    string
	WatchStdin bool
	Out        io.Writer
	Stdin      io.Reader
}

// Run starts the gateway HTTP server and blocks until shutdown (via stdin EOF or ctx).
func Run(ctx context.Context, opts Options) error {
	token := opts.Token
	if token == "" {
		token = os.Getenv("EKKO_GW_TOKEN")
	}
	if token == "" {
		return errors.New("gateway: missing EKKO_GW_TOKEN")
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("gateway listen: %w", err)
	}
	defer ln.Close()

	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = os.Getenv("EKKO_DATA_DIR")
	}
	gw := newGatewayApp(dataDir, ln.Addr().String())

	startedAt := time.Now()
	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"pid":       os.Getpid(),
			"uptimeSec": int64(time.Since(startedAt).Seconds()),
		})
	})
	gw.registerProtected(protectedMux)

	rootMux := http.NewServeMux()
	gw.registerStream(rootMux)
	rootMux.Handle("/", authMiddleware(token, protectedMux))

	srv := &http.Server{
		Handler:           rootMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.WatchStdin {
		go func() {
			buf := make([]byte, 64)
			for {
				if _, err := stdin.Read(buf); err != nil {
					cancel()
					return
				}
			}
		}()
	}

	// Emit handshake line before serving.
	hs, _ := json.Marshal(Handshake{Addr: ln.Addr().String(), PID: os.Getpid()})
	if _, err := fmt.Fprintln(out, string(hs)); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func authMiddleware(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		auth := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if strings.HasPrefix(r.URL.Path, "//") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
