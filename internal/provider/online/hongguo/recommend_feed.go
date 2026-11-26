package hongguo

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/legacymodel"
	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

const (
	bookmallTabPath    = "/reading/bookapi/bookmall/tab/v1/"
	bookmallChangePath = "/reading/bookapi/bookmall/cell/change/v1/"
	feedSeenCapacity   = 540
)

// seenRing maintains a bounded FIFO set of seen series IDs for filter_ids deduplication.
type seenRing struct {
	cap   int
	order []string
	set   map[string]struct{}
}

func newSeenRing(capacity int) *seenRing {
	if capacity <= 0 {
		capacity = feedSeenCapacity
	}
	return &seenRing{
		cap:   capacity,
		order: make([]string, 0, capacity),
		set:   make(map[string]struct{}, capacity),
	}
}

func (r *seenRing) has(id string) bool {
	if id == "" || r == nil {
		return false
	}
	_, ok := r.set[id]
	return ok
}

func (r *seenRing) add(id string) {
	if id == "" || r == nil {
		return
	}
	if _, ok := r.set[id]; ok {
		return
	}
	if len(r.order) >= r.cap {
		evict := r.order[0]
		copy(r.order, r.order[1:])
		r.order[len(r.order)-1] = id
		delete(r.set, evict)
	} else {
		r.order = append(r.order, id)
	}
	r.set[id] = struct{}{}
}

func (r *seenRing) filterIDs(limit int) string {
	if r == nil || len(r.order) == 0 {
		return ""
	}
	start := 0
	if limit > 0 && len(r.order) > limit {
		start = len(r.order) - limit
	}
	return strings.Join(r.order[start:], ",")
}

type feedCursor struct {
	sessionID   string
	offset      int
	cellID      string
	planID      string
	versionTag  string
	initialized bool
}

type recommendFeedState struct {
	mu     sync.Mutex
	cursor feedCursor
	seen   *seenRing
}

func (c *Client) feedState() *recommendFeedState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.feed == nil {
		c.feed = &recommendFeedState{
			seen: newSeenRing(feedSeenCapacity),
		}
	}
	return c.feed
}

// RandomFeed returns a batch of randomized/recommended dramas from Hongguo's
// App bookmall feed ("随心看"), falling back to a shuffled web Recommend page
// if the App API is unavailable or returns no new items.
func (c *Client) RandomFeed(reset bool, page int) (*model.BrowseResult, error) {
	st := c.feedState()

	st.mu.Lock()
	if reset || page <= 1 {
		st.cursor.initialized = false
	}
	cur := st.cursor
	filterIDs := st.seen.filterIDs(120)
	st.mu.Unlock()

	if c.HTTP != nil {
		items, nextCur, err := c.fetchBookmallFeed(cur, filterIDs)
		if err == nil && len(items) > 0 {
			st.mu.Lock()
			fresh := make([]model.DramaCard, 0, len(items))
			for _, it := range items {
				if st.seen.has(it.SeriesID) {
					continue
				}
				st.seen.add(it.SeriesID)
				fresh = append(fresh, it)
			}
			// If all returned items were already seen, keep the batch anyway so the UI is never blank.
			if len(fresh) == 0 {
				fresh = items
			}
			st.cursor = nextCur
			st.mu.Unlock()

			return &model.BrowseResult{
				Items:      fresh,
				Page:       page,
				TotalPages: page + 1,
				Total:      len(fresh),
				HasMore:    true,
			}, nil
		}
	}

	return c.shuffledRecommendFallback(st, page)
}

