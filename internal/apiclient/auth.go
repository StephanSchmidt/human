package apiclient

import "net/http"

// AuthFunc applies authentication to an outgoing HTTP request.
type AuthFunc func(req *http.Request)

// BasicAuth returns an AuthFunc that sets HTTP Basic Authentication.
func BasicAuth(user, password string) AuthFunc {
	return func(req *http.Request) {
		req.SetBasicAuth(user, password)
	}
}

// BearerToken returns an AuthFunc that sets a Bearer token Authorization header.
func BearerToken(token string) AuthFunc {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// HeaderAuth returns an AuthFunc that sets a custom header for authentication.
func HeaderAuth(name, value string) AuthFunc {
	return func(req *http.Request) {
		req.Header.Set(name, value)
	}
}

// NoAuth returns an AuthFunc that does not set any authentication.
func NoAuth() AuthFunc {
	return func(_ *http.Request) {}
}
