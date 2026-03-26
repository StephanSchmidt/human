package index

import (
	"context"
	"fmt"
	"io"

	"github.com/StephanSchmidt/human/internal/tracker"
)

// SyncResult summarises one sync run.
type SyncResult struct {
	Indexed int
	Pruned  int
	Errors  int
}

// Sync iterates all instances, lists issues per configured project,
// fetches descriptions, upserts entries, and prunes stale keys.
// When fullSync is false, it performs incremental sync using the last
// indexed timestamp per source to only fetch recently updated issues.
func Sync(ctx context.Context, store Store, instances []tracker.Instance, fullSync bool, logger io.Writer) (*SyncResult, error) {
	result := &SyncResult{}

	for i := range instances {
		inst := &instances[i]
		if err := syncInstance(ctx, store, inst, fullSync, logger, result); err != nil {
			_, _ = fmt.Fprintf(logger, "Error syncing %s (%s): %v\n", inst.Name, inst.Kind, err)
			result.Errors++
		}
	}

	return result, nil
}

// syncInstance syncs a single tracker instance.
func syncInstance(ctx context.Context, store Store, inst *tracker.Instance, fullSync bool, logger io.Writer, result *SyncResult) error {
	seen := make(map[string]bool)

	// Determine if we can do incremental sync.
	lastIndexed, err := store.LastIndexedAt(ctx, inst.Name)
	if err != nil {
		return err
	}

	incremental := !fullSync && !lastIndexed.IsZero()

	if incremental {
		_, _ = fmt.Fprintf(logger, "Incremental sync for %s (%s) since %s\n", inst.Name, inst.Kind, lastIndexed.Format("2006-01-02 15:04:05"))
	} else {
		_, _ = fmt.Fprintf(logger, "Full sync for %s (%s)\n", inst.Name, inst.Kind)
	}

	// When projects are configured, sync each one; otherwise sync all projects at once.
	projects := inst.Projects
	if len(projects) == 0 {
		projects = []string{""}
	}

	for _, project := range projects {
		opts := tracker.ListOptions{
			Project:    project,
			MaxResults: 100,
			IncludeAll: fullSync,
		}
		if incremental {
			opts.UpdatedSince = lastIndexed
		}

		issues, err := inst.Provider.ListIssues(ctx, opts)
		if err != nil {
			_, _ = fmt.Fprintf(logger, "  Error listing %s/%s: %v\n", inst.Name, project, err)
			result.Errors++
			continue
		}

		label := project
		if label == "" {
			label = "(all projects)"
		}
		_, _ = fmt.Fprintf(logger, "Indexing %s (%s): %s (%d issues)...\n", inst.Name, inst.Kind, label, len(issues))

		for _, issue := range issues {
			full, err := inst.Provider.GetIssue(ctx, issue.Key)
			if err != nil {
				_, _ = fmt.Fprintf(logger, "  Error fetching %s: %v\n", issue.Key, err)
				result.Errors++
				continue
			}

			p := project
			if p == "" {
				p = full.Project
			}
			entry := Entry{
				Key:      issue.Key,
				Source:   inst.Name,
				Kind:     inst.Kind,
				Project:  p,
				Title:    full.Title,
				Status:   full.Status,
				Assignee: full.Assignee,
				URL:      inst.URL,
			}
			if err := store.UpsertEntry(ctx, entry, full.Description); err != nil {
				_, _ = fmt.Fprintf(logger, "  Error indexing %s: %v\n", issue.Key, err)
				result.Errors++
				continue
			}
			seen[issue.Key] = true
			result.Indexed++
		}
	}

	// Only prune on full sync — incremental sync cannot detect deletions.
	if !incremental {
		existingKeys, err := store.AllKeys(ctx, inst.Name)
		if err != nil {
			return err
		}
		for _, key := range existingKeys {
			if !seen[key] {
				if err := store.DeleteEntry(ctx, key, inst.Name); err != nil {
					_, _ = fmt.Fprintf(logger, "  Error pruning %s: %v\n", key, err)
					result.Errors++
					continue
				}
				result.Pruned++
			}
		}
	}

	return nil
}
