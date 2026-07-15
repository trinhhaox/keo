package ingest

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestEnvelopeCipher(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	localKMS, err := NewLocalKMS(key)
	if err != nil {
		t.Fatal(err)
	}

	cipher := NewEnvelopeCipher(localKMS)
	ctx := context.Background()
	
	plaintext := []byte("secret-access-token-from-strava")

	// 1. Encrypt
	ciphertext1, err := cipher.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// 2. Encrypt again - should yield different ciphertext because of new DEK
	ciphertext2, err := cipher.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}
	
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Fatalf("Envelope Encryption failed to use random DEK, ciphertexts match")
	}

	// 3. Decrypt
	decrypted1, err := cipher.Decrypt(ctx, ciphertext1)
	if err != nil {
		t.Fatalf("Decrypt 1 failed: %v", err)
	}
	
	if !bytes.Equal(decrypted1, plaintext) {
		t.Fatalf("Decrypted does not match plaintext. Got %s, want %s", decrypted1, plaintext)
	}

	decrypted2, err := cipher.Decrypt(ctx, ciphertext2)
	if err != nil {
		t.Fatalf("Decrypt 2 failed: %v", err)
	}
	if !bytes.Equal(decrypted2, plaintext) {
		t.Fatalf("Decrypted does not match plaintext. Got %s, want %s", decrypted2, plaintext)
	}
}
