package service

import (
	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

// EncryptionService 卡数据加密领域服务
type EncryptionService struct {
	keyMgr    port.KeyManager
	encrypter port.Encrypter
}

func NewEncryptionService(keyMgr port.KeyManager, enc port.Encrypter) *EncryptionService {
	return &EncryptionService{keyMgr: keyMgr, encrypter: enc}
}

// EncryptPANOnly 仅加密 PAN（不计算 HMAC）
func (s *EncryptionService) EncryptPANOnly(pan string) (*model.EncryptedPAN, error) {
	dek, version, err := s.keyMgr.CurrentDEK()
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.encrypter.Encrypt([]byte(pan), dek)
	if err != nil {
		return nil, err
	}
	return &model.EncryptedPAN{
		Ciphertext: ciphertext,
		KeyVersion: version,
	}, nil
}

// DecryptPAN 解密 PAN（按 KeyVersion 选择正确的 DEK）
func (s *EncryptionService) DecryptPAN(encrypted model.EncryptedPAN) (string, error) {
	dek, err := s.keyMgr.DEKByVersion(encrypted.KeyVersion)
	if err != nil {
		return "", err
	}
	plaintext, err := s.encrypter.Decrypt(encrypted.Ciphertext, dek)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ComputePANHash 计算 HMAC 哈希（令牌化时查重用）
func (s *EncryptionService) ComputePANHash(pan string) (model.PANHash, error) {
	hmacKey, err := s.keyMgr.HMACKey()
	if err != nil {
		return "", err
	}
	hash, err := s.encrypter.HMAC([]byte(pan), hmacKey)
	if err != nil {
		return "", err
	}
	return model.PANHash(hash), nil
}
