//go:build !darwin

package rootdaemon

// bootIdentity has no answer off Darwin. An empty identity refuses the read
// model at construction rather than letting it run on a boot it cannot name.
func bootIdentity() string { return "" }
