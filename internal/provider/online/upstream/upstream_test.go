package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestUnreachable pins the classification in both directions.
//
// The dangerous direction is over-classification: a protocol change or a
// business error must NOT be reported as "upstream down", otherwise a real
// regression gets silently skipped and the live test stops being a sentinel.
func TestUnreachable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "connection refused",
			err:  &url.Error{Op: "Post", URL: "https://x/", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "connection reset",
			err:  &url.Error{Op: "Post", URL: "https://x/", Err: syscall.ECONNRESET},
			want: true,
		},
		{
			name: "eof mid response",
			err:  &url.Error{Op: "Post", URL: "https://x/", Err: io.EOF},
			want: true,
		},
		{
			name: "unexpected eof",
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "dns failure",
			err:  &url.Error{Op: "Post", URL: "https://x/", Err: &net.DNSError{Err: "no such host", Name: "x"}},
			want: true,
		},
		{
			name: "timeout",
			err:  &url.Error{Op: "Post", URL: "https://x/", Err: os.ErrDeadlineExceeded},
			want: true,
		},
		{
			name: "context deadline",
			err:  fmt.Errorf("build mp4: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			name: "net op error",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "protocol change - json parse failure",
			err:  fmt.Errorf("singleVideoModel json: %w", errors.New("invalid character '<'")),
			want: false,
		},
		{
			name: "business error",
			err:  fmt.Errorf("singleVideoModel code 110: %s", "ILLEGAL_ACCESS"),
			want: false,
		},
		{
			name: "device rejected",
			err:  errors.New(`device_register rejected: {"device_id": 0}`),
			want: false,
		},
		{
			name: "empty response",
			err:  errors.New("empty response from novelquickapp.com"),
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Unreachable(tc.err); got != tc.want {
				t.Fatalf("Unreachable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassificationFineGrained verifies that "签名挂了" vs "DNS 挂了" are
// reliably and distinctly classified.
func TestClassificationFineGrained(t *testing.T) {
	// 1. DNS 挂了
	dnsErr1 := &url.Error{Op: "Get", URL: "https://api5.fqnovel.com", Err: &net.DNSError{Err: "no such host", Name: "api5.fqnovel.com"}}
	dnsErr2 := errors.New(`dial tcp: lookup api5-normal-sinfonlineb.fqnovel.com: no such host`)
	if !IsDNS(dnsErr1) || !IsDNS(dnsErr2) {
		t.Fatalf("expected IsDNS to be true for dnsErr1/2")
	}
	if IsSignature(dnsErr1) || IsSignature(dnsErr2) {
		t.Fatalf("IsSignature should be false for DNS error")
	}
	if Classify(dnsErr1) != CategoryDNS || Classify(dnsErr2) != CategoryDNS {
		t.Fatalf("Classify failed for DNS error")
	}

	// 2. 签名挂了 / 设备失效
	sigErr1 := errors.New(`device_register rejected: {"device_id": 0}`)
	sigErr2 := errors.New(`singleVideoModel code 110: ILLEGAL_ACCESS`)
	sigErr3 := errors.New(`app resolve http 403 from api5-normal-lf.fqnovel.com`)
	sigErr4 := errors.New(`signature rejected by upstream`)
	for i, err := range []error{sigErr1, sigErr2, sigErr3, sigErr4} {
		if !IsSignature(err) {
			t.Fatalf("expected IsSignature to be true for sigErr%d (%v)", i+1, err)
		}
		if IsDNS(err) {
			t.Fatalf("IsDNS should be false for sigErr%d (%v)", i+1, err)
		}
		if Classify(err) != CategorySignature {
			t.Fatalf("Classify should be CategorySignature for sigErr%d, got %v", i+1, Classify(err))
		}
	}

	// 3. 诊断文案验证
	if diag := Diagnosis(dnsErr1); !strings.Contains(diag, "DNS") {
		t.Fatalf("unexpected DNS diagnosis: %s", diag)
	}
	if diag := Diagnosis(sigErr1); !strings.Contains(diag, "签名") {
		t.Fatalf("unexpected signature diagnosis: %s", diag)
	}
}
