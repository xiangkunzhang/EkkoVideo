package hongguo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

// useStableTestDeviceDir points the device-credential cache at a stable
// location outside the repository.
//
// It must not be t.TempDir(): every run would then register a brand-new
// device, and frequent registration is exactly the pattern Doc 4 §1.1
// identifies as the trigger for 红果's risk control (device_id: 0).
//
// It also must not stay at the default relative path, because the default
// resolves against the process working directory - which for `go test` is the
// package directory, dropping a live credential into the source tree on every
// run.
func useStableTestDeviceDir(t *testing.T) {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = filepath.Join(os.TempDir(), "duanju-desktop-test")
	}
	dir := filepath.Join(base, "duanju-desktop-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir test device dir: %v", err)
	}
	DeviceConfigDir = dir
}

// requireLiveOK turns an unreachable platform into a skip.
//
// The live tests exist to report that a platform changed its protocol. An
// outage is not that, and must not be allowed to train us to ignore red.
func requireLiveOK(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if upstream.Unreachable(err) {
		t.Skipf("%s: 红果上游当前不可达，跳过 live 校验（%v）", label, err)
	}
	t.Fatalf("%s: %v", label, err)
}

func TestAppResolveEp6Live(t *testing.T) {
	useStableTestDeviceDir(t)
	c := New()
	st, err := c.AppResolve("7706514796681628697", "720p")
	requireLiveOK(t, "AppResolve", err)
	t.Logf("Resolved ep6: def=%s codec=%s %dx%d hasKey=%v url=%s",
		st.Definition, st.Codec, st.Width, st.Height, len(st.Key) == 16, st.URL[:min(80, len(st.URL))])
}

func TestSearchAndResolveEp6Live(t *testing.T) {
	useStableTestDeviceDir(t)
	c := New()
	items, err := c.Search("假少爷回村")
	requireLiveOK(t, "Search", err)
	if len(items) == 0 {
		t.Fatalf("Search returned no items")
	}
	sid := items[0].VideoData.SeriesID
	d, err := c.Detail(sid)
	requireLiveOK(t, "Detail", err)
	if len(d.VidList) < 6 {
		t.Fatalf("Detail(%s) expected >=6 episodes, got %d", sid, len(d.VidList))
	}
	t.Logf("Series=%s Title=%s Ep6VID=%s", sid, d.SeriesName, d.VidList[5])
	st, err := c.AppResolve(d.VidList[5], "720p")
	requireLiveOK(t, "AppResolve", err)
	t.Logf("AppResolve ep6 OK: def=%s codec=%s url=%s", st.Definition, st.Codec, st.URL[:min(80, len(st.URL))])
	res, err := c.Resolve(sid, d.VidList[5], "720p")
	requireLiveOK(t, "Resolve", err)
	t.Logf("Resolve ep6 OK: full=%v dash=%v", res.Full, res.Dash != nil)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
