package daemon

// Request is sent from the client to the daemon (one JSON line per connection).
type Request struct {
	Version string            `json:"version"`
	Token   string            `json:"token"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// Response is sent from the daemon back to the client (one or more JSON lines per connection).
type Response struct {
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	ExitCode      int    `json:"exit_code"`
	AwaitCallback bool   `json:"await_callback,omitempty"`
	Callback      string `json:"callback,omitempty"`
}