func (c *Client) shuffledRecommendFallback(st *recommendFeedState, page int) (*model.BrowseResult, error) {
	// Mix across 真人剧 / 漫剧 / AI 剧 so 随心看 is never limited to a single genre.
	routes := []string{"real-drama", "comic-drama", "ai-drama"}
	type catRes struct {
		items []model.DramaCard
	}
	ch := make(chan catRes, len(routes))
	for _, route := range routes {
		go func(r string) {
			webPage := rand.IntN(5) + 1
			res, _, err := c.CategoryBrowse(r, "", webPage)
			if (err != nil || res == nil || len(res.Items) == 0) && webPage != 1 {
				res, _, err = c.CategoryBrowse(r, "", 1)
			}
			if err != nil || res == nil {
				ch <- catRes{}
				return
			}
			ch <- catRes{items: res.Items}
		}(route)
	}

	var combined []model.DramaCard
	seenLocal := make(map[string]struct{})
	for range routes {
		r := <-ch
		for _, it := range r.items {
			if _, ok := seenLocal[it.SeriesID]; ok {
				continue
			}
			seenLocal[it.SeriesID] = struct{}{}
			combined = append(combined, it)
		}
	}

	if len(combined) == 0 {
		res, err := c.Recommend(1)
		if err != nil {
			return nil, err
		}
		combined = append(combined, res.Items...)
	}

	rand.Shuffle(len(combined), func(i, j int) { combined[i], combined[j] = combined[j], combined[i] })

	st.mu.Lock()
	fresh := make([]model.DramaCard, 0, len(combined))
	for _, it := range combined {
		if !st.seen.has(it.SeriesID) {
			st.seen.add(it.SeriesID)
			fresh = append(fresh, it)
		}
	}
	if len(fresh) == 0 {
		fresh = combined
	}
	st.mu.Unlock()

	return &model.BrowseResult{
		Items:      fresh,
		Page:       page,
		TotalPages: page + 1,
		Total:      len(fresh),
		HasMore:    true,
	}, nil
}

func (c *Client) fetchBookmallFeed(cur feedCursor, filterIDs string) ([]model.DramaCard, feedCursor, error) {
	dev, signer, err := defaultAppSession.get(c.HTTP, true)
	if err != nil {
		return nil, cur, err
	}
	items, nextCur, err := c.fetchBookmallWithIdentity(dev, signer, cur, filterIDs)
	if err != nil && upstream.IsIdentityRejected(err) {
		dev2, signer2, rerr := defaultAppSession.refreshRejected(c.HTTP, dev)
		if rerr != nil {
			return nil, cur, rerr
		}
		return c.fetchBookmallWithIdentity(dev2, signer2, cur, filterIDs)
	}
	return items, nextCur, err
}

func (c *Client) fetchBookmallWithIdentity(dev *RegisteredDevice, signer *AppSigner, cur feedCursor, filterIDs string) ([]model.DramaCard, feedCursor, error) {
	path := bookmallTabPath
	clientReqType := "1"
	if cur.offset > 0 {
		clientReqType = "2"
	}
	extra := [][2]string{
		{"tab_type", "16"},
		{"bottom_tab_type", "7"},
		{"landing_bottom_tab_type", "7"},
		{"offset", strconv.Itoa(cur.offset)},
		{"client_req_type", clientReqType},
	}
	if cur.initialized && cur.cellID != "" {
		path = bookmallChangePath
		extra = [][2]string{
			{"tab_type", "16"},
			{"bottom_tab_type", "7"},
			{"landing_bottom_tab_type", "7"},
			{"cell_id", cur.cellID},
			{"client_template", "7"},
			{"change_type", "0"},
			{"client_req_type", "2"},
			{"session_id", cur.sessionID},
			{"offset", strconv.Itoa(cur.offset)},
		}
		if cur.planID != "" {
			extra = append(extra, [2]string{"plan_id", cur.planID})
		}
		if cur.versionTag != "" {
			extra = append(extra, [2]string{"version_tag", cur.versionTag})
		}
		if filterIDs != "" {
			extra = append(extra, [2]string{"filter_ids", filterIDs})
		}
	} else if cur.sessionID != "" {
		extra = append(extra, [2]string{"session_id", cur.sessionID})
	}

	hosts := appPlayHosts
	if c.Routes != nil {
		if planned := c.Routes.Plan(true); len(planned) > 0 {
			hosts = planned
		}
	}

	var lastErr error
	for _, host := range hosts {
		baseURL := "https://" + host + path
		fullURL, headers, err := signer.SignRequestWithParams(baseURL, dev, extra, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodGet, fullURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("hongguo: bookmall HTTP %d: %s", resp.StatusCode, snippet(raw))
			continue
		}
		items, nextCur, perr := parseBookmallFeed(raw, cur)
		if perr != nil {
			if upstream.IsIdentityRejected(perr) {
				return nil, cur, perr
			}
			lastErr = perr
			continue
		}
		return items, nextCur, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hongguo: bookmall no hosts available")
	}
	return nil, cur, lastErr
}

