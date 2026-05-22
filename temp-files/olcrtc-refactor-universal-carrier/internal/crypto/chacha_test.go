package crypto

import (
	"bytes"
	"errors"
	"testing"
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
