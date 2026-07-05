package lyrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	timeTokenRE = regexp.MustCompile(`[\[<](\d{1,3}):(\d{1,2})\.(\d{1,6})[\]>]`)
	tagRE       = regexp.MustCompile(`^\[([A-Za-z#][^:\]]*):([^\]]*)\]$`)
)

// SPLParser parses SPL and its LRC/Enhanced LRC subsets.
type SPLParser struct{}

func (SPLParser) Format() string { return "spl" }

func (SPLParser) Parse(text string) (*Lyric, error) {
	ly := &Lyric{Tags: map[string]string{}}
	var lastMain *Line

	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		lineText := strings.TrimSpace(raw)
		if lineText == "" {
			continue
		}
		if parseTag(lineText, ly.Tags) {
			continue
		}

		tokens, err := scanTokens(lineText)
		if err != nil {
			return nil, err
		}
		if len(tokens) == 0 {
			if lastMain != nil {
				appendTranslation(lastMain, lineText)
			}
			continue
		}

		lines, isTranslation := buildLines(lineText, tokens)
		if (isTranslation || sameTimestampTranslation(lines, lastMain)) && lastMain != nil {
			appendTranslation(lastMain, lines[0].Text)
			continue
		}
		for i := range lines {
			ly.Lines = append(ly.Lines, lines[i])
			lastMain = &ly.Lines[len(ly.Lines)-1]
		}
	}

	fillImplicitEnds(ly.Lines)
	applyOffset(ly)
	return ly, nil
}

type token struct {
	start int
	end   int
	ms    int
	angle bool
}

func scanTokens(s string) ([]token, error) {
	matches := timeTokenRE.FindAllStringSubmatchIndex(s, -1)
	out := make([]token, 0, len(matches))
	for _, m := range matches {
		ms, err := parseTimestamp(s[m[2]:m[3]], s[m[4]:m[5]], s[m[6]:m[7]])
		if err != nil {
			return nil, err
		}
		out = append(out, token{
			start: m[0],
			end:   m[1],
			ms:    ms,
			angle: s[m[0]] == '<',
		})
	}
	return out, nil
}

func parseTimestamp(min, sec, frac string) (int, error) {
	m, err := strconv.Atoi(min)
	if err != nil {
		return 0, fmt.Errorf("parse minute %q: %w", min, err)
	}
	s, err := strconv.Atoi(sec)
	if err != nil {
		return 0, fmt.Errorf("parse second %q: %w", sec, err)
	}
	for len(frac) < 3 {
		frac += "0"
	}
	if len(frac) > 3 {
		frac = frac[:3]
	}
	f, err := strconv.Atoi(frac)
	if err != nil {
		return 0, fmt.Errorf("parse fraction %q: %w", frac, err)
	}
	return m*60000 + s*1000 + f, nil
}

func parseTag(line string, tags map[string]string) bool {
	matches := tagRE.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return false
	}
	consumed := ""
	for _, m := range matches {
		consumed += m[0]
		tags[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
	}
	return consumed == line
}

func buildLines(src string, tokens []token) ([]Line, bool) {
	if len(tokens) == 0 || tokens[0].angle {
		return nil, false
	}

	firstText := strings.TrimSpace(src[:tokens[0].start])
	if firstText != "" {
		return []Line{{Text: firstText}}, true
	}

	prefixCount := 1
	for prefixCount < len(tokens) &&
		!tokens[prefixCount].angle &&
		strings.TrimSpace(src[tokens[prefixCount-1].end:tokens[prefixCount].start]) == "" {
		prefixCount++
	}

	hasInlineWords := prefixCount < len(tokens)
	text := textOutsideTokens(src, tokens[prefixCount-1:])
	if !hasInlineWords && prefixCount > 1 {
		lines := make([]Line, 0, prefixCount)
		for i := 0; i < prefixCount; i++ {
			lines = append(lines, Line{StartMs: tokens[i].ms, Text: text})
		}
		return lines, false
	}

	line := Line{StartMs: tokens[0].ms, Text: text}
	if hasInlineWords {
		line.Words, line.EndMs = buildWords(src, tokens[prefixCount-1:])
		if len(line.Words) == 0 {
			line.Text = text
		}
	} else if strings.TrimSpace(src[tokens[len(tokens)-1].end:]) == "" && len(tokens) > 1 {
		line.EndMs = tokens[len(tokens)-1].ms
	}
	return []Line{line}, false
}

func buildWords(src string, tokens []token) ([]Word, int) {
	words := []Word{}
	endMs := 0
	for i := 0; i < len(tokens)-1; i++ {
		text := src[tokens[i].end:tokens[i+1].start]
		if text == "" || tokens[i].ms >= tokens[i+1].ms {
			continue
		}
		words = append(words, Word{Text: text, StartMs: tokens[i].ms, EndMs: tokens[i+1].ms})
	}
	if len(tokens) > 0 && strings.TrimSpace(src[tokens[len(tokens)-1].end:]) == "" {
		endMs = tokens[len(tokens)-1].ms
	}
	return words, endMs
}

func textOutsideTokens(src string, tokens []token) string {
	var b strings.Builder
	for i := 0; i < len(tokens)-1; i++ {
		b.WriteString(src[tokens[i].end:tokens[i+1].start])
	}
	if len(tokens) > 0 {
		b.WriteString(src[tokens[len(tokens)-1].end:])
	}
	return b.String()
}

func appendTranslation(line *Line, text string) {
	if line.Translation == "" {
		line.Translation = text
		return
	}
	line.Translation += "\n" + text
}

func sameTimestampTranslation(lines []Line, lastMain *Line) bool {
	return lastMain != nil &&
		len(lines) == 1 &&
		lines[0].StartMs == lastMain.StartMs &&
		len(lines[0].Words) == 0
}

func applyOffset(ly *Lyric) {
	raw := ly.Tags["offset"]
	if raw == "" {
		return
	}
	offset, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || offset == 0 {
		return
	}
	for i := range ly.Lines {
		ly.Lines[i].StartMs += offset
		if ly.Lines[i].EndMs > 0 {
			ly.Lines[i].EndMs += offset
		}
		for j := range ly.Lines[i].Words {
			ly.Lines[i].Words[j].StartMs += offset
			ly.Lines[i].Words[j].EndMs += offset
		}
	}
}

func fillImplicitEnds(lines []Line) {
	for i := range lines {
		if lines[i].EndMs > 0 {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if lines[j].StartMs > lines[i].StartMs {
				lines[i].EndMs = lines[j].StartMs
				break
			}
		}
	}
}
