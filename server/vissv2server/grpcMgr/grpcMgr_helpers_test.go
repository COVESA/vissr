/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Coverage tests for the pure-helper grpcMgr functions beyond the mutex
* and pool-allocation tests from PR #122. Specifically:
*
*   - getSubscriptionId — JSON value extractor for the "subscriptionId"
*     field on an unsubscribe response.
*   - extractClientId — string parser for the "clientId:xyz" form used
*     in subscription-kill messages.
*
* The streaming RPC entry points (GetRequest, SetRequest,
* UnsubscribeRequest, SubscribeRequest) and the manager-loop drivers
* (initGrpcServer, GrpcMgrInit, RemoveRoutingForwardResponse,
* updateRoutingList) drive a gRPC server / stream and need a peer
* client to exercise. They are tested via runtest.sh integration.
**/
package grpcMgr

import (
	"testing"
)

// TestGetSubscriptionId_ValidJSON pulls the subscriptionId field out
// of an unsubscribe response.
func TestGetSubscriptionId_ValidJSON(t *testing.T) {
	resp := `{"action":"unsubscribe","subscriptionId":"sub-123","RouterId":"0?1"}`
	if got := getSubscriptionId(resp); got != "sub-123" {
		t.Fatalf("getSubscriptionId = %q; want sub-123", got)
	}
}

// TestGetSubscriptionId_MissingField returns empty (not panic).
func TestGetSubscriptionId_MissingField(t *testing.T) {
	resp := `{"action":"unsubscribe"}`
	if got := getSubscriptionId(resp); got != "" {
		t.Fatalf("getSubscriptionId on missing field = %q; want \"\"", got)
	}
}

// TestGetSubscriptionId_MalformedJSON returns empty (not panic).
func TestGetSubscriptionId_MalformedJSON(t *testing.T) {
	cases := []string{
		"",
		"not json",
		"{",
		`{"subscriptionId":}`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getSubscriptionId panicked on %q: %v", in, r)
				}
			}()
			if got := getSubscriptionId(in); got != "" {
				t.Fatalf("getSubscriptionId(%q) = %q; want \"\"", in, got)
			}
		})
	}
}

// TestExtractClientId_Valid parses "clientId:123" forms.
func TestExtractClientId_Valid(t *testing.T) {
	cases := map[string]int{
		"clientId:42":  42,
		"prefix:0":     0,
		"abc:7":        7,
		"x:9999":       9999,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := extractClientId(in); got != want {
				t.Fatalf("extractClientId(%q) = %d; want %d", in, got, want)
			}
		})
	}
}

// TestExtractClientId_Malformed must not panic on missing delimiter or
// non-numeric tail.
func TestExtractClientId_Malformed(t *testing.T) {
	cases := []string{
		"no-delimiter",
		":42",       // empty key
		"x:",        // empty value
		"x:not-num", // non-numeric
		"",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("extractClientId panicked on %q: %v", in, r)
				}
			}()
			_ = extractClientId(in)
		})
	}
}

// TODO(testing): the streaming RPC handlers are not unit-testable
// without a full gRPC server fixture. The simplest path forward is
// to extract the per-request body of GetRequest/SetRequest/
// UnsubscribeRequest into a function that operates on plain Go
// values and have those tests target the extracted helper.