type bookmallResponse struct {
	Code int `json:"code"`
	Data struct {
		SessionID string             `json:"session_id"`
		Offset    int                `json:"offset"`
		TabItem   []bookmallTabItem  `json:"tab_item"`
		CellData  []bookmallCellNode `json:"cell_data"`
	} `json:"data"`
}

type bookmallTabItem struct {
	CellData []bookmallCellNode `json:"cell_data"`
}

type bookmallCellNode struct {
	CellID     string             `json:"cell_id"`
	PlanID     string             `json:"plan_id"`
	VersionTag string             `json:"version_tag"`
	CellData   []bookmallCellNode `json:"cell_data"`
	VideoData  []VideoData        `json:"video_data"`
	VideoItem  *VideoData         `json:"video_item"`
}

func parseBookmallFeed(raw []byte, prev feedCursor) ([]model.DramaCard, feedCursor, error) {
	var parsed bookmallResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, prev, fmt.Errorf("hongguo: bookmall parse failed: %w", err)
	}
	if parsed.Code != 0 {
		if rej := identityRejection(parsed.Code, "推荐流接口"); rej != nil {
			return nil, prev, rej
		}
		return nil, prev, fmt.Errorf("hongguo: bookmall code=%d", parsed.Code)
	}

	next := prev
	next.initialized = true
	if parsed.Data.SessionID != "" {
		next.sessionID = parsed.Data.SessionID
	}
	if parsed.Data.Offset > 0 {
		next.offset = parsed.Data.Offset
	}

	var items []model.DramaCard
	seenLocal := make(map[string]struct{})

	appendVD := func(vd VideoData) {
		sid := strings.TrimSpace(vd.SeriesID)
		title := strings.TrimSpace(vd.SeriesTitle)
		if sid == "" || title == "" {
			return
		}
		if _, dup := seenLocal[sid]; dup {
			return
		}
		seenLocal[sid] = struct{}{}

		var cats []string
		for _, ct := range vd.CategoryList {
			if ct.Name != "" {
				cats = append(cats, ct.Name)
			}
		}
		rightText := vd.EpisodeRightText
		if rightText == "" && vd.EpisodeCnt > 0 {
			rightText = fmt.Sprintf("共%d集", vd.EpisodeCnt)
		}
		items = append(items, model.DramaCard{
			Source:     "hongguo",
			SeriesID:   sid,
			Title:      title,
			Cover:      vd.SeriesCover,
			Intro:      vd.SeriesIntro,
			EpisodeCnt: vd.EpisodeCnt,
			RightText:  rightText,
			Metric:     vd.HotScoreData.Text,
			Categories: cats,
		})
	}

	var walk func(node bookmallCellNode)
	walk = func(node bookmallCellNode) {
		if node.CellID != "" && next.cellID == "" {
			next.cellID = node.CellID
		}
		if node.PlanID != "" && next.planID == "" {
			next.planID = node.PlanID
		}
		if node.VersionTag != "" && next.versionTag == "" {
			next.versionTag = node.VersionTag
		}
		for _, vd := range node.VideoData {
			appendVD(vd)
		}
		if node.VideoItem != nil {
			appendVD(*node.VideoItem)
		}
		for _, child := range node.CellData {
			walk(child)
		}
	}

	for _, tab := range parsed.Data.TabItem {
		for _, cell := range tab.CellData {
			walk(cell)
		}
	}
	for _, cell := range parsed.Data.CellData {
		walk(cell)
	}

	return items, next, nil
}
