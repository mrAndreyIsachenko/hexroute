package safety

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type LifecycleOperation struct {
	Name        string
	WritePaths  []string
	RemovePaths []string
	Labels      []string
}

type ProductionBoundary struct {
	Paths  []string
	Labels []string
}

var (
	ErrInvalidLifecyclePath = errors.New("lifecycle path must be absolute and scoped")
	ErrProtectedPath        = errors.New("lifecycle operation overlaps a protected path")
	ErrProtectedLabel       = errors.New("lifecycle operation overlaps a protected label")
)

func ValidateLifecycleOperation(operation LifecycleOperation, boundary ProductionBoundary) error {
	for _, candidate := range append(append([]string{}, operation.WritePaths...), operation.RemovePaths...) {
		normalizedCandidate, err := normalizeLifecyclePath(candidate)
		if err != nil {
			return err
		}
		for _, protected := range boundary.Paths {
			normalizedProtected, err := normalizeLifecyclePath(protected)
			if err != nil {
				return fmt.Errorf("invalid protected path: %w", err)
			}
			if pathsOverlap(normalizedCandidate, normalizedProtected) {
				return fmt.Errorf("%w: operation=%q", ErrProtectedPath, operation.Name)
			}
		}
	}

	for _, candidate := range operation.Labels {
		if candidate == "" {
			return ErrProtectedLabel
		}
		for _, protected := range boundary.Labels {
			if strings.EqualFold(candidate, protected) {
				return fmt.Errorf("%w: operation=%q", ErrProtectedLabel, operation.Name)
			}
		}
	}
	return nil
}

func normalizeLifecyclePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrInvalidLifecyclePath
	}
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return "", ErrInvalidLifecyclePath
	}
	return strings.ToLower(cleaned), nil
}

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
