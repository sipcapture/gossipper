package uistore

import (
	"fmt"
	"regexp"
	"strings"
)

// mediaRefPattern matches `[[media:wav/<name>]]` / `[[media:pcap/<name>]]`.
// Name may contain letters, digits, dot, underscore, dash. Whitespace inside
// the brackets is tolerated but discouraged.
var mediaRefPattern = regexp.MustCompile(`\[\[\s*media:(wav|pcap)/([A-Za-z0-9._-]+)\s*\]\]`)

// PreprocessScenarioXML rewrites `[[media:<kind>/<name>]]` aliases inside an
// XML scenario so SIPp-style references resolve to absolute on-disk paths
// owned by the uistore. Unknown media is reported as a single error per
// reference; the returned XML still contains the original alias so the engine
// can surface a meaningful parser error.
func (s *Store) PreprocessScenarioXML(xml string) (string, []error) {
	if !strings.Contains(xml, "[[media:") {
		return xml, nil
	}
	var errs []error
	out := mediaRefPattern.ReplaceAllStringFunc(xml, func(match string) string {
		parts := mediaRefPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		kind := MediaKind(parts[1])
		name := parts[2]
		path, err := s.MediaPath(kind, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("media ref %q: %w", match, err))
			return match
		}
		return path
	})
	return out, errs
}
