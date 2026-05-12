package domain

import (
	"context"
	"errors"
	"fmt"
)

// Apply binds the given domain using params.Strategy (or the auto-recommended
// strategy from Detect()). All progress is reported via r.
//
// Currently only StrategyOwnPort is implemented; other strategies return a
// helpful "not yet implemented" error pointing the user to the manual README
// recipe.
func Apply(ctx context.Context, params BindParams, r Reporter) (*BindResult, error) {
	if r == nil {
		r = NopReporter{}
	}
	if params.Domain == "" {
		return nil, errors.New("domain is required")
	}

	r.Emit(Event{Kind: EventStep, StepID: "detect", Title: "Детект профиля хоста"})
	profile := Detect(params.PublicIP)

	strategy, err := chooseStrategy(profile, params.Strategy)
	if err != nil {
		return nil, err
	}
	r.Emit(Event{Kind: EventLog, Message: fmt.Sprintf("Выбрана стратегия: %s", strategy), Strategy: strategy})

	switch strategy {
	case StrategyOwnPort:
		return ApplyOwnPort(ctx, profile, params, r)
	case StrategyClean:
		return nil, errors.New("стратегия clean (Tier 1) пока не реализована в коде. " +
			"Используйте --strategy own-port или ручную инструкцию из server-install/README.md")
	case StrategySNIMux:
		return nil, errors.New("стратегия sni-mux (Tier 3) пока не реализована в коде. " +
			"Используйте --strategy own-port (рекомендуется для хостов с 3x-ui)")
	default:
		return nil, fmt.Errorf("неизвестная стратегия: %q", strategy)
	}
}

// chooseStrategy returns the strategy to use given the host profile and the
// user's requested strategy. "auto" or "" picks the first Recommended strategy.
func chooseStrategy(profile *HostProfile, requested string) (string, error) {
	if requested != "" && requested != "auto" {
		// User explicitly named a strategy. Verify it's available.
		for _, s := range profile.Strategies {
			if s.Name == requested {
				if !s.Available {
					return "", fmt.Errorf("стратегия %q недоступна на этом хосте: %s", requested, s.UnavailableReason)
				}
				return requested, nil
			}
		}
		return "", fmt.Errorf("неизвестная стратегия: %q (известны: clean, own-port, sni-mux)", requested)
	}
	// Auto: pick first Recommended.
	for _, s := range profile.Strategies {
		if s.Recommended {
			return s.Name, nil
		}
	}
	// Fallback: pick first Available.
	for _, s := range profile.Strategies {
		if s.Available {
			return s.Name, nil
		}
	}
	return "", errors.New("ни одна из стратегий не доступна на этом хосте")
}
