package releaseartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildObserverArchiveIsDeterministicAndBounded(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, ObserverBinaryName)
	binary := []byte("deterministic observer fixture\n")
	if err := os.WriteFile(binaryPath, binary, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	first := filepath.Join(directory, "first.tar.gz")
	second := filepath.Join(directory, "second.tar.gz")
	if err := BuildObserverArchive(binaryPath, first); err != nil {
		t.Fatalf("first BuildObserverArchive() error = %v", err)
	}
	if err := os.Chtimes(binaryPath, time.Now(), time.Now()); err != nil {
		t.Fatalf("change binary times: %v", err)
	}
	if err := BuildObserverArchive(binaryPath, second); err != nil {
		t.Fatalf("second BuildObserverArchive() error = %v", err)
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("archives differ across source timestamps")
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(firstBytes))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil {
		t.Fatalf("read header: %v", err)
	}
	if header.Name != ObserverBinaryName || header.Mode != 0o755 ||
		header.Uid != 0 || header.Gid != 0 || !header.ModTime.Equal(time.Unix(0, 0)) {
		t.Fatalf("header = %+v", header)
	}
	extracted, err := io.ReadAll(tarReader)
	if err != nil || !bytes.Equal(extracted, binary) {
		t.Fatalf("archive payload mismatch: error=%v", err)
	}
	if _, err := tarReader.Next(); err != io.EOF {
		t.Fatalf("archive has additional entries: %v", err)
	}
}
