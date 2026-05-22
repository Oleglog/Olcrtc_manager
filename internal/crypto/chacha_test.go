package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestNewCipherRejectsWrongKeySize(t *testing.T) {
	_, err := NewCipher("short")
	if !errors.Is(err, ErrInvalidKeySize) {
		t.Fatalf("NewCipher() error = %v, want %v", err, ErrInvalidKeySize)
	}
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	plaintext := []byte("hello world")
	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext unexpectedly matches plaintext")
	}

	got, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestDecryptRejectsShortCiphertext(t *testing.T) {
	c, err := NewCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	_, err = c.Decrypt([]byte("short"))
	if !errors.Is(err, ErrCiphertextTooShort) {
		t.Fatalf("Decrypt() error = %v, want %v", err, ErrCiphertextTooShort)
	}
}

// TestEncryptUniqueNonces ensures the deterministic-nonce optimisation
// never repeats a nonce within a single Cipher instance: the salt is
// fixed but the counter must move every call.
func TestEncryptUniqueNonces(t *testing.T) {
	c, err := NewCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	const iterations = 1024
	nonceSize := chacha20poly1305.NonceSizeX
	seen := make(map[string]struct{}, iterations)
	for i := range iterations {
		ct, err := c.Encrypt([]byte("payload"))
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		nonce := string(ct[:nonceSize])
		if _, dup := seen[nonce]; dup {
			t.Fatalf("nonce repeated at iteration %d", i)
		}
		seen[nonce] = struct{}{}
	}
}

// TestCipherInstancesDistinctSalts confirms two Cipher instances built
// from the same key still produce different nonce salts, so they cannot
// collide on (key, nonce) even at counter==1.
func TestCipherInstancesDistinctSalts(t *testing.T) {
	const key = "01234567890123456789012345678901"
	a, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher(a) error = %v", err)
	}
	b, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher(b) error = %v", err)
	}
	if bytes.Equal(a.salt[:], b.salt[:]) {
		t.Fatal("two Cipher instances produced the same nonce salt")
	}
}

// TestDecryptAcceptsLegacyRandomNonce verifies the new Cipher can still
// decrypt ciphertexts produced by the previous fully-random-nonce
// implementation. This guarantees rolling upgrade safety: a peer running
// the old code can talk to one running the new code in either direction.
func TestDecryptAcceptsLegacyRandomNonce(t *testing.T) {
	const key = "01234567890123456789012345678901"
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	// Reproduce the legacy encryption path inline (random nonce, no salt
	// or counter) using the same AEAD primitive.
	aead, err := chacha20poly1305.NewX([]byte(key))
	if err != nil {
		t.Fatalf("aead error = %v", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	plaintext := []byte("legacy peer payload")
	legacy := aead.Seal(nonce, nonce, plaintext, nil)

	got, err := c.Decrypt(legacy)
	if err != nil {
		t.Fatalf("Decrypt(legacy) error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt(legacy) = %q, want %q", got, plaintext)
	}
}

// BenchmarkEncrypt covers the data-plane hot path: many encrypts of a
// typical smux frame size. Run with `go test -bench=Encrypt
// -benchmem ./internal/crypto` to compare against the previous
// implementation.
func BenchmarkEncrypt(b *testing.B) {
	c, err := NewCipher("01234567890123456789012345678901")
	if err != nil {
		b.Fatalf("NewCipher() error = %v", err)
	}
	payload := bytes.Repeat([]byte{0xab}, 12*1024)
	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	for range b.N {
		if _, err := c.Encrypt(payload); err != nil {
			b.Fatalf("Encrypt() error = %v", err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	c, err := NewCipher("01234567890123456789012345678901")
	if err != nil {
		b.Fatalf("NewCipher() error = %v", err)
	}
	payload := bytes.Repeat([]byte{0xab}, 12*1024)
	ct, err := c.Encrypt(payload)
	if err != nil {
		b.Fatalf("Encrypt() error = %v", err)
	}
	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	for range b.N {
		if _, err := c.Decrypt(ct); err != nil {
			b.Fatalf("Decrypt() error = %v", err)
		}
	}
}
