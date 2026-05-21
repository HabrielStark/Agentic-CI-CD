package diagnose

import (
	"context"

	"github.com/reproforge/reproforge/internal/mlrank"
	"github.com/reproforge/reproforge/internal/store"
)

// Rerank applies an optional ML model trained on the local SQLite history
// to the rule-based confidence map. When the store has no data for the
// repo this is a no-op and the original map is returned.
//
// Alpha controls the blend: 1.0 → pure rule-based, 0.0 → pure ML, default
// 0.6 keeps the rule signal dominant which matches NFR-008 (transparency).
func Rerank(ctx context.Context, s *store.Store, repo string, in map[string]float64, evidence []string, alpha float64) (map[string]float64, error) {
	if s == nil {
		return in, nil
	}
	model, err := mlrank.TrainFromStore(ctx, s, repo)
	if err != nil {
		return in, err
	}
	if model.IsEmpty() {
		return in, nil
	}
	if alpha <= 0 {
		alpha = 0.6
	}
	return model.Rerank(in, evidence, alpha), nil
}
