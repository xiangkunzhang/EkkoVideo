package hongguo

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

// PlaybackRoutes tracks health per CDN host so we can avoid repeatedly
// hammering hosts that are temporarily down while still letting them recover
// on their own. Mirrors the approach hongguoTV uses for Phoenix shortplay.
//
// Two health states matter for callers:
//   - blockedUntil > now → host is in cooldown; try another
//   - blockedUntil == 0 → host is healthy; prefer hosts with recent success
type PlaybackRoutes struct {
	mu    sync.Mutex
	hosts []routeHealth
}

type routeHealth struct {
	host         string
	successAt    time.Time // last known successful response
	blockedUntil time.Time // cooldown deadline; zero = healthy
	empty        bool      // true if last success was 200 with empty body (temporary cold state)
	failStreak   int       // consecutive failures since last success
}

// DefaultHongguoHosts mirrors the host list hongguoTV maintains in
// PlaybackRoutes.kt — these are the Phoenix shortplay API endpoints that
// serve AppResolve. The signer's host pool already knows these names.
var DefaultHongguoHosts = []string{
	"api5-normal-sinfonlineb.fqnovel.com",
	"api5-normal-lf.fqnovel.com",
	"api5-normal-sinfonline.fqnovel.com",
	"api5-normal-lf2.fqnovel.com",
	"api5-normal-hl.fqnovel.com",
	"api5-normal-ll.fqnovel.com",
}

func NewPlaybackRoutes(hosts []string) *PlaybackRoutes {
	if len(hosts) == 0 {
		hosts = DefaultHongguoHosts
	}
	p := &PlaybackRoutes{hosts: make([]routeHealth, len(hosts))}
	for i, h := range hosts {
		p.hosts[i].host = h
	}
	return p
}

// Plan returns hosts ordered by preference: healthy ones first (most recent
// success first), blocked ones last. Speculative callers only get healthy
// hosts; retry callers include blocked hosts for when the primary pool is
// exhausted.
func (p *PlaybackRoutes) Plan(speculative bool) []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()

	healthy := []routeHealth{}
	blocked := []routeHealth{}
	for _, h := range p.hosts {
		if !h.blockedUntil.IsZero() && h.blockedUntil.After(now) {
			blocked = append(blocked, h)
		} else {
			healthy = append(healthy, h)
		}
	}
	// Healthy hosts with recent success first.
	sortBySuccessDesc(healthy)

	out := make([]string, 0, len(p.hosts))
	for _, h := range healthy {
		out = append(out, h.host)
	}
	if !speculative {
		for _, h := range blocked {
			out = append(out, h.host)
		}
	}
	return out
}

// OnSuccess records a healthy response for host. Empty-body responses from a
// cold start are still marked success — the host reached us, it just had no
// data to hand off yet.
func (p *PlaybackRoutes) OnSuccess(host string, empty bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for i := range p.hosts {
		if p.hosts[i].host == host {
			p.hosts[i].successAt = now
			p.hosts[i].blockedUntil = time.Time{}
			p.hosts[i].failStreak = 0
			p.hosts[i].empty = empty
			return
		}
	}
}

// OnFailure cools down a failing host. A single 5xx/timeout earns a short
// 5s cooldown; two empties in a row (DNS ok but empty body → host is not yet
// provisioned for this episode) earn a longer 30s cooldown.
func (p *PlaybackRoutes) OnFailure(host string, temporary bool, empty bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for i := range p.hosts {
		if p.hosts[i].host == host {
			p.hosts[i].failStreak++
			switch {
			case empty:
				p.hosts[i].blockedUntil = now.Add(30 * time.Second)
			case temporary:
				p.hosts[i].blockedUntil = now.Add(5 * time.Second)
			default:
				// Hard failure (4xx, signer rejected our request): back off
				// aggressively to avoid hammering the same endpoint.
				failStreak := p.hosts[i].failStreak
				if failStreak > 6 {
					failStreak = 6
				}
				p.hosts[i].blockedUntil = now.Add(time.Duration(failStreak) * 15 * time.Second)
			}
			return
		}
	}
}

// Pick returns one host, respecting cooldown. If all healthy hosts are
// exhausted it falls back to the least-recently-blocked one so we keep
// retrying rather than giving up entirely.
func (p *PlaybackRoutes) Pick() string {
	plan := p.Plan(false)
	if len(plan) == 0 {
		return ""
	}
	return plan[0]
}

// HostFromURL extracts the hostname from a full URL (used by callers that
// receive resolved CDN or API URLs and want to feed them into OnSuccess /
// OnFailure tracking).
func HostFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

func sortBySuccessDesc(hs []routeHealth) {
	// simple insertion sort — host count is tiny (< 10)
	for i := 1; i < len(hs); i++ {
		k := hs[i]
		j := i - 1
		for j >= 0 && hs[j].successAt.Before(k.successAt) {
			hs[j+1] = hs[j]
			j--
		}
		hs[j+1] = k
	}
}
