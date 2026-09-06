package configversion

// KeyPrefix is where versions live in the object store. A host reads under
// this prefix and nothing else, so a credential scoped to it can read versions
// and nothing else either.
const KeyPrefix = "versions"

// ObjectKey is where one version lives. It is derived from what the operator
// signed, so a retry after a failure writes the same object rather than a
// second one.
func ObjectKey(statement Statement) string {
	return prefixFor(statement.TargetKind, statement.TargetKey) +
		statement.VersionLabel + ".json"
}

// CurrentKey is the one key a host reads. It is how a node learns a version
// exists without the cloud telling it: the node asks, on its own schedule, and
// what it finds there is either a version it can verify or nothing it will act
// on.
//
// The pointer is not an instruction. It carries the same signed statement as
// the labelled object, so a host that read it still decides for itself.
//
// Naming lives here rather than beside the publisher because the host has to
// derive the same key without linking anything the publisher needs. An agent
// that carried a database driver to work out a filename would be carrying it
// onto someone else's machine.
func CurrentKey(kind string, key string) string {
	return prefixFor(kind, key) + "current.json"
}

func prefixFor(kind string, key string) string {
	return KeyPrefix + "/" + kind + "/" + key + "/"
}
