package controller

// Updater that the Controller delegates to.
type Updater interface {
	// Start the ingress updater, returning immediately after it's started.
	Start() error
	// Stop the ingress updater. Blocks until the ingress updater stops or an error occurs.
	Stop() error
	// Update the ingress updater configuration.
	// Not thread safe, should only be called by a single go routine
	Update(IngressEntries) error
	// Health returns nil if healthy, otherwise an error. Should be fast to respond, as it
	// may be called often. Any long running checks should be done separately.
	Health() error
	// Readiness returns nil if ready, otherwise an error. Should be fast to respond, as it
	// may be called often. Any long running checks should be done separately.
	Readiness() error
}
