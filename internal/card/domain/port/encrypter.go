package port

// Encrypter PAN 加密/解密/HMAC 能力端口（被驱动端口）
type Encrypter interface {
	Encrypt(plaintext []byte, dek []byte) ([]byte, error)
	Decrypt(ciphertext []byte, dek []byte) ([]byte, error)
	HMAC(data []byte, key []byte) (string, error)
}
