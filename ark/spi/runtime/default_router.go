package runtime

import (
	"context"
	"strings"
)

// DefaultDualPathwayRouter implements the Router interface based on a biological dual-pathway heuristic.
type DefaultDualPathwayRouter struct {
	fastKeywords []string
	deepKeywords []string
}

// NewDefaultDualPathwayRouter creates a new instance of the default dual-pathway router.
func NewDefaultDualPathwayRouter() *DefaultDualPathwayRouter {
	return &DefaultDualPathwayRouter{
		fastKeywords: []string{
			"hello", "hi", "ping", "你好", "在吗", "时间", "time", "date",
			"calc", "calculator", "verify:", "test",
		},
		deepKeywords: []string{
			"plan", "nested", "think", "analyze", "architect", "refactor",
			"debug", "investigate", "设计", "重构", "分析", "规划", "深度思考",
		},
	}
}

// Route evaluates request complexity and decides between Low Road (Secondary Core) and High Road (Primary Core).
func (r *DefaultDualPathwayRouter) Route(ctx context.Context, sessionID, message string, meta map[string]string) (RouteDecision, error) {
	lowerMsg := strings.ToLower(strings.TrimSpace(message))

	// 1. Force override via metadata if provided
	if meta != nil {
		if target := meta["target_core"]; target != "" {
			if CoreTarget(target) == CoreSecondary {
				return RouteDecision{
					TargetCore: CoreSecondary,
					FastPath:   true,
					Mode:       "simple",
					PreferTags: []string{"flash", "lite", "local"},
					Meta:       meta,
				}, nil
			}
			if CoreTarget(target) == CorePrimary {
				return RouteDecision{
					TargetCore: CorePrimary,
					FastPath:   false,
					Mode:       "react",
					PreferTags: []string{"pro", "deep"},
					Meta:       meta,
				}, nil
			}
		}
	}

	// 2. Check explicitly for deep thinking triggers (High Road)
	for _, kw := range r.deepKeywords {
		if strings.Contains(lowerMsg, kw) {
			return RouteDecision{
				TargetCore:   CorePrimary,
				FastPath:     false,
				Mode:         "react",
				RequiredTags: []string{"reasoning"},
				PreferTags:   []string{"deep", "pro"},
				Meta:         meta,
			}, nil
		}
	}

	// 3. Check for fast reflexive queries (Low Road)
	isShortQuery := len([]rune(lowerMsg)) < 25
	hasFastKeyword := false
	for _, kw := range r.fastKeywords {
		if strings.Contains(lowerMsg, kw) {
			hasFastKeyword = true
			break
		}
	}

	if isShortQuery && hasFastKeyword {
		return RouteDecision{
			TargetCore: CoreSecondary,
			FastPath:   true,
			Mode:       "simple",
			PreferTags: []string{"flash", "lite", "local"},
			Meta:       meta,
		}, nil
	}

	// 4. Default: Standard Primary Core ReAct loop
	return RouteDecision{
		TargetCore: CorePrimary,
		FastPath:   false,
		Mode:       "react",
		PreferTags: []string{"balanced"},
		Meta:       meta,
	}, nil
}
