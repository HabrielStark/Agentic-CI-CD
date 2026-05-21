// Package mlrank implements FR-030: an optional, locally trained Naive Bayes
// classifier that re-ranks the rule-based diagnosis candidates using the
// repo's run history (internal/store).
//
// The model is intentionally tiny and explainable — features are extracted
// from a normalised set of evidence tokens (lowercased, alphanumerics
// only). Training data is the repo's own historical (fingerprint →
// category) pairs from runs.db. The Rerank() function returns calibrated
// posterior probabilities per category which the caller may blend with its
// own rule-based confidence. Calibration is monotonic: a category never
// loses its rule-based confidence floor.
package mlrank

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/reproforge/reproforge/internal/store"
)

// Model is a trained Naive Bayes model.
type Model struct {
	Classes      []string                      // ordered class labels
	ClassPrior   map[string]float64            // log P(c)
	WordLikely   map[string]map[string]float64 // c -> w -> log P(w|c)
	Vocabulary   map[string]int                // global vocab
	TotalDocs    int
	TotalClasses int
}

// IsEmpty reports whether the model has any training data.
func (m *Model) IsEmpty() bool { return m == nil || m.TotalDocs == 0 }

// TrainFromStore reads the runs table for repo and trains a Naive Bayes
// model keyed by category. Each historical run becomes a "document" of
// tokens taken from its fingerprint, repo, job, workflow and provider.
func TrainFromStore(ctx context.Context, s *store.Store, repo string) (*Model, error) {
	if s == nil {
		return nil, errors.New("mlrank: store is nil")
	}
	stats, err := s.Aggregate(ctx, repo)
	if err != nil {
		return nil, err
	}
	if stats.TotalRuns == 0 {
		return &Model{}, nil
	}

	corpus := map[string][][]string{}
	for class := range stats.Categories {
		recent, err := s.HistoryByCategory(ctx, repo, class, 200)
		if err != nil {
			continue
		}
		for _, r := range recent {
			doc := tokenise(r.Fingerprint, r.Job, r.Workflow, r.Provider, r.Repo)
			corpus[class] = append(corpus[class], doc)
		}
	}
	return TrainFromCorpus(corpus), nil
}

// TrainFromCorpus is exposed for tests and CLI tooling.
func TrainFromCorpus(corpus map[string][][]string) *Model {
	m := &Model{
		Classes:    make([]string, 0, len(corpus)),
		ClassPrior: map[string]float64{},
		WordLikely: map[string]map[string]float64{},
		Vocabulary: map[string]int{},
	}
	totalDocs := 0
	classCounts := map[string]int{}
	classWordCounts := map[string]map[string]int{}
	classWordTotals := map[string]int{}

	for c, docs := range corpus {
		m.Classes = append(m.Classes, c)
		classCounts[c] = len(docs)
		totalDocs += len(docs)
		classWordCounts[c] = map[string]int{}
		for _, d := range docs {
			for _, w := range d {
				classWordCounts[c][w]++
				classWordTotals[c]++
				m.Vocabulary[w]++
			}
		}
	}
	sort.Strings(m.Classes)
	m.TotalDocs = totalDocs
	m.TotalClasses = len(m.Classes)
	if totalDocs == 0 {
		return m
	}
	V := float64(len(m.Vocabulary))
	for _, c := range m.Classes {
		m.ClassPrior[c] = math.Log(float64(classCounts[c]) / float64(totalDocs))
		m.WordLikely[c] = map[string]float64{}
		denom := float64(classWordTotals[c]) + V // Laplace smoothing
		for w := range m.Vocabulary {
			count := float64(classWordCounts[c][w])
			m.WordLikely[c][w] = math.Log((count + 1) / denom)
		}
	}
	return m
}

// Score returns log-posterior log P(c|tokens) for each class.
func (m *Model) Score(tokens []string) map[string]float64 {
	out := map[string]float64{}
	if m.IsEmpty() {
		return out
	}
	for _, c := range m.Classes {
		score := m.ClassPrior[c]
		for _, w := range tokens {
			if lp, ok := m.WordLikely[c][w]; ok {
				score += lp
			}
		}
		out[c] = score
	}
	return out
}

// Rerank takes (category → rule confidence) candidates and re-ranks them
// using the trained model. Returns a new map with blended probabilities.
// When the model is empty the input is returned verbatim.
//
// Calibration: blended = max(rule, alpha*rule + (1-alpha)*ml). The blend
// preserves the original confidence as a floor so we never drop a confident
// rule-based finding because of sparse history.
func (m *Model) Rerank(input map[string]float64, evidence []string, alpha float64) map[string]float64 {
	if m.IsEmpty() {
		return input
	}
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	tokens := normaliseTokens(strings.Join(evidence, " "))
	logp := m.Score(tokens)
	if len(logp) == 0 {
		return input
	}
	// Softmax over classes present in input.
	maxLog := math.Inf(-1)
	for _, c := range m.Classes {
		if _, ok := input[c]; !ok {
			continue
		}
		if logp[c] > maxLog {
			maxLog = logp[c]
		}
	}
	if math.IsInf(maxLog, -1) {
		return input
	}
	denom := 0.0
	for _, c := range m.Classes {
		if _, ok := input[c]; !ok {
			continue
		}
		denom += math.Exp(logp[c] - maxLog)
	}
	out := make(map[string]float64, len(input))
	for cat, conf := range input {
		ml := 0.0
		if _, ok := logp[cat]; ok {
			ml = math.Exp(logp[cat]-maxLog) / denom
		}
		blended := alpha*conf + (1-alpha)*ml
		if blended < conf {
			blended = conf
		}
		out[cat] = round2(blended)
	}
	return out
}

// tokenise produces a flat token list from the supplied strings.
func tokenise(parts ...string) []string {
	return normaliseTokens(strings.Join(parts, " "))
}

func normaliseTokens(s string) []string {
	s = strings.ToLower(s)
	tokens := make([]string, 0, 16)
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
