package application

import (
	"context"
	"log"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
	"payment-demo/internal/card/domain/service"
)

// CardUseCase 卡管理用例编排层
type CardUseCase struct {
	repo       port.CardRepository
	vault      port.CardVault
	encryption *service.EncryptionService
}

func NewCardUseCase(
	repo port.CardRepository,
	vault port.CardVault,
	encryption *service.EncryptionService,
) *CardUseCase {
	return &CardUseCase{repo: repo, vault: vault, encryption: encryption}
}

// ── 令牌化 ──────────────────────────────────────────────────────

// TokenizeRequest 令牌化入参
type TokenizeRequest struct {
	UserID         string
	PAN            string
	ExpiryMonth    int
	ExpiryYear     int
	CVV            string
	CardholderName string
}

// Tokenize 卡令牌化：校验 → HMAC 查重 → 加密 → 缓存 → 返回临时 token
func (uc *CardUseCase) Tokenize(ctx context.Context, req TokenizeRequest) (*port.TokenizeResult, error) {
	brand := service.IdentifyBrand(req.PAN)
	if !service.LuhnCheck(req.PAN) {
		return nil, model.ErrCardTokenInvalid
	}
	last4 := req.PAN[len(req.PAN)-4:]

	panHash, err := uc.encryption.ComputePANHash(req.PAN)
	if err != nil {
		return nil, model.ErrEncryptionFailed
	}

	existing, err := uc.repo.FindActiveByUserAndPANHash(ctx, req.UserID, panHash)
	if err == nil && existing != nil {
		existingID := existing.ID
		return &port.TokenizeResult{
			CardToken: nil,
			Mask: model.CardMask{
				Last4:       last4,
				Brand:       string(brand),
				ExpireMonth: req.ExpiryMonth,
				ExpireYear:  req.ExpiryYear,
			},
			Brand:          brand,
			ExistingCardID: &existingID,
		}, nil
	}

	encrypted, err := uc.encryption.EncryptPANOnly(req.PAN)
	if err != nil {
		return nil, model.ErrEncryptionFailed
	}

	mask := model.CardMask{
		Last4:       last4,
		Brand:       string(brand),
		ExpireMonth: req.ExpiryMonth,
		ExpireYear:  req.ExpiryYear,
	}
	cardToken, err := uc.vault.CacheTokenizedCard(ctx, port.CachedCardData{
		EncryptedPAN: *encrypted,
		PANHash:      panHash,
		Mask:         mask,
		Holder: model.CardHolder{
			Name: req.CardholderName,
		},
		UserID: req.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &port.TokenizeResult{
		CardToken: &cardToken,
		Mask:      mask,
		Brand:     brand,
	}, nil
}

// ResolveForGatewayResult 支付 Authorize 前由 card 服务返回的权威卡信息 + 网关 token
type ResolveForGatewayResult struct {
	Last4        string
	Brand        string
	GatewayToken string
}

// ResolveCardForGateway 校验 ct_* 归属，从 vault 取密文并解密 PAN，写入新的 RawPAN 临时 token 供网关使用。
// 原始 ct_* 不消费，留给 Capture 后 BindCardFromToken。
// 若缓存条目已是 RawPAN 形态（如 PrepareOneTimeToken），则直接复用同一 token。
func (uc *CardUseCase) ResolveCardForGateway(ctx context.Context, cardToken, userID string) (*ResolveForGatewayResult, error) {
	cached, err := uc.vault.PeekCachedCard(ctx, cardToken, userID)
	if err != nil {
		return nil, err
	}
	last4 := cached.Mask.Last4
	brand := cached.Mask.Brand

	if cached.RawPAN != "" {
		return &ResolveForGatewayResult{
			Last4:        last4,
			Brand:        brand,
			GatewayToken: cardToken,
		}, nil
	}
	if len(cached.EncryptedPAN.Ciphertext) == 0 {
		return nil, model.ErrCardTokenInvalid
	}
	pan, err := uc.encryption.DecryptPAN(cached.EncryptedPAN)
	if err != nil {
		return nil, model.ErrDecryptionFailed
	}
	gwTok, err := uc.vault.CacheTokenizedCard(ctx, port.CachedCardData{
		RawPAN:  pan,
		PANHash: cached.PANHash,
		Mask:    cached.Mask,
		Holder:  cached.Holder,
		UserID:  cached.UserID,
	})
	if err != nil {
		return nil, err
	}
	return &ResolveForGatewayResult{
		Last4:        last4,
		Brand:        brand,
		GatewayToken: gwTok,
	}, nil
}

// ── 支付成功后绑卡 ──────────────────────────────────────────────

// BindFromTokenRequest 从临时 token 创建持久化卡
type BindFromTokenRequest struct {
	CardToken    string
	ChannelToken string
	Channel      string
	ShopperRef   string
}

// BindCardFromToken 支付成功后持久化卡
func (uc *CardUseCase) BindCardFromToken(ctx context.Context, req BindFromTokenRequest) (*model.SavedCard, error) {
	cached, err := uc.vault.ConsumeCardToken(ctx, req.CardToken)
	if err != nil {
		return nil, err
	}

	existing, err := uc.repo.FindActiveByUserAndPANHash(ctx, cached.UserID, cached.PANHash)
	if err == nil && existing != nil {
		if req.ChannelToken != "" {
			existing.StoreChannelToken(req.Channel, req.ChannelToken, req.ShopperRef)
			if saveErr := uc.repo.Save(ctx, existing); saveErr != nil {
				log.Printf("[CardUseCase] BindCardFromToken: channel_token save failed (card=%s, channel=%s): %v",
					existing.ID, req.Channel, saveErr)
				return nil, saveErr
			}
		}
		return existing, nil
	}

	card := model.NewSavedCard(
		cached.UserID,
		cached.EncryptedPAN,
		cached.PANHash,
		cached.Mask,
		cached.Holder,
	)

	if req.ChannelToken != "" {
		card.StoreChannelToken(req.Channel, req.ChannelToken, req.ShopperRef)
	}

	existingDefault, _ := uc.repo.FindDefaultByUserID(ctx, cached.UserID)
	if existingDefault == nil {
		card.BindAsDefault()
	} else {
		card.Bind()
	}

	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}

	uc.publishEvents(card)
	return card, nil
}

// ── StoreChannelToken ────────────────────────────────────────────

func (uc *CardUseCase) StoreChannelToken(
	ctx context.Context,
	cardID model.SavedCardID,
	channel, token, shopperRef string,
) error {
	card, err := uc.repo.FindByID(ctx, cardID)
	if err != nil {
		return err
	}
	card.StoreChannelToken(channel, token, shopperRef)
	if err := uc.repo.Save(ctx, card); err != nil {
		return err
	}
	uc.publishEvents(card)
	return nil
}

// ── PrepareOneTimeToken ──────────────────────────────────────────

func (uc *CardUseCase) PrepareOneTimeToken(ctx context.Context, cardID model.SavedCardID, userID string) (string, error) {
	card, err := uc.repo.FindByID(ctx, cardID)
	if err != nil {
		return "", err
	}
	if card.UserID != userID {
		return "", model.ErrCardBelongsToOtherUser
	}

	pan, err := uc.encryption.DecryptPAN(card.EncryptedPAN)
	if err != nil {
		return "", model.ErrDecryptionFailed
	}
	card.RecordPANDecryption("charge_no_channel_token")
	if err := uc.repo.Save(ctx, card); err != nil {
		log.Printf("[CardUseCase] PrepareOneTimeToken: audit event save failed (card=%s): %v", cardID, err)
	}
	uc.publishEvents(card)

	cardToken, err := uc.vault.CacheTokenizedCard(ctx, port.CachedCardData{
		RawPAN:  pan,
		PANHash: card.PANHash,
		Mask:    card.Mask,
		Holder:  card.Holder,
		UserID:  card.UserID,
	})
	if err != nil {
		return "", err
	}
	return cardToken, nil
}

// ── 原有用例 ────────────────────────────────────────────────────

func (uc *CardUseCase) ListCards(ctx context.Context, userID string) ([]*model.SavedCard, error) {
	return uc.repo.FindAllByUserID(ctx, userID)
}

func (uc *CardUseCase) SuspendCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}
	if err := card.Suspend(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}
	uc.publishEvents(card)
	return card, nil
}

