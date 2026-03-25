package dispatch

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingNotifier struct {
	calls []string
	err   error
}

func (r *recordingNotifier) Notify(_ context.Context, _ int64, text string) error {
	r.calls = append(r.calls, text)
	return r.err
}

func TestCompositeNotifier_CallsBoth(t *testing.T) {
	n1 := &recordingNotifier{}
	n2 := &recordingNotifier{}
	composite := &CompositeNotifier{Notifiers: []Notifier{n1, n2}}

	err := composite.Notify(context.Background(), 42, "hello")
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, n1.calls)
	assert.Equal(t, []string{"hello"}, n2.calls)
}

func TestCompositeNotifier_FirstErrorReturned(t *testing.T) {
	n1 := &recordingNotifier{err: fmt.Errorf("n1 failed")}
	n2 := &recordingNotifier{}
	composite := &CompositeNotifier{Notifiers: []Notifier{n1, n2}}

	err := composite.Notify(context.Background(), 42, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n1 failed")
	// n2 should still be called.
	assert.Equal(t, []string{"hello"}, n2.calls)
}

func TestCompositeNotifier_SecondErrorReturned(t *testing.T) {
	n1 := &recordingNotifier{}
	n2 := &recordingNotifier{err: fmt.Errorf("n2 failed")}
	composite := &CompositeNotifier{Notifiers: []Notifier{n1, n2}}

	err := composite.Notify(context.Background(), 42, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n2 failed")
}

func TestCompositeNotifier_Empty(t *testing.T) {
	composite := &CompositeNotifier{Notifiers: nil}

	err := composite.Notify(context.Background(), 42, "hello")
	require.NoError(t, err)
}
