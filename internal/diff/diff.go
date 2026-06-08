// Package diff parses a unified diff and classifies hunks into the change types
// that affect LLM cost. It does not need an AST: the patterns that matter
// (model string, max_tokens, retry/loop wrappers, call-site markers) are
// detectable with line-level regex matching.
package diff

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// ChangeType names the cost-relevant mutation a hunk introduces.
type ChangeType int

const (
	ChangeUnknown       ChangeType = iota
	ChangeModelSwap                // model identifier changed
	ChangeMaxTokens                // max_tokens / max_output_tokens value changed
	ChangeVolumeAdded              // retry, loop, or batch wrapper added
	ChangeVolumeRemoved            // retry, loop, or batch wrapper removed
	ChangeCallAdded                // new LLM call site
	ChangeCallRemoved              // LLM call site removed
)

func (c ChangeType) String() string {
	return [...]string{
		"unknown", "model_swap", "max_tokens",
		"volume_added", "volume_removed", "call_added", "call_removed",
	}[c]
}

// Hunk is one contiguous block of changes from a unified diff.
type Hunk struct {
	File    string
	OldLine int
	NewLine int
	Removed []string
	Added   []string
}

// Change is a classified hunk. A single hunk can produce at most one Change;
// the classifier picks the most specific matching type.
type Change struct {
	Hunk
	Type     ChangeType
	Label    string // call-site label extracted from surrounding context
	OldValue string // e.g. old model name, old max_tokens
	NewValue string // e.g. new model name, new max_tokens
}

// patterns below match the line content (after the +/- prefix) for the
// patterns that matter to cost.

var (
	// model string in code: model="...", model: "...", "model": "...", Go const/var *Model* identifiers
	reModel = regexp.MustCompile(`(?i)(?:\w*[Mm]odel\w*\s*[:=]+\s*[` + "`" + `"']|"model"\s*:\s*")([a-z0-9._:/-]+)`)
	// max_tokens / max_output_tokens integer value
	reMaxTok = regexp.MustCompile(`(?i)max_?(?:output_?)?tokens\s*[:=]\s*(\d+)`)
	// retry / loop / batch wrappers that multiply call volume
	reVolume = regexp.MustCompile(`(?i)\b(?:retry|retries|for\s+_?,?\s*:?=\s*range|for\s+i\s*:?=|for\s+\w+\s+in\s+|batch|ProcessBatch|map\(lambda|asyncio\.gather)\b`)
	// LLM call sites: common SDK method patterns
	reCallSite = regexp.MustCompile(`(?i)\b(?:client\.chat\.completions\.create|client\.messages\.create|GenerateContent|generate_content|openai\.ChatCompletion|anthropic\.messages|genai\.GenerativeModel)\b`)
	// call-site label comment: # label: foo  or // spendlint:label foo
	reLabel = regexp.MustCompile(`(?i)(?:#|//)\s*(?:label:|spendlint:label)\s*(\S+)`)
)

// Parse parses a unified diff string into a slice of Hunk values.
func Parse(unifiedDiff string) ([]Hunk, error) {
	var hunks []Hunk
	var cur *Hunk
	var file string
	oldLine, newLine := 0, 0

	sc := bufio.NewScanner(strings.NewReader(unifiedDiff))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "--- "):
			// old file name (we use +++ for the canonical name)
		case strings.HasPrefix(line, "+++ "):
			file = strings.TrimPrefix(line, "+++ ")
			if len(file) > 2 && file[:2] == "b/" {
				file = file[2:]
			}
		case strings.HasPrefix(line, "@@ "):
			if cur != nil {
				hunks = append(hunks, *cur)
			}
			ol, nl, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			oldLine, newLine = ol, nl
			cur = &Hunk{File: file, OldLine: oldLine, NewLine: newLine}
		case cur != nil && strings.HasPrefix(line, "-"):
			cur.Removed = append(cur.Removed, line[1:])
			oldLine++
		case cur != nil && strings.HasPrefix(line, "+"):
			cur.Added = append(cur.Added, line[1:])
			newLine++
		default:
			oldLine++
			newLine++
		}
	}
	if cur != nil {
		hunks = append(hunks, *cur)
	}
	return hunks, sc.Err()
}

// parseHunkHeader extracts the old and new starting line numbers from a @@ line.
func parseHunkHeader(line string) (oldStart, newStart int, err error) {
	// @@ -<old>,<count> +<new>,<count> @@
	var oStart, oCount, nStart, nCount int
	_, scanErr := fmt.Sscanf(line, "@@ -%d,%d +%d,%d", &oStart, &oCount, &nStart, &nCount)
	if scanErr != nil {
		// Try without counts (single-line hunks): @@ -N +N @@
		_, scanErr = fmt.Sscanf(line, "@@ -%d +%d", &oStart, &nStart)
		if scanErr != nil {
			return 0, 0, fmt.Errorf("parse hunk header %q: %w", line, scanErr)
		}
	}
	return oStart, nStart, nil
}

// Classify inspects a hunk and returns the most specific cost-relevant Change,
// or nil when the hunk has no known cost impact.
func Classify(h Hunk) *Change {
	c := &Change{Hunk: h}
	c.Label = extractLabel(h)

	// Model swap: old lines had a model, new lines have a different model.
	oldModel := firstMatch(reModel, h.Removed)
	newModel := firstMatch(reModel, h.Added)
	if oldModel != "" && newModel != "" && oldModel != newModel {
		c.Type, c.OldValue, c.NewValue = ChangeModelSwap, oldModel, newModel
		return c
	}

	// max_tokens change.
	oldTok := firstMatch(reMaxTok, h.Removed)
	newTok := firstMatch(reMaxTok, h.Added)
	if oldTok != "" && newTok != "" && oldTok != newTok {
		c.Type, c.OldValue, c.NewValue = ChangeMaxTokens, oldTok, newTok
		return c
	}

	// Volume wrapper added.
	if matchesAny(reVolume, h.Added) && !matchesAny(reVolume, h.Removed) {
		c.Type = ChangeVolumeAdded
		return c
	}

	// Volume wrapper removed.
	if matchesAny(reVolume, h.Removed) && !matchesAny(reVolume, h.Added) {
		c.Type = ChangeVolumeRemoved
		return c
	}

	// Call site added.
	if matchesAny(reCallSite, h.Added) && !matchesAny(reCallSite, h.Removed) {
		c.Type = ChangeCallAdded
		return c
	}

	// Call site removed.
	if matchesAny(reCallSite, h.Removed) && !matchesAny(reCallSite, h.Added) {
		c.Type = ChangeCallRemoved
		return c
	}

	return nil
}

// ClassifyAll classifies every hunk in a diff, returning only the non-nil results.
func ClassifyAll(hunks []Hunk) []Change {
	var out []Change
	for _, h := range hunks {
		if c := Classify(h); c != nil {
			out = append(out, *c)
		}
	}
	return out
}

// firstMatch returns the first submatch of re across lines, or "".
func firstMatch(re *regexp.Regexp, lines []string) string {
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// matchesAny returns true if re matches any line.
func matchesAny(re *regexp.Regexp, lines []string) bool {
	for _, l := range lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// extractLabel tries to find a call-site label in the hunk's context lines.
func extractLabel(h Hunk) string {
	all := append(h.Removed, h.Added...)
	for _, l := range all {
		if m := reLabel.FindStringSubmatch(l); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
