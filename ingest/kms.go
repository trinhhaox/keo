package ingest

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

// KMSClient là interface trừu tượng hóa giao tiếp với Key Management Service (như AWS KMS hoặc GCP KMS).
type KMSClient interface {
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}

// LocalKMS là implementation dùng chung cho môi trường dev.
// KHÔNG DÙNG trên production nếu không bắt buộc. Trên prod nên dùng GCP/AWS KMS.
type LocalKMS struct {
	aead cipher.AEAD
}

func NewLocalKMS(key32 []byte) (*LocalKMS, error) {
	block, err := aes.NewCipher(key32)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &LocalKMS{aead: aead}, nil
}

func (k *LocalKMS) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *LocalKMS) Decrypt(ctx context.Context, ct []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(ct) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return k.aead.Open(nil, ct[:ns], ct[ns:], nil)
}

// EnvelopeCipher sử dụng KMS để mã hóa phong bì (Envelope Encryption).
// DEK (Data Encryption Key) được sinh ngẫu nhiên cho mỗi lần Encrypt.
// Cấu trúc lưu trữ: [Kích thước Encrypted DEK (2 bytes)] + [Encrypted DEK] + [Encrypted Token (nonce || ciphertext)]
type EnvelopeCipher struct {
	kms KMSClient
}

func NewEnvelopeCipher(kms KMSClient) *EnvelopeCipher {
	return &EnvelopeCipher{kms: kms}
}

func (c *EnvelopeCipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	// 1. Sinh DEK (32 bytes cho AES-256)
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}

	// 2. Mã hóa token bằng DEK
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("DEK cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("DEK gcm: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	encryptedToken := aead.Seal(nonce, nonce, plaintext, nil)

	// 3. Mã hóa DEK bằng KEK (KMS)
	encryptedDEK, err := c.kms.Encrypt(ctx, dek)
	if err != nil {
		return nil, fmt.Errorf("encrypt DEK via KMS: %w", err)
	}

	if len(encryptedDEK) > 65535 {
		return nil, fmt.Errorf("encrypted DEK too large")
	}

	// 4. Ghép chuỗi: [len(encryptedDEK)] + [encryptedDEK] + [encryptedToken]
	out := make([]byte, 2+len(encryptedDEK)+len(encryptedToken))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(encryptedDEK)))
	copy(out[2:], encryptedDEK)
	copy(out[2+len(encryptedDEK):], encryptedToken)

	return out, nil
}

func (c *EnvelopeCipher) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 2 {
		return nil, errors.New("invalid envelope length")
	}

	// 1. Tách cấu trúc
	encDEKLen := int(binary.BigEndian.Uint16(ciphertext[0:2]))
	if len(ciphertext) < 2+encDEKLen {
		return nil, errors.New("invalid envelope format")
	}

	encryptedDEK := ciphertext[2 : 2+encDEKLen]
	encryptedToken := ciphertext[2+encDEKLen:]

	// 2. Giải mã DEK qua KMS
	dek, err := c.kms.Decrypt(ctx, encryptedDEK)
	if err != nil {
		return nil, fmt.Errorf("decrypt DEK via KMS: %w", err)
	}

	// 3. Giải mã token bằng DEK
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("DEK cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("DEK gcm: %w", err)
	}

	ns := aead.NonceSize()
	if len(encryptedToken) < ns {
		return nil, errors.New("token ciphertext too short")
	}

	return aead.Open(nil, encryptedToken[:ns], encryptedToken[ns:], nil)
}
