package explain

import (
	"math"
	"regexp"
	"strconv"
)

var numberTokenRe = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)

// Faithfulness implements METRICS 3.5 "Faithfulness: every number/claim derivable from
// facts" as a pure function. It re-derives every numeric token found in a rendered text
// from the `facts` map and returns the numbers that have no numeric source. An empty
// result means the text is fully faithful.
func Faithfulness(text string, facts map[string]any) []string {
	if facts == nil {
		facts = map[string]any{}
	}
	available := collectNumbers(facts)
	unsourced := make([]string, 0)
	for _, token := range numberTokenRe.FindAllString(text, -1) {
		value, err := strconv.ParseFloat(token, 64)
		if err != nil {
			continue
		}
		if !hasNumber(available, value) {
			unsourced = append(unsourced, token)
		}
	}
	if len(unsourced) == 0 {
		return nil
	}
	return unsourced
}

// collectNumbers gathers every numeric scalar reachable in the facts map (including
// numbers inside nested maps and slices).
func collectNumbers(facts map[string]any) []float64 {
	out := make([]float64, 0)
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case float64:
			out = append(out, value)
		case float32:
			out = append(out, float64(value))
		case int:
			out = append(out, float64(value))
		case int64:
			out = append(out, float64(value))
		case uint:
			out = append(out, float64(value))
		case map[string]any:
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	for _, v := range facts {
		walk(v)
	}
	return out
}

// hasNumber reports whether a numeric token is present among the fact numbers within a
// tolerance that absorbs binary float noise at report precision (<= 1e-6).
func hasNumber(available []float64, target float64) bool {
	for _, value := range available {
		if math.Abs(value-target) <= 1e-6 {
			return true
		}
	}
	return false
}
