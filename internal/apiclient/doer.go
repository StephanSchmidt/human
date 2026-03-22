package apiclient

import "net/http"

// HTTPDoer abstracts HTTP request execution for testability and to decouple
// from the concrete *http.Client type.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
