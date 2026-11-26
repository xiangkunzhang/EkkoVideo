package hongguo

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

// 上游用业务码表达"这台设备不被接受"。
//
// 只有这几个码判定身份失效。泛化的 HTTP 401/403 不在其列：那是请求层的
// 鉴权问题，重新注册设备解决不了，盲目重注册反而会招来注册接口的风控。
var identityRejectedCodes = map[int]bool{
	10001:  true,
	10002:  true,
	110001: true,
}

// identityCooldown 是身份恢复失败后的冷却窗口。
//
// 重新注册设备是上游会做风控的动作。冷却期内不再重复注册，避免把
// "上游拒绝了这台设备"变成"上游把我们整个出口 IP 限了"。
const identityCooldown = 30 * time.Second

// identityRejection 把上游业务码翻译成身份失效错误，非身份码返回 nil。
func identityRejection(code int, msg string) error {
	if !identityRejectedCodes[code] {
		return nil
	}
	return &upstream.IdentityRejectedError{Code: code, Msg: msg}
}

// sameDevice 判断两个设备身份是不是同一个。
func sameDevice(a, b *RegisteredDevice) bool {
	if a == nil || b == nil {
		return false
	}
	return a.DeviceID == b.DeviceID && a.InstallID == b.InstallID
}

// refreshRejected 丢弃被拒的身份，换一个新身份。
//
// 并发场景：多个请求同时撞上身份被拒时，只有第一个真正去注册；后到的请求
// 发现当前身份已经不是被拒的那一个，直接复用已经刷新的结果，不重复注册。
func (s *appSession) refreshRejected(httpClient *http.Client, rejected *RegisteredDevice) (*RegisteredDevice, *AppSigner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 已经被别人换掉了 —— 复用，不重复注册。
	if s.dev != nil && !sameDevice(s.dev, rejected) {
		return s.dev, s.signer, nil
	}
	if cached := loadCachedDevice(); cached != nil && !sameDevice(cached, rejected) {
		s.dev = cached
		s.signer = NewAppSigner()
		return s.dev, s.signer, nil
	}

	// 清掉被拒的身份：它已经没用了，而且不能继续留在磁盘上。
	s.dev = nil
	s.signer = nil
	_ = os.Remove(appDeviceCachePath())

	dev, err := s.registerDevice(httpClient)
	if err != nil {
		s.retryAfter = time.Now().Add(identityCooldown)
		return nil, nil, fmt.Errorf("播放连接恢复失败，请稍后重试: %w", err)
	}
	saveCachedDevice(dev)
	s.dev = dev
	s.signer = NewAppSigner()
	return s.dev, s.signer, nil
}

// rejectReplacement 新的身份同样被拒：不落盘它，并进入冷却。
func (s *appSession) rejectReplacement(dev *RegisteredDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev != nil && sameDevice(s.dev, dev) {
		s.dev = nil
		s.signer = nil
		_ = os.Remove(appDeviceCachePath())
	}
	s.retryAfter = time.Now().Add(identityCooldown)
}
