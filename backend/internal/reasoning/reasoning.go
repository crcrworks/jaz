// Package reasoning is the one vocabulary for reasoning effort: the levels Jaz
// stores, their order, and their display names. Every layer that accepts,
// validates, sorts or labels an effort reads it here instead of keeping a list
// of its own.
package reasoning

import (
	"fmt"
	"slices"
	"strings"
)

// The levels Jaz stores, lowest first. Off and None switch thinking off; Ultra
// and Ultracode are the top settings of the agents that have them.
const (
	Off       = "off"
	None      = "none"
	Minimal   = "minimal"
	Low       = "low"
	Medium    = "medium"
	High      = "high"
	XHigh     = "xhigh"
	Max       = "max"
	Ultra     = "ultra"
	Ultracode = "ultracode"
)

// Levels is the whole vocabulary, lowest first.
var Levels = []string{Off, None, Minimal, Low, Medium, High, XHigh, Max, Ultra, Ultracode}

// Graded are the levels that measure thinking depth, the only ones an agent
// encodes in a model id such as "gpt-6-luna/high".
var Graded = []string{Minimal, Low, Medium, High, XHigh, Max}

// Endpoint is the level set an OpenAI-compatible endpoint understands. Off,
// Ultra and Ultracode are Jaz's or an agent's own names for something such an
// endpoint does not take.
var Endpoint = []string{None, Minimal, Low, Medium, High, XHigh, Max}

var labels = map[string]string{
	"":        "Default",
	Off:       "Off",
	None:      "None",
	Minimal:   "Minimal",
	Low:       "Low",
	Medium:    "Medium",
	High:      "High",
	XHigh:     "Extra high",
	Max:       "Max",
	Ultra:     "Ultra",
	Ultracode: "Ultracode",
}

// Label is the display name of a level; an empty level is the model default.
func Label(value string) string {
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

// Normalize returns value as Jaz stores it, or an error naming the levels that
// are accepted. An empty value means no level was chosen.
func Normalize(value string) (string, error) {
	return NormalizeIn(value, Levels)
}

// NormalizeIn normalizes against a subset, such as an endpoint's or an agent's.
func NormalizeIn(value string, levels []string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || slices.Contains(levels, value) {
		return value, nil
	}
	return "", fmt.Errorf("unknown reasoning effort %q; valid values are %s", value, strings.Join(levels, ", "))
}

// Rank orders levels lowest first. A level outside the vocabulary ranks after
// all of them.
func Rank(value string) int {
	if i := slices.Index(Levels, value); i >= 0 {
		return i
	}
	return len(Levels)
}

// Sort orders levels lowest first, keeping the order of levels it does not know.
func Sort(levels []string) {
	slices.SortStableFunc(levels, func(a, b string) int { return Rank(a) - Rank(b) })
}
