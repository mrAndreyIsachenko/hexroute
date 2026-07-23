package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const keyFileSchema = "hexroute.node-key.v1"

type Key struct {
	NodeID     metadata.UUID
	KeyID      metadata.UUID
	privateKey ed25519.PrivateKey
}

type keyFile struct {
	Schema     string        `json:"schema"`
	NodeID     metadata.UUID `json:"node_id"`
	KeyID      metadata.UUID `json:"key_id"`
	PrivateKey string        `json:"private_key"`
}

var (
	ErrInvalidKeyFile  = errors.New("invalid node key file")
	ErrInsecureKeyFile = errors.New("node key file permissions are not private")
	ErrKeyFileExists   = errors.New("node key file already exists")
)

func GenerateFile(path string, nodeID metadata.UUID, random io.Reader) (Key, error) {
	if _, err := metadata.ParseUUID(string(nodeID)); err != nil {
		return Key{}, err
	}
	if !filepath.IsAbs(path) {
		return Key{}, ErrInvalidKeyFile
	}
	if random == nil {
		random = rand.Reader
	}
	publicKey, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		return Key{}, err
	}
	_ = publicKey
	keyID, err := metadata.NewUUID(random)
	if err != nil {
		return Key{}, err
	}
	key := Key{
		NodeID:     nodeID,
		KeyID:      keyID,
		privateKey: privateKey,
	}
	if err := writeKeyFile(path, key); err != nil {
		return Key{}, err
	}
	return key, nil
}

func LoadFile(path string) (Key, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Key{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return Key{}, ErrInsecureKeyFile
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Key{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var stored keyFile
	if err := decoder.Decode(&stored); err != nil {
		return Key{}, ErrInvalidKeyFile
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Key{}, ErrInvalidKeyFile
	}
	if stored.Schema != keyFileSchema {
		return Key{}, ErrInvalidKeyFile
	}
	if _, err := metadata.ParseUUID(string(stored.NodeID)); err != nil {
		return Key{}, ErrInvalidKeyFile
	}
	if _, err := metadata.ParseUUID(string(stored.KeyID)); err != nil {
		return Key{}, ErrInvalidKeyFile
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(stored.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return Key{}, ErrInvalidKeyFile
	}
	return Key{
		NodeID:     stored.NodeID,
		KeyID:      stored.KeyID,
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

func (key Key) PublicKey() ed25519.PublicKey {
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return nil
	}
	publicKey := key.privateKey.Public().(ed25519.PublicKey)
	return append(ed25519.PublicKey(nil), publicKey...)
}

func writeKeyFile(path string, key Key) error {
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return ErrInvalidKeyFile
	}
	parent := filepath.Dir(path)
	if err := ensurePrivateDirectory(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return ErrKeyFileExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	encoded, err := json.Marshal(keyFile{
		Schema:     keyFileSchema,
		NodeID:     key.NodeID,
		KeyID:      key.KeyID,
		PrivateKey: base64.RawStdEncoding.EncodeToString(key.privateKey),
	})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".node-key-*")
	if err != nil {
		return fmt.Errorf("create node key: %w", err)
	}
	tempPath := file.Name()
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure node key: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write node key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync node key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close node key: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("commit node key: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	success = true
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create node key directory: %w", err)
		}
	case err != nil:
		return err
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return ErrInsecureKeyFile
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure node key directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
