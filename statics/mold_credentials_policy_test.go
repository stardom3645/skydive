package statics

import (
	"strings"
	"testing"
)

func TestEmbeddedPolicyProtectsMoldCredentials(t *testing.T) {
	policy, err := Asset("rbac/policy.csv")
	if err != nil {
		t.Fatalf("failed to load embedded RBAC policy: %v", err)
	}
	for _, rule := range []string{
		"p, admin, mold-credentials, read, allow",
		"p, admin, mold-credentials, write, allow",
		"p, guest, mold-credentials, read, deny",
		"p, guest, mold-credentials, write, deny",
	} {
		if !strings.Contains(string(policy), rule) {
			t.Fatalf("embedded RBAC policy is missing %q", rule)
		}
	}
}
