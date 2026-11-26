package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"
)

// timeoutErr is a transport-level timeout that is neither DNS nor TLS.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestDiagnosticCode pins the short codes the UI branches on.
//
// The code is a stable contract, the Chinese diagnosis text is not: rewording
// the text must not change any code, and a new failure mode must not silently
// reuse an existing code.
func TestDiagnosticCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"preparation stage", WithStage(StagePreparation, timeoutErr{}), "timeout=preparation"},
		{"prefetch stage", WithStage(StagePrefetchWait, timeoutErr{}), "timeout=prefetch_wait"},
		{"transport stage", WithStage(StageTransport, timeoutErr{}), "timeout=transport"},
		{"deadline stage", WithStage(StagePreparation, context.DeadlineExceeded), "timeout=preparation"},
		// A stage on a non-timeout error must not turn it into a timeout:
		// the real cause has to survive.
		{"stage on 404", WithStage(StagePreparation, errors.New("HTTP 404: not found")), "http=404"},
		{"stage on protocol error", WithStage(StagePreparation, errors.New("unexpected json shape")), "protocol"},
		{"identity rejected", &IdentityRejectedError{Code: 10001, Msg: "denied"}, "identity=rejected code=10001"},
		{"identity rejected wrapped", fmt.Errorf("outer: %w", &IdentityRejectedError{Code: 110001}), "identity=rejected code=110001"},
		{"identity wins over stage", WithStage(StagePreparation, &IdentityRejectedError{Code: 10002}), "identity=rejected code=10002"},
		{"context deadline", context.DeadlineExceeded, "timeout=deadline"},
		{"os deadline", os.ErrDeadlineExceeded, "timeout=deadline"},
		{
			"dns",
			&url.Error{Op: "Get", URL: "https://x/", Err: &net.DNSError{Err: "no such host"}},
			"dns",
		},
		{"tls", errors.New("tls: failed to verify certificate"), "tls"},
		{"signature rejected", errors.New("app resolve http 403 from api3-normal"), "signature"},
		{"server status", errors.New("app resolve http 500 from api3-normal"), "http=500"},
		{"transport timeout", &url.Error{Op: "Post", URL: "https://x/", Err: timeoutErr{}}, "timeout=transport"},
		{"protocol change", errors.New("unexpected json shape"), "protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DiagnosticCode(tc.err); got != tc.want {
				t.Fatalf("DiagnosticCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestStageErrorUnwrap keeps errors.Is / errors.As working through the stage
// wrapper — otherwise the stage would hide the real cause.
func TestStageErrorUnwrap(t *testing.T) {
	inner := errors.New("boom")
	err := WithStage(StagePreparation, inner)
	if !errors.Is(err, inner) {
		t.Fatal("stage wrapper hid the inner error")
	}
	if WithStage(StagePreparation, nil) != nil {
		t.Fatal("WithStage(nil) must stay nil")
	}
}

// TestIsIdentityRejectedOnlyMatchesItsOwnType guards against a broad matcher
// sweeping unrelated errors into the identity-recovery path.
func TestIsIdentityRejectedOnlyMatchesItsOwnType(t *testing.T) {
	if IsIdentityRejected(errors.New("app resolve http 403 from api3")) {
		t.Fatal("an HTTP 403 must not be treated as an identity rejection")
	}
	if !IsIdentityRejected(&IdentityRejectedError{Code: 10001}) {
		t.Fatal("identity rejection not detected")
	}
	if got := IdentityRejectedCode(&IdentityRejectedError{Code: 10002}); got != 10002 {
		t.Fatalf("IdentityRejectedCode = %d, want 10002", got)
	}
	if got := IdentityRejectedCode(errors.New("x")); got != 0 {
		t.Fatalf("IdentityRejectedCode of unrelated error = %d, want 0", got)
	}
}
