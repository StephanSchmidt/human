package cmdutil

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/StephanSchmidt/human/internal/tracker"
)

// Deps holds injectable dependencies for command builders that need
// tracker instance loading and resolution.
type Deps struct {
	LoadInstances     func(dir string) ([]tracker.Instance, error)
	InstanceFromFlags func(cmd *cobra.Command) *tracker.Instance
	AuditLogPath      func() string
}

// DefaultDeps returns a Deps using the real implementations.
func DefaultDeps() Deps {
	return Deps{
		LoadInstances:     LoadAllInstances,
		InstanceFromFlags: InstanceFromFlags,
		AuditLogPath:      AuditLogPath,
	}
}

// ResolveProvider loads instances, applies CLI flag overrides, and resolves
// the provider for the given kind using the tracker name from persistent flags.
func ResolveProvider(cmd *cobra.Command, kind string, deps Deps) (tracker.Provider, func(), error) {
	instances, err := deps.LoadInstances(".")
	if err != nil {
		return nil, nil, err
	}

	if inst := deps.InstanceFromFlags(cmd); inst != nil {
		instances = append(instances, *inst)
	}

	trackerName, _ := cmd.Root().PersistentFlags().GetString("tracker")

	instance, err := tracker.ResolveByKind(kind, instances, trackerName)
	if err != nil {
		return nil, nil, err
	}

	safeFlag, _ := cmd.Root().PersistentFlags().GetBool("safe")
	p := instance.Provider
	if safeFlag || instance.Safe {
		p = tracker.NewSafeProvider(p, instance.Name)
	}

	auditPath := deps.AuditLogPath()
	ap, auditErr := tracker.NewAuditProvider(p, instance.Name, instance.Kind, auditPath)
	if auditErr != nil {
		fmt.Fprintln(os.Stderr, "warning: audit logging disabled:", auditErr)
		return p, func() {}, nil
	}
	return ap, func() { _ = ap.Close() }, nil
}

// ResolveAutoProvider loads all instances, applies flag overrides, and resolves
// the provider without requiring a fixed kind. It uses tracker.Resolve for
// auto-detection and falls back to FindTracker + ResolveByKind for ambiguous
// get commands.
func ResolveAutoProvider(ctx context.Context, cmd *cobra.Command, keyHint string, allowFindFallback bool, deps Deps) (tracker.Provider, string, func(), error) {
	instances, err := deps.LoadInstances(".")
	if err != nil {
		return nil, "", nil, err
	}

	if inst := deps.InstanceFromFlags(cmd); inst != nil {
		instances = append(instances, *inst)
	}

	trackerName, _ := cmd.Root().PersistentFlags().GetString("tracker")

	// Try Resolve first (name-based or auto-detect).
	instance, err := tracker.Resolve(trackerName, instances, keyHint)
	if err != nil && allowFindFallback && trackerName == "" {
		// Ambiguous — fall back to FindTracker for get commands.
		result, findErr := tracker.FindTracker(ctx, keyHint, instances)
		if findErr != nil {
			// Return the original Resolve error — it's more informative.
			return nil, "", nil, err
		}
		instance, err = tracker.ResolveByKind(result.Provider, instances, "")
		if err != nil {
			return nil, "", nil, err
		}
	} else if err != nil {
		return nil, "", nil, err
	}

	safeFlag, _ := cmd.Root().PersistentFlags().GetBool("safe")
	p := instance.Provider
	if safeFlag || instance.Safe {
		p = tracker.NewSafeProvider(p, instance.Name)
	}

	auditPath := deps.AuditLogPath()
	ap, auditErr := tracker.NewAuditProvider(p, instance.Name, instance.Kind, auditPath)
	if auditErr != nil {
		fmt.Fprintln(os.Stderr, "warning: audit logging disabled:", auditErr)
		return p, instance.Kind, func() {}, nil
	}
	return ap, instance.Kind, func() { _ = ap.Close() }, nil
}
