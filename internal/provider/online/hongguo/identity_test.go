package hongguo

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

// roundTripFunc turns a function into an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonBody(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// newTestSession builds an isolated session: the device cache lives in a
// temporary directory and the registration call is replaceable, so no test
// ever touches the real app_device.json or the upstream registration API.
func newTestSession(t *testing.T, register func(*http.Client) (*RegisteredDevice, error)) *appSession {
	t.Helper()
	oldDir := DeviceConfigDir
	DeviceConfigDir = t.TempDir()
	t.Cleanup(func() { DeviceConfigDir = oldDir })
	return &appSession{register: register}
}

// TestIdentityRejectionCodes pins which upstream business codes mean
// "this device identity is dead".
//
// Only these codes trigger a re-registration. Everything else - including a
// generic HTTP 403 - is a request-level failure that a fresh device identity
// cannot fix.
func TestIdentityRejectionCodes(t *testing.T) {
	for _, code := range []int{10001, 10002, 110001} {
		err := identityRejection(code, "denied")
		if err == nil {
			t.Fatalf("code %d should be an identity rejection", code)
		}
		if !upstream.IsIdentityRejected(err) {
			t.Fatalf("code %d not recognised as identity rejection", code)
		}
		if got := upstream.IdentityRejectedCode(err); got != code {
			t.Fatalf("IdentityRejectedCode = %d, want %d", got, code)
		}
	}
	for _, code := range []int{0, 1, 20001, 104} {
		if err := identityRejection(code, "other"); err != nil {
			t.Fatalf("code %d must not be an identity rejection: %v", code, err)
		}
	}
}

// TestRefreshRejectedSwapsIdentity checks the core recovery: the rejected
// identity is dropped from memory and from disk, and a fresh one takes its
// place.
func TestRefreshRejectedSwapsIdentity(t *testing.T) {
	var calls int32
	s := newTestSession(t, func(*http.Client) (*RegisteredDevice, error) {
		atomic.AddInt32(&calls, 1)
		return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2"}, nil
	})
	rejected := &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1"}
	s.dev = rejected
	s.signer = NewAppSigner()
	saveCachedDevice(rejected)

	got, _, err := s.refreshRejected(http.DefaultClient, rejected)
	if err != nil {
		t.Fatalf("refreshRejected: %v", err)
	}
	if got.DeviceID != "dev-2" {
		t.Fatalf("device = %q, want dev-2", got.DeviceID)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("register called %d times, want 1", calls)
	}
	cached := loadCachedDevice()
	if cached == nil || cached.DeviceID != "dev-2" {
		t.Fatalf("replacement identity not persisted: %+v", cached)
	}
}

// TestRefreshRejectedReusesConcurrentRefresh makes sure a second caller does
// not register another device: the first refresh already fixed the slot.
func TestRefreshRejectedReusesConcurrentRefresh(t *testing.T) {
	var calls int32
	s := newTestSession(t, func(*http.Client) (*RegisteredDevice, error) {
		atomic.AddInt32(&calls, 1)
		return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2"}, nil
	})
	rejected := &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1"}
	s.dev = rejected
	s.signer = NewAppSigner()

	if _, _, err := s.refreshRejected(http.DefaultClient, rejected); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	got, _, err := s.refreshRejected(http.DefaultClient, rejected)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got.DeviceID != "dev-2" {
		t.Fatalf("device = %q, want dev-2", got.DeviceID)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("register called %d times, want 1 (the refresh must be shared)", calls)
	}
}

