// Integration tests that exercise the human binary against live trackers.
// Credentials come from environment variables (see local/test.env.example).
//
// Usage: source local/test.env && make test-integration
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var bin string

var (
	passed int
	failed int
)

type trackerTest struct {
	name    string   // "jira", "linear", etc.
	tracker string   // --tracker flag value (e.g. "amazingcto", "work")
	project string   // --project value
	create  []string // extra create args (e.g. "--type", "Story" for Jira)
}

func main() {
	bin = os.Getenv("HUMAN_BIN")
	if bin == "" {
		bin = "./bin/human"
	}
	if _, err := os.Stat(bin); err != nil {
		fatal("binary not found at %s — run 'make build' first", bin)
	}

	ran := 0

	// ── Jira ────────────────────────────────────────
	if p := os.Getenv("HUMAN_TEST_JIRA_PROJECT"); p != "" {
		trackerName := os.Getenv("HUMAN_TEST_JIRA_TRACKER")
		if trackerName == "" {
			trackerName = "amazingcto"
		}
		issueType := os.Getenv("HUMAN_TEST_JIRA_TYPE")
		if issueType == "" {
			issueType = "Task"
		}
		runTracker(trackerTest{
			name: "jira", tracker: trackerName, project: p,
			create: []string{"--type", issueType},
		})
		ran++
	}

	// ── Linear ──────────────────────────────────────
	if p := os.Getenv("HUMAN_TEST_LINEAR_PROJECT"); p != "" {
		trackerName := os.Getenv("HUMAN_TEST_LINEAR_TRACKER")
		if trackerName == "" {
			trackerName = "work"
		}
		runTracker(trackerTest{
			name: "linear", tracker: trackerName, project: p,
		})
		ran++
	}

	// ── GitLab ──────────────────────────────────────
	if p := os.Getenv("HUMAN_TEST_GITLAB_PROJECT"); p != "" {
		trackerName := os.Getenv("HUMAN_TEST_GITLAB_TRACKER")
		if trackerName == "" {
			trackerName = "human"
		}
		runTracker(trackerTest{
			name: "gitlab", tracker: trackerName, project: p,
		})
		ran++
	}

	// ── Summary ─────────────────────────────────────
	fmt.Println()
	if ran == 0 {
		fatal("no trackers configured — set HUMAN_TEST_*_PROJECT env vars (see local/test.env.example)")
	}

	fmt.Println("─────────────────────────────────────")
	fmt.Printf("  %d passed, %d failed\n", passed, failed)
	fmt.Println("─────────────────────────────────────")

	if failed > 0 {
		os.Exit(1)
	}
}

// runTracker executes the 6-step integration test sequence for a single tracker.
// If the initial create step fails, remaining steps are skipped (but other trackers still run).
func runTracker(t trackerTest) {
	fmt.Printf("\n━━━ %s (tracker=%s, project=%s) ━━━\n", t.name, t.tracker, t.project)

	ts := fmt.Sprintf("%d", time.Now().Unix())
	summary := "integration-test-" + ts
	comment := "test comment " + ts

	run := func(desc string, args ...string) (string, bool) {
		fullArgs := []string{"--tracker", t.tracker}
		fullArgs = append(fullArgs, args...)
		return mustRun(desc, fullArgs...)
	}

	// 1. Create a ticket
	section("issue create")
	createArgs := []string{"issue", "create", "--project", t.project, "--description", "automated integration test"}
	createArgs = append(createArgs, t.create...)
	createArgs = append(createArgs, summary)
	createOut, ok := run("issue create", createArgs...)
	if !ok {
		fmt.Printf("  skipping remaining %s steps (create failed)\n", t.name)
		return
	}

	createdKey := firstField(createOut)
	fmt.Printf("  created %s\n", createdKey)

	// 2. Add a comment
	section("issue comment add")
	addOut, ok := run("issue comment add",
		"issue", "comment", "add", createdKey, comment)

	if ok {
		commentID := firstField(addOut)
		fmt.Printf("  comment id %s\n", commentID)
	}

	// 3. Read the ticket back — verify summary appears
	section("issue get")
	getOut, ok := run("issue get",
		"issue", "get", createdKey)
	if ok {
		assertContains("issue get contains summary", getOut, summary)
		assertContains("issue get contains description", getOut, "automated integration test")
	}

	// 4. Read comments back — verify our comment appears
	section("issue comment list")
	listCommentsOut, ok := run("issue comment list",
		"issue", "comment", "list", createdKey)
	if ok {
		assertContains("comment list contains comment", listCommentsOut, comment)
	}

	// 5. List all tickets — verify the created ticket is in there.
	//    Some trackers (Jira) have eventual-consistency search indexes,
	//    so retry a few times before giving up.
	section("issues list")
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		listOut, ok := run("issues list",
			"issues", "list", "--project", t.project)
		if !ok {
			break
		}

		var issues []struct {
			Key string `json:"key"`
		}
		mustUnmarshal(listOut, &issues)

		found := false
		for _, iss := range issues {
			if iss.Key == createdKey {
				found = true
				break
			}
		}
		if found {
			pass("issues list contains " + createdKey)
			break
		}
		if attempt < maxAttempts {
			fmt.Printf("  %s not yet in index, retrying in 2s (%d/%d)\n", createdKey, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		} else {
			fail("issues list contains "+createdKey, "not found in %d issues after %d attempts", len(issues), maxAttempts)
		}
	}

	// 6. Delete the ticket
	section("issue delete")
	_, ok = run("issue delete",
		"issue", "delete", createdKey)
	if ok {
		fmt.Printf("  deleted %s\n", createdKey)
	}
}

// ── Helpers ─────────────────────────────────────────

func mustRun(desc string, args ...string) (string, bool) {
	cmd := exec.Command(bin, args...) // #nosec G204 -- integration test intentionally runs the built binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		fail(desc, "%s: %s", err, string(out))
		return "", false
	}
	pass(desc)
	return string(out), true
}

func mustUnmarshal(data string, v any) {
	if err := json.Unmarshal([]byte(data), v); err != nil {
		fatal("unmarshal: %s\n  data: %.200s", err, data)
	}
}

func firstField(line string) string {
	line = strings.TrimSpace(line)
	if i := strings.IndexAny(line, " \t"); i != -1 {
		return line[:i]
	}
	return line
}

func assertContains(desc, haystack, needle string) {
	if strings.Contains(haystack, needle) {
		pass(desc)
	} else {
		fail(desc, "expected to contain %q:\n  %.300s", needle, haystack)
	}
}

// ── Reporting ───────────────────────────────────────

func section(name string) {
	fmt.Printf("\n=== %s ===\n", name)
}

func pass(desc string) {
	passed++
	fmt.Printf("  PASS  %s\n", desc)
}

func fail(desc, format string, a ...any) {
	failed++
	fmt.Printf("  FAIL  %s: %s\n", desc, fmt.Sprintf(format, a...))
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	os.Exit(2)
}
