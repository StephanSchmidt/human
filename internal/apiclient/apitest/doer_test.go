package apitest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrDoer(t *testing.T) {
	d := &ErrDoer{Err: fmt.Errorf("boom")}
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	resp, err := d.Do(req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, "boom", err.Error())
}

func TestNilDoer(t *testing.T) {
	d := &NilDoer{}
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	resp, err := d.Do(req)
	assert.Nil(t, resp)
	assert.NoError(t, err)
}
