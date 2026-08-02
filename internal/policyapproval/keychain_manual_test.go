//go:build darwin

package policyapproval

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"testing"
	"time"
)

func TestManualKeychainUserPresence(t *testing.T) {
	if os.Getenv("HEXROUTE_POLICY_TOUCH_ID_TEST") != "1" {
		t.Skip("set HEXROUTE_POLICY_TOUCH_ID_TEST=1 for the manual user-presence gate")
	}
	service := os.Getenv("HEXROUTE_POLICY_KEYCHAIN_SERVICE")
	account := os.Getenv("HEXROUTE_POLICY_KEYCHAIN_ACCOUNT")
	encodedPublicKey := os.Getenv("HEXROUTE_POLICY_PUBLIC_KEY")
	publicKey, err := base64.RawStdEncoding.DecodeString(encodedPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		t.Fatal("invalid manual public-key configuration")
	}
	signer, err := NewKeychainSigner(ExecKeychainRunner{}, KeychainConfig{
		Service: service, Account: account, PublicKey: ed25519.PublicKey(publicKey),
		RequireUserPresence: true, PromptTimeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("hexroute manual user-presence policy approval test")
	signature, err := signer.Sign(message)
	if err != nil || !ed25519.Verify(publicKey, message, signature) {
		t.Fatal("manual user-presence signature failed")
	}
}
