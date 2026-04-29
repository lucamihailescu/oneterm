package utils

import (
	"strings"
	"testing"
)

// TestEncryptAES_RoundTrip verifies that GCM-encrypted data round-trips and
// produces a different ciphertext on each call (random nonce).
func TestEncryptAES_RoundTrip(t *testing.T) {
	plaintext := "123456789abcdefghijklmnopqrstuvwxyz"

	ct1 := EncryptAES(plaintext)
	ct2 := EncryptAES(plaintext)

	if ct1 == ct2 {
		t.Fatalf("expected different ciphertexts due to random nonce; got identical %q", ct1)
	}
	if !strings.HasPrefix(ct1, "v1:") {
		t.Fatalf("expected ciphertext to use v1 (GCM) format, got %q", ct1)
	}
	if got := DecryptAES(ct1); got != plaintext {
		t.Errorf("DecryptAES(v1) = %q, want %q", got, plaintext)
	}
	if got := DecryptAES(ct2); got != plaintext {
		t.Errorf("DecryptAES(v1) = %q, want %q", got, plaintext)
	}
}

// TestDecryptAES_LegacyCBC ensures historical CBC+static-IV ciphertexts in the
// database continue to decrypt after the GCM migration.
func TestDecryptAES_LegacyCBC(t *testing.T) {
	const legacy = "hrr23HSXrZEOw5haacoj32QJLrHdpj42jaQcPVRf9AI8SzeSdWJhzTrYgsOgmNoN"
	const plaintext = "123456789abcdefghijklmnopqrstuvwxyz"

	if got := DecryptAES(legacy); got != plaintext {
		t.Errorf("legacy DecryptAES = %q, want %q", got, plaintext)
	}
}

// TestDecryptAES_Empty handles malformed input gracefully (used to be a panic).
func TestDecryptAES_Empty(t *testing.T) {
	if got := DecryptAES(""); got != "" {
		t.Errorf("DecryptAES(\"\") = %q, want empty string", got)
	}
	if got := DecryptAES("not-base64-!!!"); got != "" {
		t.Errorf("DecryptAES(garbage) = %q, want empty string", got)
	}
}
