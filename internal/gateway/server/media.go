package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xiangkunzhang/Velan/internal/provider/online/hongguo"
)

const moovCacheTTL = 10 * time.Minute

type moovContext struct {
	headBytes  []byte
	fileSize   int64
	headerSize int64
	samples    []hongguo.CencSampleRange
	mainURL    string
	rawKey     []byte
	createdAt  time.Time
}

type moovLoad struct {
	done chan struct{}
	ctx  *moovContext
	err  error
}

func (g *gatewayApp) handleCENCStream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sid := strings.TrimSpace(q.Get("series_id"))
	ep, _ := strconv.Atoi(q.Get("ep"))
	def := q.Get("definition")
	if def == "" {
		def = "720p"
	}
	if sid == "" || ep <= 0 {
		http.Error(w, "missing series_id or ep", http.StatusBadRequest)
		return
	}

	mc, err := g.getMoovContext(sid, ep, def)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	rangeHdr := r.Header.Get("Range")
	if rangeHdr == "" {
		rangeHdr = "bytes=0-"
	}
	start, end, err := parseHTTPRange(rangeHdr, mc.fileSize)
	if err != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, mc.fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	if start < mc.headerSize {
		hi := min(end+1, mc.headerSize)
		_, _ = w.Write(mc.headBytes[start:hi])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if end < mc.headerSize {
			return
		}
	}

	upStart := max(start, mc.headerSize)
	upEnd := min(end, mc.fileSize-1)
	if upStart > upEnd {
		return
	}

	const chunkBytes = 1 << 20
	for cs := upStart; cs <= upEnd; cs += chunkBytes {
		ce := min(cs+chunkBytes-1, upEnd)
		wantLen := ce - cs + 1
		var rawBlock []byte
		var fetchErr error
		for attempt := 0; attempt < 3; attempt++ {
			rawBlock, _, fetchErr = g.fetchRange(r.Context(), mc.mainURL, cs, ce)
			if fetchErr == nil && int64(len(rawBlock)) == wantLen {
				break
			}
			if isCtxErr(fetchErr) {
				return
			}
			time.Sleep(time.Duration(attempt+1) * 150 * time.Millisecond)
		}
		if fetchErr != nil || int64(len(rawBlock)) != wantLen {
			if !isCtxErr(fetchErr) {
				g.invalidateMoov(sid, ep, def)
			}
			return
		}
		_ = hongguo.DecryptBlock(rawBlock, cs, mc.samples, mc.rawKey)
		if _, err := w.Write(rawBlock); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (g *gatewayApp) getMoovContext(sid string, ep int, def string) (*moovContext, error) {
	key := fmt.Sprintf("%s|%d|%s", sid, ep, def)
	g.moovMu.Lock()
	if c := g.moovCache[key]; c != nil && time.Since(c.createdAt) < moovCacheTTL {
		g.moovMu.Unlock()
		return c, nil
	}
	if ld := g.moovLoading[key]; ld != nil {
		g.moovMu.Unlock()
		<-ld.done
		return ld.ctx, ld.err
	}
	ld := &moovLoad{done: make(chan struct{})}
	g.moovLoading[key] = ld
	g.moovMu.Unlock()

	defer func() {
		g.moovMu.Lock()
		delete(g.moovLoading, key)
		g.moovMu.Unlock()
		close(ld.done)
	}()

	ctx, err := g.buildMoovContext(sid, ep, def)
	if err != nil {
		ld.err = err
		return nil, err
	}
	ld.ctx = ctx
	g.moovMu.Lock()
	g.moovCache[key] = ctx
	g.moovMu.Unlock()
	return ctx, nil
}

func (g *gatewayApp) invalidateMoov(sid string, ep int, def string) {
	key := fmt.Sprintf("%s|%d|%s", sid, ep, def)
	g.moovMu.Lock()
	delete(g.moovCache, key)
	g.moovMu.Unlock()
}

func (g *gatewayApp) buildMoovContext(sid string, ep int, def string) (*moovContext, error) {
	d, err := g.online.Hongguo.Detail(sid)
	if err != nil {
		return nil, err
	}
	if ep > len(d.VidList) {
		return nil, fmt.Errorf("episode %d out of range", ep)
	}
	st, err := g.online.Hongguo.Resolve(d.SeriesID, d.VidList[ep-1], def)
	if err != nil {
		return nil, err
	}
	if st.Dash == nil || st.Dash.MainURL == "" || len(st.RawKey) != 16 {
		return nil, fmt.Errorf("missing cenc dash stream or 16-byte key")
	}

	headCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var headBytes []byte
	var fileSize int64
	var moovEnd int
	for _, win := range []int{256 * 1024, 512 * 1024, 1024 * 1024, 1536 * 1024} {
		hb, fs, err := g.fetchRange(headCtx, st.Dash.MainURL, 0, int64(win-1))
		if err != nil {
			return nil, err
		}
		headBytes, fileSize = hb, fs
		if ms, me := findMoovBounds(headBytes); ms >= 0 && me <= len(headBytes) {
			moovEnd = me
			break
		}
	}
	if moovEnd == 0 {
		return nil, fmt.Errorf("moov not found")
	}

	samples, err := hongguo.ParseMoovSamples(headBytes)
	if err != nil || len(samples) == 0 {
		return nil, fmt.Errorf("parse sample table: %w", err)
	}
	ms, me := findMoovBounds(headBytes)
	hongguo.StripCencInHeader(headBytes[ms+8 : me])

	return &moovContext{
		headBytes:  headBytes,
		fileSize:   fileSize,
		headerSize: int64(me),
		samples:    samples,
		mainURL:    st.Dash.MainURL,
		rawKey:     st.RawKey,
		createdAt:  time.Now(),
	}, nil
}

func (g *gatewayApp) fetchRange(ctx context.Context, rawURL string, start, end int64) ([]byte, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := g.mediaClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if idx := strings.LastIndexByte(cr, '/'); idx >= 0 {
			total, _ = strconv.ParseInt(cr[idx+1:], 10, 64)
		}
	}
	if total == 0 {
		total = int64(len(data))
	}
	return data, total, nil
}

func findMoovBounds(buf []byte) (int, int) {
	off := 0
	for off+8 <= len(buf) {
		size := int(binary.BigEndian.Uint32(buf[off : off+4]))
		if size < 8 {
			break
		}
		if string(buf[off+4:off+8]) == "moov" {
			return off, off + size
		}
		off += size
	}
	return -1, -1
}

func parseHTTPRange(hdr string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(hdr, "bytes=") {
		return 0, 0, fmt.Errorf("bad range")
	}
	spec := strings.TrimPrefix(hdr, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad range")
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= fileSize {
		return 0, 0, fmt.Errorf("bad start")
	}
	end := fileSize - 1
	if parts[1] != "" {
		if e, err := strconv.ParseInt(parts[1], 10, 64); err == nil && e < fileSize {
			end = e
		}
	}
	if end < start {
		return 0, 0, fmt.Errorf("end < start")
	}
	return start, end, nil
}
