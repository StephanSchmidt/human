package apitest

import "net/http"

// ErrDoer is a mock HTTPDoer that returns a fixed error.
type ErrDoer struct {
	Err error
}

// Do implements apiclient.HTTPDoer.
func (d *ErrDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.Err
}

// NilDoer is a mock HTTPDoer that returns a nil response.
type NilDoer struct{}

// Do implements apiclient.HTTPDoer.
func (*NilDoer) Do(*http.Request) (*http.Response, error) {
	return nil, nil
}
