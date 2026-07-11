package brain

import (
	"math"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// TopicFingerprint is a compact representation of a conversation topic.
// It uses term frequency weighting — no LLM calls, pure CPU.
type TopicFingerprint struct {
	Terms    map[string]float64 // term → weight (TF-style)
	RawTerms []string           // ordered for similarity comparison
}

// TopicDetector watches for topic shifts across consecutive user turns.
// It's stateless — caller holds the previous fingerprint for comparison.
type TopicDetector struct {
	stopWords map[string]struct{}

	// A regex to capture domain-specific signals: file paths, symbols, error codes
	domainPattern *regexp.Regexp

	mu sync.RWMutex
}

// NewTopicDetector creates a detector with a built-in stop word list.
func NewTopicDetector() *TopicDetector {
	return &TopicDetector{
		stopWords: newStopWords(),
		domainPattern: regexp.MustCompile(
			`([\w./\\-]+\.\w{1,6})` + // file paths: main.go, src/app.ts
				`|([A-Z][a-z]+Error|Err[A-Z]\w+)` + // error types: NotFoundError
				`|(\w+Func|\w+Handler|\w+Service|\w+Repo)` + // code symbols
				`|(` + "`" + `[^` + "`" + `]+` + "`" + `)`, // inline code: `authMiddleware`
		),
	}
}

// ExtractFingerprint tokenizes text and builds a weighted term set.
func (d *TopicDetector) ExtractFingerprint(text string) TopicFingerprint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	terms := make(map[string]float64)
	var raw []string

	// 1. Extract domain-specific signals (high weight)
	domainMatches := d.domainPattern.FindAllString(text, -1)
	for _, m := range domainMatches {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// Domain terms get double weight
		terms[m] += 2.0
		if _, exists := terms[m]; !exists || terms[m] == 2.0 {
			raw = append(raw, m)
		}
	}

	// 2. General tokenization
	tokens := tokenize(text)
	for _, t := range tokens {
		if _, isStop := d.stopWords[t]; isStop {
			continue
		}
		if len(t) < 2 {
			continue
		}
		terms[t] += 1.0
		raw = append(raw, t)
	}

	return TopicFingerprint{Terms: terms, RawTerms: raw}
}

// DetectShift returns true if the two fingerprints suggest a topic change.
// It uses a combo of Jaccard similarity and cosine similarity on term weights.
// score < threshold → shift detected.
//
// Threshold guide:
//   - 0.15+: lenient (fewer splits, more context retention)
//   - 0.08-: strict (more splits, cleaner isolation)
//   - Default 0.12: balanced
func (d *TopicDetector) DetectShift(prev, curr TopicFingerprint, threshold float64) bool {
	if len(prev.Terms) == 0 || len(curr.Terms) == 0 {
		return false // first turn or one side empty → no shift
	}

	jaccard := jaccardSimilarity(prev.RawTerms, curr.RawTerms)
	cosine := cosineSimilarity(prev.Terms, curr.Terms)

	// Weighted blend: Jaccard favors lexical overlap, Cosine favors weighted terms
	score := 0.4*jaccard + 0.6*cosine
	return score < threshold
}

// SuggestTopicLabel picks the top-3 highest-weight terms as a label.
func (d *TopicDetector) SuggestTopicLabel(fp TopicFingerprint) string {
	type termScore struct {
		term  string
		score float64
	}
	var scored []termScore
	for t, s := range fp.Terms {
		scored = append(scored, termScore{t, s})
	}
	// Sort descending
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	n := 3
	if len(scored) < n {
		n = len(scored)
	}
	parts := make([]string, 0, n)
	for _, s := range scored[:n] {
		parts = append(parts, s.term)
	}
	return strings.Join(parts, "/")
}

// --- similarity functions ---

func jaccardSimilarity[T comparable](a, b []T) float64 {
	setA := make(map[T]struct{}, len(a))
	setB := make(map[T]struct{}, len(b))
	intersection := 0

	for _, v := range a {
		setA[v] = struct{}{}
	}
	for _, v := range b {
		setB[v] = struct{}{}
	}
	for v := range setA {
		if _, ok := setB[v]; ok {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func cosineSimilarity(a, b map[string]float64) float64 {
	dotProduct := 0.0
	normA := 0.0
	normB := 0.0

	for k, v := range a {
		normA += v * v
		if w, ok := b[k]; ok {
			dotProduct += v * w
		}
	}
	for _, v := range b {
		normB += v * v
	}

	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func tokenize(text string) []string {
	var tokens []string
	var buf strings.Builder

	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			// CJK character: flush ASCII buffer, emit as individual token
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
			tokens = append(tokens, string(unicode.ToLower(r)))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/' {
			buf.WriteRune(unicode.ToLower(r))
		} else {
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func newStopWords() map[string]struct{} {
	words := []string{
		"the", "a", "an", "is", "are", "was", "were", "be", "been",
		"have", "has", "had", "do", "does", "did", "will", "would",
		"could", "should", "may", "might", "shall", "can", "need",
		"this", "that", "these", "those", "i", "you", "he", "she",
		"it", "we", "they", "me", "him", "her", "us", "them",
		"my", "your", "his", "its", "our", "their",
		"and", "or", "but", "if", "because", "so", "then", "than",
		"too", "very", "just", "about", "also", "more", "some",
		"any", "each", "every", "all", "both", "few", "many",
		"much", "no", "not", "only", "own", "same", "what",
		"which", "who", "whom", "where", "when", "why", "how",
		"in", "on", "at", "to", "for", "with", "by", "from",
		"up", "down", "out", "off", "over", "under", "again",
		"further", "once", "here", "there", "get", "got",
		"make", "made", "know", "think", "see", "want", "go",
		"come", "take", "use", "find", "give", "tell", "ask",
		"please", "yes", "no", "ok", "okay", "thanks", "thank",
		"let", "like", "say", "says", "said", "look", "looks",
		"looking", "need", "needs", "needed",
	}
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}