func (uc *CardUseCase) ActivateCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}
	if err := card.Activate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}
	uc.publishEvents(card)
	return card, nil
}

func (uc *CardUseCase) DeleteCard(ctx context.Context, userID string, cardID model.SavedCardID) error {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return err
	}
	if err := card.Delete(); err != nil {
		return err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return err
	}
	uc.publishEvents(card)
	return nil
}

func (uc *CardUseCase) SetDefaultCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.findOwnedCard(ctx, userID, cardID)
	if err != nil {
		return nil, err
	}
	oldDefault, err := uc.repo.FindDefaultByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if oldDefault != nil && oldDefault.ID != cardID {
		oldDefault.UnsetDefault()
	}
	if err := card.SetDefault(); err != nil {
		return nil, err
	}
	if err := uc.repo.Save(ctx, card); err != nil {
		return nil, err
	}
	if oldDefault != nil && oldDefault.ID != cardID {
		if err := uc.repo.Save(ctx, oldDefault); err != nil {
			log.Printf("[CardUseCase] SetDefaultCard: new card saved but old card unset failed: %v", err)
		}
	}
	uc.publishEvents(card)
	return card, nil
}

func (uc *CardUseCase) GetCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	return uc.findOwnedCard(ctx, userID, cardID)
}

func (uc *CardUseCase) findOwnedCard(ctx context.Context, userID string, cardID model.SavedCardID) (*model.SavedCard, error) {
	card, err := uc.repo.FindByID(ctx, cardID)
	if err != nil {
		return nil, err
	}
	if card.UserID != userID {
		return nil, model.ErrCardBelongsToOtherUser
	}
	return card, nil
}

func (uc *CardUseCase) publishEvents(card *model.SavedCard) {
	for _, evt := range card.ClearEvents() {
		log.Printf("[DomainEvent] %s: %s", evt.EventName(), evt)
	}
}
