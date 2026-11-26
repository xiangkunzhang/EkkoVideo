package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
)

// TimeoutStage 标识一次超时发生在哪个阶段。
//
// 分阶段的意义：同一个"超时"，发生在取流准备阶段和发生在分片下载阶段，
// 排查方向完全不同。合成一句话的"网络超时"会让人无从下手。
type TimeoutStage string

const (
	StagePreparation  TimeoutStage = "preparation"   // 取流准备：解析播放地址、拿签名
	StagePrefetchWait TimeoutStage = "prefetch_wait" // 预加载等待：后台预算耗尽
	StageTransport    TimeoutStage = "transport"     // 传输层：分片下载、Range 读取
	StageDeadline     TimeoutStage = "deadline"      // 上下文截止
)

// StageError 给错误附加一个阶段标记。
type StageError struct {
	Stage TimeoutStage
	Err   error
}

func (e *StageError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Stage)
	}
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

// Unwrap exposes the wrapped error to errors.Is / errors.As.
func (e *StageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WithStage 给 err 套上阶段标记。err 为 nil 时返回 nil。
func WithStage(stage TimeoutStage, err error) error {
	if err == nil {
		return nil
	}
	return &StageError{Stage: stage, Err: err}
}

// IdentityRejectedError 表示上游明确拒绝了当前设备身份。
//
// 这是"设备身份过期"的判定依据，和 HTTP 401/403 不是一回事：
// 401/403 是请求层的鉴权问题，重新签名也没用；而身份被拒是上游把这台设备
// 判死了，换一个设备身份重试才有意义。
type IdentityRejectedError struct {
	Code int
	Msg  string
}

func (e *IdentityRejectedError) Error() string {
	return fmt.Sprintf("设备身份被上游拒绝（业务码 %d）: %s", e.Code, e.Msg)
}

// IsIdentityRejected reports whether err means the device identity was rejected.
func IsIdentityRejected(err error) bool {
	var rej *IdentityRejectedError
	return errors.As(err, &rej)
}

// IdentityRejectedCode returns the upstream business code behind the rejection,
// or 0 when err is not an identity rejection.
func IdentityRejectedCode(err error) int {
	var rej *IdentityRejectedError
	if errors.As(err, &rej) {
		return rej.Code
	}
	return 0
}

var httpStatusRe = regexp.MustCompile(`(?i)\bhttp\s+(\d{3})\b`)

// DiagnosticCode compresses a failure into a short machine-readable code.
//
// The code is a stable contract: the UI may branch on it. The Chinese
// diagnosis text is free to change at any time, the code is not.
//
//	timeout=preparation   取流准备阶段超时
//	timeout=prefetch_wait 预加载预算耗尽
//	timeout=transport     传输层超时
//	timeout=deadline      上下文截止
//	identity=rejected     设备身份被上游拒绝
//	signature             签名/凭据被拒
//	http=<code>           上游返回了具体 HTTP 状态
//	dns / tls / server / network / protocol
func DiagnosticCode(err error) string {
	if err == nil {
		return ""
	}

	// An identity rejection is the most specific answer we have: the device
	// is dead and the only way out is a fresh identity.
	var rej *IdentityRejectedError
	if errors.As(err, &rej) {
		return fmt.Sprintf("identity=rejected code=%d", rej.Code)
	}

	// A stage marker only turns into a "timeout=" code when the wrapped error
	// really is a timeout. Otherwise it would label a 404 as a timeout and
	// hide the real cause.
	var stage *StageError
	if errors.As(err, &stage) && isTimeout(stage.Err) {
		return "timeout=" + string(stage.Stage)
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout=deadline"
	}
	if IsDNS(err) {
		return "dns"
	}
	if IsTLS(err) {
		return "tls"
	}
	// 401/403 belong to the credential family; "signature" tells the UI more
	// than "http=403" would.
	if IsSignature(err) {
		return "signature"
	}
	if code, ok := httpStatus(err); ok {
		return "http=" + strconv.Itoa(code)
	}
	if isTimeout(err) {
		return "timeout=transport"
	}
	if IsServer(err) {
		return "server"
	}
	if IsNetwork(err) {
		return "network"
	}
	return "protocol"
}

// httpStatus digs the HTTP status code out of an error message.
func httpStatus(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	m := httpStatusRe.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return 0, false
	}
	code, err := strconv.Atoi(m[1])
	if err != nil || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

// isTimeout reports whether err is a transport-level timeout.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
