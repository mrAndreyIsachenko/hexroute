//go:build !darwin

package userdaemon

// bootIdentity has no answer off Darwin, which leaves the publisher disabled.
func bootIdentity() string { return "" }
