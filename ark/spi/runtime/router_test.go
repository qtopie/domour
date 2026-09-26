package runtime_test

import (
	"context"
	"testing"

	"github.com/qtopie/domour/ark/spi/runtime"
)

func TestDefaultDualPathwayRouter_Routing(t *testing.T) {
	r := runtime.NewDefaultDualPathwayRouter()
	ctx := context.Background()

	// 1. Test Low Road (Fast Path -> Secondary Core)
	fastQueries := []string{"hello", "你好", "ping", "calc 2+2"}
	for _, q := range fastQueries {
		dec, err := r.Route(ctx, "s1", q, nil)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", q, err)
		}
		if dec.TargetCore != runtime.CoreSecondary {
			t.Errorf("expected CoreSecondary for query %q, got %s", q, dec.TargetCore)
		}
		if !dec.FastPath {
			t.Errorf("expected FastPath=true for query %q", q)
		}
	}

	// 2. Test High Road (Deep Path -> Primary Core)
	deepQueries := []string{"请帮我深度思考并设计插件化架构", "refactor the storage layer and analyze dependencies"}
	for _, q := range deepQueries {
		dec, err := r.Route(ctx, "s2", q, nil)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", q, err)
		}
		if dec.TargetCore != runtime.CorePrimary {
			t.Errorf("expected CorePrimary for query %q, got %s", q, dec.TargetCore)
		}
		if dec.FastPath {
			t.Errorf("expected FastPath=false for query %q", q)
		}
	}

	// 3. Test Meta override
	metaOverride := map[string]string{"target_core": "secondary"}
	dec, err := r.Route(ctx, "s3", "very complex task but forced override", metaOverride)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.TargetCore != runtime.CoreSecondary || !dec.FastPath {
		t.Errorf("meta override failed, got %+v", dec)
	}
}
