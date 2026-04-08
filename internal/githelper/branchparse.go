package githelper

import "regexp"

// cuBranchRe matches CU-<id> or cu-<id> in branch names, including after
// common prefixes like "feature/". The ID is a ClickUp native alphanumeric ID.
var cuBranchRe = regexp.MustCompile(`(?i)(?:^|/)cu-([a-z0-9]+)`)

// customIDBranchRe matches PREFIX-123 custom task IDs in branch names,
// including after common prefixes like "feature/".
var customIDBranchRe = regexp.MustCompile(`(?:^|/)([A-Z][A-Z0-9]*-\d+)`)

// excludedPrefixes avoids false matches on common branch prefixes.
var excludedPrefixes = map[string]bool{
	"FEATURE": true, "BUGFIX": true, "RELEASE": true,
	"HOTFIX": true, "FIX": true, "CHORE": true,
	"DOCS": true, "REFACTOR": true, "TEST": true, "CI": true,
}

// TaskIDFromBranch extracts a ClickUp task ID from a git branch name.
//
// Patterns matched (in order):
//  1. "CU-abc123-description"     → "abc123"  (ClickUp native ID)
//  2. "feature/CU-abc123-desc"    → "abc123"  (with branch prefix)
//  3. "PROJ-42-description"       → "PROJ-42" (custom task ID)
//  4. "feature/PROJ-42-desc"      → "PROJ-42" (with branch prefix)
//
// Returns "" if no task ID pattern is found.
func TaskIDFromBranch(branch string) string {
	// Try ClickUp native ID first (CU-<alphanumeric>).
	if m := cuBranchRe.FindStringSubmatch(branch); m != nil {
		return m[1]
	}

	// Try custom task ID (PREFIX-123). FindAllStringSubmatch lets us
	// skip over leading excluded prefixes (e.g. "FEATURE-1/PROJ-42")
	// and still return the real ID that follows.
	for _, m := range customIDBranchRe.FindAllStringSubmatch(branch, -1) {
		id := m[1]
		prefix := id
		for i := range id {
			if id[i] == '-' {
				prefix = id[:i]
				break
			}
		}
		if excludedPrefixes[prefix] {
			continue
		}
		return id
	}

	return ""
}
