package releaseartifact

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const ObserverBinaryName = "hexroute-ingress-observer"

var ErrInvalidArtifact = errors.New("invalid release artifact")

func BuildObserverArchive(binaryPath, outputPath string) error {
	if !filepath.IsAbs(binaryPath) || !filepath.IsAbs(outputPath) ||
		filepath.Clean(binaryPath) != binaryPath || filepath.Clean(outputPath) != outputPath ||
		binaryPath == outputPath {
		return ErrInvalidArtifact
	}
	info, err := os.Lstat(binaryPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 {
		return ErrInvalidArtifact
	}
	if _, err := os.Lstat(outputPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return ErrInvalidArtifact
	}
	input, err := os.Open(binaryPath)
	if err != nil {
		return fmt.Errorf("open observer binary: %w", err)
	}
	defer input.Close()

	parent := filepath.Dir(outputPath)
	if parentInfo, err := os.Lstat(parent); err != nil || !parentInfo.IsDir() ||
		parentInfo.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidArtifact
	}
	temporary, err := os.CreateTemp(parent, ".observer-release-*")
	if err != nil {
		return fmt.Errorf("create observer archive: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect observer archive: %w", err)
	}

	gzipWriter, err := gzip.NewWriterLevel(temporary, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip stream: %w", err)
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{
		Name:       ObserverBinaryName,
		Mode:       0o755,
		Size:       info.Size(),
		ModTime:    time.Unix(0, 0).UTC(),
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
		Uid:        0,
		Gid:        0,
		Uname:      "root",
		Gname:      "root",
		Format:     tar.FormatUSTAR,
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write observer header: %w", err)
	}
	if _, err := io.Copy(tarWriter, input); err != nil {
		return fmt.Errorf("write observer binary: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar stream: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip stream: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync observer archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close observer archive: %w", err)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("commit observer archive: %w", err)
	}
	if err := os.Chmod(outputPath, 0o644); err != nil {
		return fmt.Errorf("publish observer archive: %w", err)
	}
	committed = true
	return nil
}
