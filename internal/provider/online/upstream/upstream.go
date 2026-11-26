// Package upstream classifies errors returned by the remote short-drama
// platforms, so that a platform outage can be told apart from a regression in
// our own code.
//
// Why this distinction matters: Doc 4 §4.2 keeps the live tests on purpose as
// an early warning that a platform changed its protocol. If every outage also
// turns those tests red, the signal stops carrying information and stops
// being trusted - which is exactly how a useful sentinel becomes noise.
package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// Category represents a fine-grained classification of an upstream or client error.
type Category string

const (
	CategoryNone      Category = "none"
	CategoryDNS       Category = "dns"       // DNS 域名解析失败（DNS 挂了）
	CategoryNetwork   Category = "network"   // 网络超时 / 远端断开 / 连接被拒
	CategoryTLS       Category = "tls"       // TLS/SSL 握手或证书异常
	CategorySignature Category = "signature" // 签名校验被拒 / 设备凭据失效 / 401/403 鉴权失败（签名挂了）
	CategoryServer    Category = "server"    // 上游服务端 5xx / 节点返回空包
	CategoryProtocol  Category = "protocol"  // 协议格式改变 / JSON 校验失败 / 业务错误
)

// IsDNS reports whether err was caused specifically by a DNS resolution failure.
func IsDNS(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return IsDNS(urlErr.Err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "lookup ") ||
		strings.Contains(msg, "server misbehaving") ||
		strings.Contains(msg, "name resolution")
}

// IsTLS reports whether err was caused specifically by a TLS/certificate error.
func IsTLS(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return IsTLS(urlErr.Err)
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}
	var unknownAuth x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return true
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return true
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls:") ||
		strings.Contains(msg, "certificate signed by unknown authority") ||
		strings.Contains(msg, "certificate has expired")
}

// IsSignature reports whether err was caused by signature rejection, device
// registration rejection, or auth token expiration ("签名挂了").
func IsSignature(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "device_register rejected") ||
		strings.Contains(msg, "device_id: 0") ||
		strings.Contains(msg, "empty device_id") ||
		strings.Contains(msg, "illegal_access") ||
		strings.Contains(msg, "signature rejected") ||
		strings.Contains(msg, "signature error") ||
		strings.Contains(msg, "sign error") ||
		strings.Contains(msg, "sign failed") ||
		strings.Contains(msg, "invalid signature") ||
		strings.Contains(msg, "token expired") ||
		strings.Contains(msg, "token invalid") ||
		strings.Contains(msg, "app resolve http 401") ||
		strings.Contains(msg, "app resolve http 403") ||
		strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403") ||
		strings.Contains(msg, "code 110") ||
		strings.Contains(msg, "code 104") ||
		strings.Contains(msg, "illegal access")
}

// IsServer reports whether err was caused by an upstream 5xx or empty response.
func IsServer(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "empty response") ||
		strings.Contains(msg, "http 500") ||
		strings.Contains(msg, "http 502") ||
		strings.Contains(msg, "http 503") ||
		strings.Contains(msg, "http 504") ||
		strings.Contains(msg, "app resolve http 5")
}

// IsNetwork reports whether err was caused by network timeouts, dropped connections,
// or unreachable sockets (excluding pure DNS and TLS errors).
func IsNetwork(err error) bool {
	if err == nil || IsDNS(err) || IsTLS(err) {
		return false
	}
	return Unreachable(err)
}

// IsThrottled reports whether err is caused by upstream rate limiting or risk
// control rather than by our request being wrong.
//
// These conditions surface as an HTTP 200/502 with a business code like
// "signature_unavailable", so Unreachable (which only covers transport
// failures) does not match them. A live check that trips this should skip,
// not fail: it says nothing about whether our code is correct.
func IsThrottled(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "risk control") ||
		strings.Contains(msg, "signature_unavailable") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "access denied")
}

// Classify categorizes err into a fine-grained Category.
func Classify(err error) Category {
	if err == nil {
		return CategoryNone
	}
	if IsDNS(err) {
		return CategoryDNS
	}
	if IsTLS(err) {
		return CategoryTLS
	}
	if IsSignature(err) {
		return CategorySignature
	}
	if IsServer(err) {
		return CategoryServer
	}
	if IsNetwork(err) {
		return CategoryNetwork
	}
	return CategoryProtocol
}

// Diagnosis produces a clear, human-readable Chinese diagnosis of what went wrong.
func Diagnosis(err error) string {
	switch Classify(err) {
	case CategoryDNS:
		return "DNS 域名解析失败，请检查网络连接或本地 DNS 配置"
	case CategoryNetwork:
		return "网络连接异常或请求超时，请检查网络连接"
	case CategoryTLS:
		return "TLS/SSL 证书校验异常，请检查本地系统时间与证书"
	case CategorySignature:
		return "签名校验被拒或设备凭据失效（签名挂了），可能需要重新注册设备或更新签名"
	case CategoryServer:
		return "上游平台服务暂时不可用或返回空数据（节点冷却中）"
	case CategoryProtocol:
		return "平台接口结构发生变更或业务逻辑错误"
	default:
		return ""
	}
}

// Unreachable reports whether err was caused by the remote platform being
// unreachable at the transport level: DNS, TCP, TLS, timeout or connection
// reset.
//
// It deliberately returns false for protocol-level failures. A changed
// response shape, a rejected signature, an unexpected status code or an
// unparsable body all mean the platform changed - those must fail loudly.
func Unreachable(err error) bool {
	if err == nil {
		return false
	}

	// Transport failures surface as *url.Error wrapping the real cause.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return Unreachable(urlErr.Err)
	}

	// Timeouts, temporary network failures and *net.OpError.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// DNS resolution failures.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Connection closed before a complete response arrived.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Socket level failures.
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}

	// Deadline exceeded, whether from a client timeout or a context.
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// TLS certificate problems: an expired or untrusted chain is still an
	// upstream condition rather than a protocol change.
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}
	var unknownAuth x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return true
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return true
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return true
	}

	return false
}
