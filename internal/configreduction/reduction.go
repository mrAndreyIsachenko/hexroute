// Package configreduction answers one question about two configurations: what
// would be lost by replacing one with the other.
//
// It exists because validity could not answer it. On 2026-09-10 two daemons
// were installed with configurations that were valid, accepted by every check
// in the path, and carried neither the pinned key that lets a runtime read its
// policy nor — for root — the named service that is its whole rescue
// capability. A signed generation stopped being in force and nothing said so,
// because nothing had compared the new file with the old one.
package configreduction

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
)

var ErrInvalidConfiguration = errors.New("configuration is not a JSON object")

// Reductions names the settings the candidate would drop, as key paths into
// the installed configuration, sorted.
//
// Only the presence of object keys is compared. Values are deliberately not:
// a configuration legitimately changes an interval, an address or a rotated
// key on every install, and comparing values would make the answer noise. What
// it should not do unannounced is stop carrying a setting it used to carry.
//
// Arrays are compared as single values rather than descended into. A shorter
// list of routes or endpoints is a real reduction and a legitimate one, and
// indexing into arrays would report a reordering as a loss — which would make
// the refusal too noisy to be worth enforcing.
func Reductions(installed, candidate []byte) ([]string, error) {
	was, err := object(installed)
	if err != nil {
		return nil, err
	}
	now, err := object(candidate)
	if err != nil {
		return nil, err
	}
	lost := missing(was, now, "")
	sort.Strings(lost)
	return lost, nil
}

func object(raw []byte) (map[string]any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrInvalidConfiguration
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidConfiguration
	}
	return fields, nil
}

// missing walks the installed configuration and names what the candidate does
// not have.
//
// A key whose value stopped being an object is reported once, for the key
// itself, and its former contents are not enumerated: a reader told that
// `policy_control` is gone does not also need to be told each thing inside it
// is gone. A key that is an object on both sides is descended into, because
// that is where the pinned key and the compatibility live.
func missing(was, now map[string]any, prefix string) []string {
	lost := make([]string, 0, len(was))
	for key, previous := range was {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		current, present := now[key]
		if !present {
			lost = append(lost, path)
			lost = append(lost, within(previous, path)...)
			continue
		}
		previousFields, previousIsObject := previous.(map[string]any)
		currentFields, currentIsObject := current.(map[string]any)
		if previousIsObject && currentIsObject {
			lost = append(lost, missing(previousFields, currentFields, path)...)
			continue
		}
		if previousIsObject {
			// It was a set of settings and is now a single value. Everything it
			// held is gone, whatever the new value says.
			lost = append(lost, within(previous, path)...)
		}
	}
	return lost
}

// within names everything an absent object used to hold, so that the answer
// says which settings were lost rather than only which branch of the file.
func within(value any, prefix string) []string {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	lost := make([]string, 0, len(fields))
	for key, held := range fields {
		path := prefix + "." + key
		lost = append(lost, path)
		lost = append(lost, within(held, path)...)
	}
	return lost
}

// Compare reads two configurations from disk and answers the same question.
//
// It is here rather than in each daemon because both ask it, and because the
// answer must not differ between them: an installer that guards one domain and
// not the other guards nothing.
func Compare(installedPath, candidatePath string) ([]string, error) {
	installed, err := os.ReadFile(installedPath)
	if err != nil {
		return nil, err
	}
	candidate, err := os.ReadFile(candidatePath)
	if err != nil {
		return nil, err
	}
	return Reductions(installed, candidate)
}
