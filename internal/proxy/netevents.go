package proxy

// NetworkEventEmitter is the minimal interface the SNI proxy uses to
// surface ambient network activity (forwards, intercepts, blocks,
// failures) to any external observer. The daemon's NetworkEventStore
// satisfies this interface. The proxy package does not depend on the
// daemon package so the dependency direction stays one-way.
type NetworkEventEmitter interface {
	// Emit records a single network decision or failure.
	// Source is one of "proxy", "oauth", "fail".
	// Status is a short human readable decision: "forward", "intercept",
	// "block", "no-sni", "parse-fail", "dial-fail", "callback".
	// Host may be empty for failures that happen before SNI extraction.
	Emit(source, status, host string)
}
