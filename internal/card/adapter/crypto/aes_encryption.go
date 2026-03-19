package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"payment-demo/internal/card/domain/port"
)

// AESEncrypter AES-256-GCM 加密实现
type AESEncrypter struct{}

var _ port.Encrypter = (*AESEncrypter)(nil)

func NewAESEncrypter() *AESEncrypter { return &AESEncrypter{} }

func (e *AESEncrypter) Encrypt(plaintext, dek []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce generation: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (e *AESEncrypter) Decrypt(ciphertext, dek []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}

func (e *AESEncrypter) HMAC(data, key []byte) (string, error) {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
