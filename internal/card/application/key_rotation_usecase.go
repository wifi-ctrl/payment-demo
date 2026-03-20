package application

import (
	"context"
	"fmt"
	"log"

	"payment-demo/internal/card/domain/port"
	"payment-demo/internal/card/domain/service"
)

// KeyRotationUseCase DEK 密钥轮换用例
type KeyRotationUseCase struct {
	repo       port.CardRepository
	keyMgr     port.KeyManager
	encryption *service.EncryptionService
}

func NewKeyRotationUseCase(
	repo port.CardRepository,
	keyMgr port.KeyManager,
	encryption *service.EncryptionService,
) *KeyRotationUseCase {
	return &KeyRotationUseCase{repo: repo, keyMgr: keyMgr, encryption: encryption}
}

// RotateAndMigrate 轮换 DEK 并迁移旧版本加密的卡数据。
// PCI Req 7/8: operatorID 标识执行者，不可为空，确保审计可追溯。
//
//	1. RotateDEK → 生成新 DEK 版本
//	2. 查所有使用旧版本加密的卡
//	3. 逐卡：DecryptPAN(old) → EncryptPAN(new) → ReEncrypt → Save
//	4. 迁移完成 → RetireDEK(old)
func (uc *KeyRotationUseCase) RotateAndMigrate(ctx context.Context, operatorID string) error {
	if operatorID == "" {
		return fmt.Errorf("key rotation requires operator identity (PCI Req 8)")
	}
	log.Printf("[KeyRotation] Initiated by operator=%s", operatorID)

	versions, err := uc.keyMgr.ListVersions()
	if err != nil {
		return err
	}

	var oldVersion int
	for _, v := range versions {
		if v.Status == "active" {
			oldVersion = v.Version
			break
		}
	}

	newVersion, err := uc.keyMgr.RotateDEK()
	if err != nil {
		return err
	}
	log.Printf("[KeyRotation] Rotated DEK: v%d → v%d", oldVersion, newVersion)

	cards, err := uc.repo.FindByKeyVersion(ctx, oldVersion)
	if err != nil {
		return err
	}

	var migrated, failed int
	for _, card := range cards {
		pan, err := uc.encryption.DecryptPAN(card.EncryptedPAN)
		if err != nil {
			log.Printf("[KeyRotation] Decrypt failed (card=%s): %v", card.ID, err)
			failed++
			continue
		}

		newEncrypted, err := uc.encryption.EncryptPANOnly(pan)
		if err != nil {
			log.Printf("[KeyRotation] ReEncrypt failed (card=%s): %v", card.ID, err)
			failed++
			continue
		}

		card.ReEncrypt(*newEncrypted)
		if err := uc.repo.Save(ctx, card); err != nil {
			log.Printf("[KeyRotation] Save failed (card=%s): %v", card.ID, err)
			failed++
			continue
		}
		migrated++
	}

	log.Printf("[KeyRotation] Migration complete: %d migrated, %d failed out of %d", migrated, failed, len(cards))

	if failed == 0 && len(cards) > 0 {
		if err := uc.keyMgr.RetireDEK(oldVersion); err != nil {
			log.Printf("[KeyRotation] RetireDEK(v%d) failed: %v", oldVersion, err)
		} else {
			log.Printf("[KeyRotation] DEK v%d retired", oldVersion)
		}
	}

	return nil
}
