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
func Sync(ctx context.Context, store Store, instances []tracker.Instance, includeAll bool, logger io.Writer) (*SyncResult, error) {
	result := &SyncResult{}

	for i := range instances {
		inst := &instances[i]
		if len(inst.Projects) == 0 {
			_, _ = fmt.Fprintf(logger, "Skipping %s (%s): no projects configured\n", inst.Name, inst.Kind)
			continue
		}
		if err := syncInstance(ctx, store, inst, includeAll, logger, result); err != nil {
			_, _ = fmt.Fprintf(logger, "Error syncing %s (%s): %v\n", inst.Name, inst.Kind, err)
			result.Errors++
		}
	}

	return result, nil
}

// syncInstance syncs a single tracker instance.
func syncInstance(ctx context.Context, store Store, inst *tracker.Instance, includeAll bool, logger io.Writer, result *SyncResult) error {
	seen := make(map[string]bool)

	for _, project := range inst.Projects {
		issues, err := inst.Provider.ListIssues(ctx, tracker.ListOptions{
			Project:    project,
			MaxResults: 100,
			IncludeAll: includeAll,
		})
		if err != nil {
			_, _ = fmt.Fprintf(logger, "  Error listing %s/%s: %v\n", inst.Name, project, err)
			result.Errors++
			continue
		}

		_, _ = fmt.Fprintf(logger, "Indexing %s (%s): %s (%d issues)...\n", inst.Name, inst.Kind, project, len(issues))

		for _, issue := range issues {
			full, err := inst.Provider.GetIssue(ctx, issue.Key)
			if err != nil {
				_, _ = fmt.Fprintf(logger, "  Error fetching %s: %v\n", issue.Key, err)
				result.Errors++
				continue
			}

			entry := Entry{
				Key:      issue.Key,
				Source:   inst.Name,
				Kind:     inst.Kind,
				Project:  project,
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

	// Prune stale entries for this instance.
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

	return nil
}
