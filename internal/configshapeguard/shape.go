// Package configshapeguard reads the shape a decoder accepts from the decoder
// itself, so an example configuration can be held to it.
//
// The repository held no record of the configuration its daemons run on:
// measured 2026-10-08, the root example carried 46 settings against the 112 the
// installed configuration held, with no tunnel_supervision and no
// policy_control at all, so the whole tunnel executor existed on one machine
// only. A list of settings written beside the decoder is what was not updated,
// so this takes them from the wire types instead.
package configshapeguard

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

var (
	// ErrUnresolvable is a field the walk cannot enter. It is refused rather
	// than skipped: a field passed over silently is a setting that need never
	// be recorded, which is the hole this package exists to close.
	ErrUnresolvable = errors.New("configshapeguard: a field's type cannot be walked")
	// ErrCycle is a wire type that reaches itself.
	ErrCycle = errors.New("configshapeguard: the type reaches itself")
	// ErrEmptyList is a list in a document with no element. A list with
	// nothing in it records a key and no shape.
	ErrEmptyList = errors.New("configshapeguard: a list carries no element")
)

var listIndex = regexp.MustCompile(`\[[0-9]+\]`)

// Settings is every json path the wire type accepts, including the paths of the
// types it reaches into. Optional fields are included: an optional setting is
// the kind that rots, and both blocks missing from the examples were optional.
func Settings(wire reflect.Type) ([]string, error) {
	found := map[string]struct{}{}
	if err := walk(wire, "", map[reflect.Type]struct{}{}, found); err != nil {
		return nil, err
	}
	return sorted(found), nil
}

func walk(
	wire reflect.Type,
	prefix string,
	entered map[reflect.Type]struct{},
	found map[string]struct{},
) error {
	wire = elem(wire)
	if wire.Kind() != reflect.Struct {
		return nil
	}
	if _, seen := entered[wire]; seen {
		return fmt.Errorf("%w: %s at %q", ErrCycle, wire, prefix)
	}
	entered[wire] = struct{}{}
	defer delete(entered, wire)

	for index := 0; index < wire.NumField(); index++ {
		field := wire.Field(index)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			if field.Anonymous {
				if err := walk(field.Type, prefix, entered, found); err != nil {
					return err
				}
				continue
			}
			if field.IsExported() {
				return fmt.Errorf(
					"%w: %s.%s has no json name", ErrUnresolvable, wire, field.Name)
			}
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		resolved := elem(field.Type)
		switch resolved.Kind() {
		case reflect.Interface, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
			return fmt.Errorf("%w: %q is %s", ErrUnresolvable, path, resolved.Kind())
		}
		found[path] = struct{}{}
		if resolved.Kind() == reflect.Struct {
			if err := walk(resolved, path, entered, found); err != nil {
				return err
			}
		}
	}
	return nil
}

// elem reaches through pointers and lists to the type that carries the shape.
func elem(wire reflect.Type) reflect.Type {
	for {
		switch wire.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			wire = wire.Elem()
		default:
			return wire
		}
	}
}

// DocumentKeys is every json path a decoded document carries, with list
// indices removed so one element stands for the list's shape.
func DocumentKeys(document any) ([]string, error) {
	found := map[string]struct{}{}
	if err := keys(document, "", found); err != nil {
		return nil, err
	}
	return sorted(found), nil
}

func keys(node any, prefix string, found map[string]struct{}) error {
	switch typed := node.(type) {
	case map[string]any:
		for name, value := range typed {
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			found[path] = struct{}{}
			if err := keys(value, path, found); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) == 0 {
			return fmt.Errorf("%w: %q", ErrEmptyList, prefix)
		}
		for _, value := range typed {
			if err := keys(value, prefix, found); err != nil {
				return err
			}
		}
	}
	return nil
}

// Compare answers what the example omits and what it carries beyond the type.
// It compares names only and never a value, so it cannot become a reason to
// write a live one into the repository.
func Compare(settings, documentKeys []string) (missing, extra []string) {
	accepted := map[string]struct{}{}
	for _, setting := range settings {
		accepted[setting] = struct{}{}
	}
	present := map[string]struct{}{}
	for _, key := range documentKeys {
		present[listIndex.ReplaceAllString(key, "")] = struct{}{}
	}
	for setting := range accepted {
		if _, ok := present[setting]; !ok {
			missing = append(missing, setting)
		}
	}
	for key := range present {
		if _, ok := accepted[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