// TestRefreshRejectedFailureEntersCooldown checks that a failed recovery does
// not turn into a registration storm.
func TestRefreshRejectedFailureEntersCooldown(t *testing.T) {
	var calls int32
	s := newTestSession(t, func(*http.Client) (*RegisteredDevice, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("register failed")
	})
	rejected := &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1"}
	s.dev = rejected
	s.signer = NewAppSigner()

	if _, _, err := s.refreshRejected(http.DefaultClient, rejected); err == nil {
		t.Fatal("expected the failed recovery to surface an error")
	}
	_, _, err := s.get(http.DefaultClient, false)
	if err == nil || !strings.Contains(err.Error(), "稍后重试") {
		t.Fatalf("expected a cooldown error, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("register called %d times, want 1 (cooldown must block re-registration)", calls)
	}
}

// TestRejectReplacementDropsIdentity checks that a replacement which is also
// rejected never survives on disk.
func TestRejectReplacementDropsIdentity(t *testing.T) {
	s := newTestSession(t, func(*http.Client) (*RegisteredDevice, error) {
		return &RegisteredDevice{DeviceID: "dev-3", InstallID: "inst-3"}, nil
	})
	dev := &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2"}
	s.dev = dev
	s.signer = NewAppSigner()
	saveCachedDevice(dev)

	s.rejectReplacement(dev)

	if s.dev != nil {
		t.Fatal("the rejected replacement must be dropped from memory")
	}
	if cached := loadCachedDevice(); cached != nil {
		t.Fatalf("the rejected replacement must not stay on disk: %+v", cached)
	}
	if _, _, err := s.get(http.DefaultClient, false); err == nil {
		t.Fatal("a cooldown must block registration right after a rejection")
	}
}

// TestSpeculativeGetNeverRegisters pins the prefetch rule: a background
// request may reuse an existing identity but never creates one.
func TestSpeculativeGetNeverRegisters(t *testing.T) {
	var calls int32
	s := newTestSession(t, func(*http.Client) (*RegisteredDevice, error) {
		atomic.AddInt32(&calls, 1)
		return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2"}, nil
	})
	if _, _, err := s.get(http.DefaultClient, true); err == nil {
		t.Fatal("a speculative get must not register a device")
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("register called %d times, want 0", calls)
	}
}

// TestAppResolveRecoversOnceFromIdentityRejection walks the whole path: the
// upstream rejects the identity, the client registers a new one, retries
// exactly once and succeeds.
func TestAppResolveRecoversOnceFromIdentityRejection(t *testing.T) {
	oldSession := defaultAppSession
	oldDir := DeviceConfigDir
	DeviceConfigDir = t.TempDir()
	t.Cleanup(func() { defaultAppSession = oldSession; DeviceConfigDir = oldDir })

	var attempts int32
	var registers int32
	defaultAppSession = &appSession{
		dev:    &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1", CDID: "cdid"},
		signer: NewAppSigner(),
		register: func(*http.Client) (*RegisteredDevice, error) {
			atomic.AddInt32(&registers, 1)
			return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2", CDID: "cdid"}, nil
		},
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return jsonBody(`{"code":10001,"message":"设备被拒绝"}`), nil
		}
		// 成功响应按当前主路径（v5）的形态给出：data 先按业务类型分组，
		// 再按剧集 id 分组，最后才是 video_model。
		return jsonBody(`{"code":0,"data":{"1004":{"vid-1":{"video_model":{"video_list":[{"main_url":"https://cdn.example/e.mp4","video_meta":{"definition":"720p","codec_type":"h264","vwidth":1280,"vheight":720}}]}}}}}`), nil
	})}

	c := &Client{HTTP: client}
	st, err := c.AppResolve("vid-1", "720p")
	if err != nil {
		t.Fatalf("AppResolve: %v", err)
	}
	if st.URL != "https://cdn.example/e.mp4" {
		t.Fatalf("url = %q", st.URL)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("upstream requests = %d, want 2 (one rejection + one retry)", got)
	}
	if got := atomic.LoadInt32(&registers); got != 1 {
		t.Fatalf("registrations = %d, want 1", got)
	}
}

// TestAppResolveGivesUpAfterSecondRejection checks the ceiling: the recovery
// happens once, the rejected replacement is not persisted and the session
// enters a cooldown.
func TestAppResolveGivesUpAfterSecondRejection(t *testing.T) {
	oldSession := defaultAppSession
	oldDir := DeviceConfigDir
	DeviceConfigDir = t.TempDir()
	t.Cleanup(func() { defaultAppSession = oldSession; DeviceConfigDir = oldDir })

	var attempts int32
	defaultAppSession = &appSession{
		dev:    &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1", CDID: "cdid"},
		signer: NewAppSigner(),
		register: func(*http.Client) (*RegisteredDevice, error) {
			return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2", CDID: "cdid"}, nil
		},
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return jsonBody(`{"code":110001,"message":"设备被拒绝"}`), nil
	})}

	c := &Client{HTTP: client}
	_, err := c.AppResolve("vid-1", "720p")
	if err == nil {
		t.Fatal("expected the resolve to fail")
	}
	if !upstream.IsIdentityRejected(err) {
		t.Fatalf("expected an identity rejection, got %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("upstream requests = %d, want 2 (no endless retry)", got)
	}
	if cached := loadCachedDevice(); cached != nil {
		t.Fatalf("a rejected identity must not stay on disk: %+v", cached)
	}
}

// TestGenericFailureDoesNotReRegister locks in the anti-registration-storm
// behaviour: a plain failure (network / node / protocol) must NOT throw away
// the device identity.
//
// Re-registering on every failure turns one flaky request into a registration
// storm, and the upstream throttles the registration endpoint — after which
// every subsequent playback fails too.
func TestGenericFailureDoesNotReRegister(t *testing.T) {
	oldSession := defaultAppSession
	oldDir := DeviceConfigDir
	DeviceConfigDir = t.TempDir()
	t.Cleanup(func() { defaultAppSession = oldSession; DeviceConfigDir = oldDir })

	var registers int32
	device := &RegisteredDevice{DeviceID: "dev-1", InstallID: "inst-1", CDID: "cdid"}
	defaultAppSession = &appSession{
		dev:    device,
		signer: NewAppSigner(),
		register: func(*http.Client) (*RegisteredDevice, error) {
			atomic.AddInt32(&registers, 1)
			return &RegisteredDevice{DeviceID: "dev-2", InstallID: "inst-2"}, nil
		},
	}
	// 身份已经落盘：普通失败也不能把它删掉
	saveCachedDevice(device)

	// 上游返回空包 —— 这是普通失败，不是身份拒绝
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})}

	c := &Client{HTTP: client}
	if _, err := c.AppResolve("vid-1", "720p"); err == nil {
		t.Fatal("空响应应当导致取流失败")
	}
	if got := atomic.LoadInt32(&registers); got != 0 {
		t.Fatalf("普通失败触发了 %d 次设备注册（应为 0）—— 这就是注册风暴的来源", got)
	}
	if loadCachedDevice() == nil {
		t.Fatal("普通失败把设备身份删掉了")
	}
	if defaultAppSession.dev == nil {
		t.Fatal("普通失败把内存里的身份清掉了")
	}
}
