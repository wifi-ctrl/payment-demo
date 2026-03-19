# Card Vault 加密存储 & 复购实现方案

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Demo 从 Stripe 托管令牌化（SAQ A-EP）升级为自建 Card Vault 加密存储（SAQ D 架构模式），并实现基于 ChannelToken 的复购流程。

**Architecture:** 遵循 `doc/pci-ddd-analysis.md` 第 3.3 节聚合设计和第 6.2 节 Card Vault 存储方案。Card 聚合新增 `EncryptedPAN`、`PANHash` 值对象和 `ChannelToken` 子实体。引入 `EncryptionService` 领域服务处理 AES-256-GCM 加密/HMAC 查重。复购路径通过 ChannelToken 优先匹配实现"有 token 不解密 PAN"的核心原则。

**Tech Stack:** Go 1.21+, `crypto/aes` + `crypto/cipher` (GCM), `crypto/hmac` + `crypto/sha256`, `crypto/rand`

**参考文档：** `doc/pci-ddd-analysis.md` 第 2.1（术语）、3.3（聚合）、3.4（事件）、3.5（领域服务）、6.2（存储拆分）、7（时序图）、8.1.2（API）节

---

## 总体架构

### 当前 Demo vs 目标对比

| 维度 | 当前 Demo (SAQ A-EP) | 目标 (SAQ D 架构模式) |
|------|---------------------|---------------------|
| PAN 存储 | 不存储，Stripe 托管 (`tok_xxx`) | AES-256-GCM 加密后存储在 Card Vault |
| 查重 | 无 | HMAC-SHA-256(hmac_key, PAN) |
| 密钥管理 | 无 | DEK + HMAC Key（Demo 用内存 KMS） |
| 复购 | VaultToken 重复使用（碰巧可行） | ChannelToken 优先 → 无 token 时解密 PAN |
| 令牌化 | 前端 → Stripe API → tok_xxx | 前端 → pci-gateway → Card Vault 加密+Redis 缓存 |
| 卡数据生命周期 | Stripe 管理 | 令牌化=Redis 15min → 支付成功=PCI DB 持久化 |

### 改动范围全景

```
internal/
├── card/
│   ├── domain/
│   │   ├── model/
│   │   │   ├── saved_card.go          # ✏️ 重构聚合根：+EncryptedPAN, PANHash, KeyVersion, ChannelTokens
│   │   │   ├── crypto.go              # 🆕 加密相关值对象 (EncryptedPAN, PANHash, DEK, HMACKey)
│   │   │   ├── channel_token.go       # 🆕 ChannelToken 子实体
│   │   │   └── errors.go              # ✏️ +新错误类型
│   │   ├── port/
│   │   │   ├── vault.go               # ✏️ 重构：CardVault → 自建加密/解密语义
│   │   │   ├── key_manager.go         # 🆕 密钥管理端口（含 RotateDEK/RetireDEK/ListVersions）
│   │   │   ├── encrypter.go           # 🆕 加密能力端口（Encrypt/Decrypt/HMAC）[R-1]
│   │   │   └── repository.go          # ✏️ +FindByPANHash, +FindByKeyVersion, +channel token 查询
│   │   ├── event/
│   │   │   └── event.go               # ✏️ +ChannelTokenStored, ChannelTokenRevoked, PANDecrypted
│   │   └── service/
│   │       ├── encryption_service.go  # 🆕 加密领域服务（依赖 port.Encrypter）
│   │       └── card_validation.go     # 🆕 Luhn 校验 + BIN 品牌识别 [S-3]
│   ├── application/
│   │   ├── card_usecase.go            # ✏️ 重构绑卡+新增令牌化+查重逻辑
│   │   └── key_rotation_usecase.go    # 🆕 密钥轮换用例（编排：查数据→解密→重加密→存储）[R-2]
│   ├── adapter/
│   │   ├── vault/
│   │   │   └── local_vault.go         # 🆕 自建 CardVault 实现（替换 stripe_vault.go）
│   │   ├── crypto/
│   │   │   └── aes_encryption.go      # 🆕 AES-256-GCM 加密适配器
│   │   └── keymanager/
│   │       └── inmem_key_manager.go   # 🆕 内存 KMS 适配器（Demo 用）
│   └── handler/http/
│       └── card_handler.go            # ✏️ +令牌化 API, 响应不暴露敏感字段
├── payment/
│   ├── domain/
│   │   ├── port/
│   │   │   ├── card.go                # ✏️ SavedCardView +ChannelToken 字段
│   │   │   └── gateway.go             # ✏️ GatewayAuthResult +RecurringToken
│   │   └── model/
│   │       └── transaction.go         # ✏️ +Channel 字段（记录实际走的渠道）
│   ├── application/
│   │   └── charge_usecase.go          # ✏️ 区分首购/复购路径
│   └── adapter/
│       └── card/
│           ├── card_adapter.go        # ✏️ ACL 翻译 ChannelTokens map [W-3]
│           └── card_command_adapter.go # 🆕 CardCommand ACL 适配器 [W-4]
└── infra/stripe/
    ├── client.go                      # ✏️ PaymentIntentResult +RecurringToken
    └── mock_server.go                 # ✏️ mock 返回 recurring_token
```

---

## Task 1: 加密值对象 & 密钥管理端口

### 1.1 加密相关值对象

**File:** `internal/card/domain/model/crypto.go` (新建)

```go
package model

// EncryptedPAN AES-256-GCM 加密后的 PAN 密文（含 nonce + ciphertext + authTag）
// 不可变值对象，创建后禁止修改
type EncryptedPAN struct {
    Ciphertext []byte // nonce(12) + ciphertext + authTag(16)
    KeyVersion int    // 加密时使用的 DEK 版本号，密钥轮换时用于选择正确的 DEK 解密
}

// PANHash HMAC-SHA-256(hmac_key, PAN) 的不可逆哈希
// 用于查重：防止同一用户重复绑同一张卡
// 使用 HMAC 而非裸 SHA-256，防止 PAN 空间小被暴力遍历
type PANHash string

// MaskedPAN 脱敏卡号，仅后四位（如 "**** 1234"）
// 单独存储，避免查卡列表时触发解密和审计日志
type MaskedPAN string

// CardBrand 卡品牌
type CardBrand string

const (
    CardBrandVisa       CardBrand = "visa"
    CardBrandMastercard CardBrand = "mastercard"
    CardBrandUnionPay   CardBrand = "unionpay"
    CardBrandUnknown    CardBrand = "unknown"
)

// Expiry 有效期值对象（month + year 组合）
type Expiry struct {
    Month int // 1-12
    Year  int // 如 2028
}

// RawCardData 令牌化阶段的原始卡数据（仅在内存中短暂存在）
// 禁止持久化、禁止日志输出
type RawCardData struct {
    PAN            string
    ExpiryMonth    int
    ExpiryYear     int
    CVV            string
    CardholderName string
}

// String 防止意外 %v 打印泄露 PAN/CVV [S-2]
func (r RawCardData) String() string  { return "[REDACTED]" }
func (r RawCardData) GoString() string { return "[REDACTED]" }
```

**设计要点（对应 pci-ddd-analysis.md §2.1 + §3.3）：**
- `EncryptedPAN.Ciphertext` 包含 GCM 标准的 nonce+密文+tag 整包，解密时前 12 字节为 nonce
- `PANHash` 用 HMAC-SHA-256 而非 SHA-256，防止 PAN 空间小（10^16）被暴力遍历
- `RawCardData` 仅存在于令牌化请求处理期间的内存中，处理完毕即丢弃

### 1.2 密钥管理端口

**File:** `internal/card/domain/port/key_manager.go` (新建)

```go
package port

// KeyManager 密钥管理端口（被驱动端口）
// 领域层定义接口，适配器层提供 KMS/内存 实现
// 对应 pci-ddd-analysis.md §3.1 密钥管理域
type KeyManager interface {
    // CurrentDEK 获取当前活跃的 DEK 及其版本号
    // DEK 用于 AES-256-GCM 加密/解密 PAN
    CurrentDEK() (dek []byte, version int, err error)

    // DEKByVersion 按版本号获取 DEK（密钥轮换后解密旧数据用）
    DEKByVersion(version int) ([]byte, error)

    // HMACKey 获取 HMAC 密钥
    // 用于计算 pan_hash = HMAC-SHA-256(hmac_key, PAN)
    HMACKey() ([]byte, error)
}
```

### 1.3 内存 KMS 适配器（Demo 用）

**File:** `internal/card/adapter/keymanager/inmem_key_manager.go` (新建)

```go
package keymanager

import (
    "crypto/rand"
    "fmt"

    "payment-demo/internal/card/domain/port"
)

// InMemKeyManager Demo 用内存密钥管理器
// 生产环境替换为 AWS KMS / GCP KMS / HashiCorp Vault 适配器
type InMemKeyManager struct {
    dek        []byte // 32 bytes for AES-256
    dekVersion int
    hmacKey    []byte // 32 bytes for HMAC-SHA-256
}

var _ port.KeyManager = (*InMemKeyManager)(nil)

func NewInMemKeyManager() *InMemKeyManager {
    dek := make([]byte, 32)
    if _, err := rand.Read(dek); err != nil {
        panic(fmt.Sprintf("failed to generate DEK: %v", err))
    }
    hmacKey := make([]byte, 32)
    if _, err := rand.Read(hmacKey); err != nil {
        panic(fmt.Sprintf("failed to generate HMAC key: %v", err))
    }
    return &InMemKeyManager{
        dek:        dek,
        dekVersion: 1,
        hmacKey:    hmacKey,
    }
}

func (m *InMemKeyManager) CurrentDEK() ([]byte, int, error) {
    return m.dek, m.dekVersion, nil
}

func (m *InMemKeyManager) DEKByVersion(version int) ([]byte, error) {
    if version != m.dekVersion {
        return nil, fmt.Errorf("DEK version %d not found", version)
    }
    return m.dek, nil
}

func (m *InMemKeyManager) HMACKey() ([]byte, error) {
    return m.hmacKey, nil
}
```

---

## Task 2: 加密端口 & 领域服务

### 2.1 Encrypter 端口 [R-1 修复]

**File:** `internal/card/domain/port/encrypter.go` (新建)

`Encrypter` 是被驱动端口，由消费方（domain/service）定义，adapter 层实现。
放在 `domain/port/` 与 `KeyManager` 平级，符合 CLAUDE.md "接口定义在 domain/port/" 约束。

```go
package port

// Encrypter PAN 加密/解密/HMAC 能力端口（被驱动端口）
// 领域服务依赖此接口，adapter 层提供 AES-256-GCM 实现
type Encrypter interface {
    Encrypt(plaintext []byte, dek []byte) ([]byte, error)
    Decrypt(ciphertext []byte, dek []byte) ([]byte, error)
    HMAC(data []byte, key []byte) (string, error)
}
```

### 2.2 加密领域服务

**File:** `internal/card/domain/service/encryption_service.go` (新建)

对应 `pci-ddd-analysis.md` §3.5 `CardTokenizeService` 的加密编排职责。

```go
package service

import (
    "payment-demo/internal/card/domain/model"
    "payment-demo/internal/card/domain/port"
)

// EncryptionService 卡数据加密领域服务
// 职责：PAN 加密、HMAC 查重哈希计算、PAN 解密
// 为什么不放在聚合里：涉及 KeyManager 外部依赖 + 跨记录查重校验
type EncryptionService struct {
    keyMgr    port.KeyManager
    encrypter port.Encrypter // [R-1] 依赖 port.Encrypter 而非本包接口
}

func NewEncryptionService(keyMgr port.KeyManager, enc port.Encrypter) *EncryptionService {
    return &EncryptionService{keyMgr: keyMgr, encrypter: enc}
}

// EncryptPANOnly 仅加密 PAN（不计算 HMAC）[W-1 修复]
// 供 Tokenize 场景使用：先 ComputePANHash 查重，命中则跳过加密，未命中再调此方法
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
// 调用方（UseCase）负责在调用后通过聚合根 RecordPANDecryption 记录审计事件
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

// ComputePANHash 仅计算 HMAC 哈希（令牌化时查重用）
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
```

### 2.3 AES-256-GCM 加密适配器

**File:** `internal/card/adapter/crypto/aes_encryption.go` (新建)

```go
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

var _ port.Encrypter = (*AESEncrypter)(nil) // [R-1] 实现 port.Encrypter 而非 service.Encrypter

func NewAESEncrypter() *AESEncrypter { return &AESEncrypter{} }

// Encrypt AES-256-GCM 加密
// 返回 nonce(12) + ciphertext + authTag(16) 的整包
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

    // Seal: nonce + ciphertext + authTag
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt AES-256-GCM 解密
// 输入 nonce(12) + ciphertext + authTag(16) 的整包
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

// HMAC 计算 HMAC-SHA-256
func (e *AESEncrypter) HMAC(data, key []byte) (string, error) {
    mac := hmac.New(sha256.New, key)
    mac.Write(data)
    return hex.EncodeToString(mac.Sum(nil)), nil
}
```

---

## Task 3: 重构 Card 聚合根

### 3.1 ChannelToken 子实体

**File:** `internal/card/domain/model/channel_token.go` (新建)

对应 `pci-ddd-analysis.md` §3.3 Card 聚合的 ChannelToken 子实体。

```go
package model

import "time"

// TokenStatus 渠道 Token 状态
type TokenStatus string

const (
    TokenStatusActive  TokenStatus = "active"
    TokenStatusRevoked TokenStatus = "revoked"
)

// ChannelToken 渠道复购令牌（Card 聚合的子实体）
// 一张卡可以有多个 ChannelToken（一卡一渠道一令牌）
// 首次卡支付成功后由渠道返回，复购时直接用 token 扣款无需解密 PAN
type ChannelToken struct {
    Channel     string      // 渠道标识: "stripe", "adyen", "paypal" ...
    Token       string      // 渠道侧 recurring token (pm_xxx, storedPaymentMethodId, ...)
    ShopperRef  string      // 渠道侧用户标识 (Adyen shopperReference 等)
    Status      TokenStatus
    CreatedAt   time.Time
}
```

### 3.2 重构 SavedCard 聚合根

**File:** `internal/card/domain/model/saved_card.go` (修改)

**核心变更：**
- 移除 `VaultToken`（Stripe 托管概念），替换为 `EncryptedPAN` + `PANHash` + `KeyVersion`（自建 Vault）
- 保留 `CardMask` 用于脱敏展示
- 新增 `ChannelTokens` 子实体集合及行为方法
- 保留 `CardHolder`
- [R-3] 新增 `ReEncrypt` + `RecordPANDecryption` 公开行为方法

```go
// SavedCard 已保存卡聚合根（Card Vault 模式）
// 对应 pci-ddd-analysis.md §3.3 Card 聚合设计
//
// 不变量 (Invariants):
//   - 同一 user_id + pan_hash 不能有两张 active 卡
//   - card_id 全局唯一
//   - 解密 PAN 必须记录审计日志（由 UseCase 层保证）
//   - 删卡是软删除 (status→deleted)，不物理删除
//   - Card 仅在首次支付成功后创建（save_card=true）
//
// 状态机: Active ⇄ Suspended，Active/Suspended → Deleted（终态不可逆）
type SavedCard struct {
    ID            SavedCardID
    UserID        string
    EncryptedPAN  EncryptedPAN  // AES-256-GCM 加密的完整卡号
    PANHash       PANHash       // HMAC-SHA-256 哈希，查重用
    Mask          CardMask      // 脱敏展示信息（last4, brand, expiry）
    Holder        CardHolder
    ChannelTokens []ChannelToken // 一卡多渠道的 recurring token
    IsDefault     bool
    Status        CardStatus
    CreatedAt     time.Time
    UpdatedAt     time.Time
    Events        []event.DomainEvent
}

// NewSavedCard 工厂方法
// 由 EncryptionService 处理加密后，UseCase 传入加密结果创建聚合
func NewSavedCard(
    userID string,
    encrypted EncryptedPAN,
    panHash PANHash,
    mask CardMask,
    holder CardHolder,
) *SavedCard {
    return &SavedCard{
        ID:           NewSavedCardID(),
        UserID:       userID,
        EncryptedPAN: encrypted,
        PANHash:      panHash,
        Mask:         mask,
        Holder:       holder,
        IsDefault:    false,
        Status:       CardStatusActive,
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }
}

// ── 加密相关行为 [R-3 修复] ─────────────────────────────────────

// ReEncrypt 密钥轮换时用新密文替换旧密文（状态变更内聚在聚合根）
func (c *SavedCard) ReEncrypt(newEncrypted EncryptedPAN) {
    c.EncryptedPAN = newEncrypted
    c.UpdatedAt = time.Now()
}

// RecordPANDecryption 记录 PAN 解密审计事件（PCI Req 10）
// 由 UseCase 在调用 EncryptionService.DecryptPAN 后立即调用
func (c *SavedCard) RecordPANDecryption(reason string) {
    c.addEvent(event.PANDecrypted{
        CardID:     string(c.ID),
        UserID:     c.UserID,
        Reason:     reason,
        OccurredAt: time.Now(),
    })
}

// ── ChannelToken 行为 ──────────────────────────────────────────

// StoreChannelToken 存储渠道复购令牌（首次支付成功后调用）
// 不变量：一卡一渠道一令牌，相同渠道覆盖旧 token
func (c *SavedCard) StoreChannelToken(channel, token, shopperRef string) {
    for i, ct := range c.ChannelTokens {
        if ct.Channel == channel {
            c.ChannelTokens[i].Token = token
            c.ChannelTokens[i].ShopperRef = shopperRef
            c.ChannelTokens[i].Status = TokenStatusActive
            c.UpdatedAt = time.Now()
            c.addEvent(event.ChannelTokenStored{
                CardID:  string(c.ID),
                Channel: channel,
                OccurredAt: time.Now(),
            })
            return
        }
    }
    c.ChannelTokens = append(c.ChannelTokens, ChannelToken{
        Channel:    channel,
        Token:      token,
        ShopperRef: shopperRef,
        Status:     TokenStatusActive,
        CreatedAt:  time.Now(),
    })
    c.UpdatedAt = time.Now()
    c.addEvent(event.ChannelTokenStored{
        CardID:     string(c.ID),
        Channel:    channel,
        OccurredAt: time.Now(),
    })
}

// GetActiveChannelToken 获取指定渠道的 active token，无则返回 nil
func (c *SavedCard) GetActiveChannelToken(channel string) *ChannelToken {
    for i, ct := range c.ChannelTokens {
        if ct.Channel == channel && ct.Status == TokenStatusActive {
            return &c.ChannelTokens[i]
        }
    }
    return nil
}

// RevokeChannelToken 吊销指定渠道的 token
func (c *SavedCard) RevokeChannelToken(channel string) {
    for i, ct := range c.ChannelTokens {
        if ct.Channel == channel && ct.Status == TokenStatusActive {
            c.ChannelTokens[i].Status = TokenStatusRevoked
            c.UpdatedAt = time.Now()
            c.addEvent(event.ChannelTokenRevoked{
                CardID:     string(c.ID),
                Channel:    channel,
                OccurredAt: time.Now(),
            })
            return
        }
    }
}

// RevokeAllChannelTokens 删卡时批量吊销所有 active token
func (c *SavedCard) RevokeAllChannelTokens() {
    for i, ct := range c.ChannelTokens {
        if ct.Status == TokenStatusActive {
            c.ChannelTokens[i].Status = TokenStatusRevoked
            c.addEvent(event.ChannelTokenRevoked{
                CardID:     string(c.ID),
                Channel:    ct.Channel,
                OccurredAt: time.Now(),
            })
        }
    }
}
```

### 3.3 新增领域事件

**File:** `internal/card/domain/event/event.go` (修改，追加)

```go
// ChannelTokenStored 渠道复购令牌已存储
type ChannelTokenStored struct {
    CardID     string
    Channel    string
    OccurredAt time.Time
}

func (e ChannelTokenStored) EventName() string { return "card.channel_token_stored" }

// ChannelTokenRevoked 渠道复购令牌已吊销
type ChannelTokenRevoked struct {
    CardID     string
    Channel    string
    OccurredAt time.Time
}

func (e ChannelTokenRevoked) EventName() string { return "card.channel_token_revoked" }

// PANDecrypted PAN 已解密（PCI Req 10 审计事件）
type PANDecrypted struct {
    CardID     string
    UserID     string
    Reason     string // "charge", "export" 等
    OccurredAt time.Time
}

func (e PANDecrypted) EventName() string { return "card.pan_decrypted" }
```

### 3.4 新增错误类型

**File:** `internal/card/domain/model/errors.go` (修改，追加)

```go
var (
    ErrDuplicateCard    = errors.New("card already exists for this user")
    ErrEncryptionFailed = errors.New("PAN encryption failed")
    ErrDecryptionFailed = errors.New("PAN decryption failed")
    ErrCardTokenExpired = errors.New("card token expired or already consumed")
    ErrCardTokenInvalid = errors.New("invalid card token")
)
```

---

## Task 4: 重构 CardVault 端口 & Repository

### 4.1 CardVault 端口重构

**File:** `internal/card/domain/port/vault.go` (重构)

当前的 `CardVault` 是 Stripe 托管语义（Tokenize = 调 Stripe API 换 token）。
重构为自建 Vault 语义：临时缓存（Redis 模式）+ 持久存储。

```go
package port

import (
    "context"

    "payment-demo/internal/card/domain/model"
)

// TokenizeResult 令牌化结果
// [S-1] CardToken 为 *string：查重命中时为 nil，前端应直接用 ExistingCardID 复购
type TokenizeResult struct {
    CardToken      *string          // 临时令牌 "ct_" + UUID（查重命中时为 nil）
    Mask           model.CardMask   // 脱敏信息（即时返回给前端）
    Brand          model.CardBrand
    ExistingCardID *model.SavedCardID // 查重命中时返回已有 card_id
}

// CachedCardData 从临时缓存中取出的卡数据（一次性消费）
// [W-NEW-3] 两种场景使用不同字段：
//   - 首次令牌化：EncryptedPAN 非空（密文），Gateway 需解密后使用
//   - PrepareOneTimeToken：RawPAN 非空（明文，TTL 极短），Gateway 直接使用
type CachedCardData struct {
    EncryptedPAN model.EncryptedPAN // 首次令牌化时存密文（绑卡持久化用）
    RawPAN       string             // PrepareOneTimeToken 时存明文（Gateway 直接用，TTL=5min）
    PANHash      model.PANHash
    Mask         model.CardMask
    Holder       model.CardHolder
    UserID       string
}

// CardVault 卡数据保险库端口（被驱动端口）
// 职责：令牌化临时缓存 + 一次性消费
// 对应 pci-ddd-analysis.md §6.2 PCI Redis 存储
type CardVault interface {
    // CacheTokenizedCard 将加密后的卡数据临时缓存，返回 card_token
    // TTL = 15min，一次性使用（取出即删除）
    CacheTokenizedCard(ctx context.Context, data CachedCardData) (cardToken string, err error)

    // ConsumeCardToken 原子取出并删除临时卡数据（GETDEL 语义）
    // 防止同一 card_token 被并发请求重复消费
    ConsumeCardToken(ctx context.Context, cardToken string) (*CachedCardData, error)
}
```

### 4.2 CardVault 内存适配器（替换 stripe_vault.go）

**File:** `internal/card/adapter/vault/local_vault.go` (新建)

```go
package vault

import (
    "context"
    "sync"
    "time"

    "github.com/google/uuid"

    "payment-demo/internal/card/domain/model"
    "payment-demo/internal/card/domain/port"
)

type cachedEntry struct {
    data      port.CachedCardData
    expiresAt time.Time
}

// LocalVault 本地内存 CardVault 实现（Demo 用）
// 生产环境替换为 PCI Redis 适配器
type LocalVault struct {
    mu    sync.Mutex
    cache map[string]*cachedEntry // card_token → cached data
    ttl   time.Duration
}

var _ port.CardVault = (*LocalVault)(nil)

func NewLocalVault() *LocalVault {
    return &LocalVault{
        cache: make(map[string]*cachedEntry),
        ttl:   15 * time.Minute,
    }
}

func (v *LocalVault) CacheTokenizedCard(_ context.Context, data port.CachedCardData) (string, error) {
    v.mu.Lock()
    defer v.mu.Unlock()

    token := "ct_" + uuid.New().String()
    v.cache[token] = &cachedEntry{
        data:      data,
        expiresAt: time.Now().Add(v.ttl),
    }
    return token, nil
}

// ConsumeCardToken 原子取出+删除（GETDEL 语义）
func (v *LocalVault) ConsumeCardToken(_ context.Context, cardToken string) (*port.CachedCardData, error) {
    v.mu.Lock()
    defer v.mu.Unlock()

    entry, ok := v.cache[cardToken]
    if !ok {
        return nil, model.ErrCardTokenInvalid
    }
    delete(v.cache, cardToken)

    if time.Now().After(entry.expiresAt) {
        return nil, model.ErrCardTokenExpired
    }
    return &entry.data, nil
}
```

### 4.3 Repository 增强

**File:** `internal/card/domain/port/repository.go` (修改)

```go
type CardRepository interface {
    Save(ctx context.Context, card *model.SavedCard) error
    FindByID(ctx context.Context, id model.SavedCardID) (*model.SavedCard, error)
    FindAllByUserID(ctx context.Context, userID string) ([]*model.SavedCard, error)
    FindDefaultByUserID(ctx context.Context, userID string) (*model.SavedCard, error)

    // 新增：按 PANHash 查重
    FindActiveByUserAndPANHash(ctx context.Context, userID string, panHash model.PANHash) (*model.SavedCard, error)
}
```

### 4.4 InMemoryCardRepository 新方法实现 [W-5]

**File:** `internal/card/adapter/persistence/inmem_card_repository.go` (修改)

```go
func (r *InMemoryCardRepository) FindActiveByUserAndPANHash(
    ctx context.Context, userID string, panHash model.PANHash,
) (*model.SavedCard, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    for _, card := range r.cards {
        if card.UserID == userID && card.PANHash == panHash && card.Status == model.CardStatusActive {
            return card, nil
        }
    }
    return nil, model.ErrCardNotFound
}

func (r *InMemoryCardRepository) FindByKeyVersion(
    ctx context.Context, keyVersion int,
) ([]*model.SavedCard, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    var result []*model.SavedCard
    for _, card := range r.cards {
        if card.EncryptedPAN.KeyVersion == keyVersion && card.Status != model.CardStatusDeleted {
            result = append(result, card)
        }
    }
    return result, nil
}
```

---

## Task 5: 重构 Card UseCase（令牌化 + 绑卡）

**File:** `internal/card/application/card_usecase.go` (重构)

对应 `pci-ddd-analysis.md` §3.5 `CardTokenizeService` 编排逻辑。

```go
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
// 对应 pci-ddd-analysis.md §8.1.2 /api/v1/vault/tokenize
func (uc *CardUseCase) Tokenize(ctx context.Context, req TokenizeRequest) (*port.TokenizeResult, error) {
    // 1. 基础校验（Luhn + BIN 识别 brand）[W-NEW-1 修复：跨包调用须用导出名]
    brand := service.IdentifyBrand(req.PAN)
    if !service.LuhnCheck(req.PAN) {
        return nil, model.ErrCardTokenInvalid
    }
    last4 := req.PAN[len(req.PAN)-4:]

    // 2. 计算 HMAC 哈希（查重）
    panHash, err := uc.encryption.ComputePANHash(req.PAN)
    if err != nil {
        return nil, model.ErrEncryptionFailed
    }

    // 3. 查重：同一用户+同一卡号是否已有 active 卡
    existing, err := uc.repo.FindActiveByUserAndPANHash(ctx, req.UserID, panHash)
    if err == nil && existing != nil {
        existingID := existing.ID
        return &port.TokenizeResult{
            CardToken: nil, // [S-1] 查重命中，不生成临时 token
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

    // 4. AES-256-GCM 加密 PAN（仅加密，不重复计算 HMAC）[W-1]
    encrypted, err := uc.encryption.EncryptPANOnly(req.PAN)
    if err != nil {
        return nil, model.ErrEncryptionFailed
    }

    // 5. 缓存到临时存储（Redis/内存），返回 card_token
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

// ── 支付成功后绑卡 ──────────────────────────────────────────────

// BindFromTokenRequest 从临时 token 创建持久化卡（支付成功后调用）
type BindFromTokenRequest struct {
    CardToken      string // 令牌化返回的 ct_xxx
    ChannelToken   string // 渠道返回的 recurring token
    Channel        string // 渠道标识
    ShopperRef     string // 渠道侧用户标识
}

// BindCardFromToken 支付成功后持久化卡
// 对应 pci-ddd-analysis.md §8.1.2 /internal/v1/charge 步骤 7b
func (uc *CardUseCase) BindCardFromToken(ctx context.Context, req BindFromTokenRequest) (*model.SavedCard, error) {
    // 1. 原子取出临时数据
    cached, err := uc.vault.ConsumeCardToken(ctx, req.CardToken)
    if err != nil {
        return nil, err
    }

    // 2. 查重（并发竞态处理）
    existing, err := uc.repo.FindActiveByUserAndPANHash(ctx, cached.UserID, cached.PANHash)
    if err == nil && existing != nil {
        // 同一卡已存在，仅补存 channel_token
        if req.ChannelToken != "" {
            existing.StoreChannelToken(req.Channel, req.ChannelToken, req.ShopperRef)
            if saveErr := uc.repo.Save(ctx, existing); saveErr != nil { // [W-2] 不静默忽略
                log.Printf("[CardUseCase] BindCardFromToken: channel_token save failed (card=%s, channel=%s): %v",
                    existing.ID, req.Channel, saveErr)
            }
        }
        return existing, nil
    }

    // 3. 创建聚合根
    card := model.NewSavedCard(
        cached.UserID,
        cached.EncryptedPAN,
        cached.PANHash,
        cached.Mask,
        cached.Holder,
    )

    // 4. 存储 channel_token（如有）
    if req.ChannelToken != "" {
        card.StoreChannelToken(req.Channel, req.ChannelToken, req.ShopperRef)
    }

    // 5. 首张卡设为默认
    existingDefault, _ := uc.repo.FindDefaultByUserID(ctx, cached.UserID)
    if existingDefault == nil {
        card.BindAsDefault()
    } else {
        card.Bind()
    }

    // 6. 持久化
    if err := uc.repo.Save(ctx, card); err != nil {
        return nil, err
    }

    uc.publishEvents(card)
    return card, nil
}

// ── StoreChannelToken（已存卡的复购后回存 token）────────────────

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

// ── PrepareOneTimeToken [W-6 修复] ──────────────────────────────
//
// 已存卡无目标渠道 ChannelToken 时：解密 PAN → 重新缓存到 CardVault → 返回一次性 token
// PAN 解密+缓存全在 card 上下文内完成，payment 上下文不接触 PAN 明文
func (uc *CardUseCase) PrepareOneTimeToken(ctx context.Context, cardID model.SavedCardID, userID string) (string, error) {
    card, err := uc.repo.FindByID(ctx, cardID)
    if err != nil {
        return "", err
    }
    if card.UserID != userID {
        return "", model.ErrCardBelongsToOtherUser
    }

    // 解密 PAN（审计事件由聚合根记录）
    pan, err := uc.encryption.DecryptPAN(card.EncryptedPAN)
    if err != nil {
        return "", model.ErrDecryptionFailed
    }
    card.RecordPANDecryption("charge_no_channel_token")
    if err := uc.repo.Save(ctx, card); err != nil { // [W-NEW-2 修复] 不静默忽略，审计事件丢失需告警
        log.Printf("[CardUseCase] PrepareOneTimeToken: audit event save failed (card=%s): %v", cardID, err)
    }
    uc.publishEvents(card)

    // [W-NEW-3 修复] 缓存 PAN 明文（非密文），Gateway 取出后可直接使用
    // RawPAN 仅短暂存活于缓存（TTL=5min），ConsumeCardToken 后立即删除
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
```

> **W-6 设计说明：** 移除 `VaultToken` 后，"已存卡无 ChannelToken" 路径不能再用 `cardView.Token`。
> 选择 `PrepareOneTimeToken` 方案（card 上下文内部解密 PAN → 重新缓存到 CardVault → 返回一次性 token）的原因：
> - PAN 明文不跨上下文边界（payment 只拿到 `ct_xxx`）
> - 复用已有的 `CardVault.CacheTokenizedCard` 和 `Gateway.Authorize(token)` 路径
> - payment UseCase 无需感知 PAN 解密逻辑
>
> **[W-NEW-3] 补充：** `PrepareOneTimeToken` 缓存的是 **PAN 明文**（`RawPAN`），而非密文。
> Gateway adapter 从 `ConsumeCardToken` 取出后，通过 `RawPAN` 直接构造 Stripe API 请求。
> 首次令牌化场景仍缓存密文（`EncryptedPAN`），绑卡时直接持久化，无需再解密。
> Gateway adapter 通过判断 `RawPAN != ""` 区分两种路径。

辅助函数放 `domain/service/`（纯业务规则，Luhn+BIN 属于领域知识）[S-3]：

**File:** `internal/card/domain/service/card_validation.go` (新建)

```go
package service

import "payment-demo/internal/card/domain/model"

func LuhnCheck(pan string) bool {
    sum := 0
    alt := false
    for i := len(pan) - 1; i >= 0; i-- {
        n := int(pan[i] - '0')
        if n < 0 || n > 9 {
            return false
        }
        if alt {
            n *= 2
            if n > 9 {
                n -= 9
            }
        }
        sum += n
        alt = !alt
    }
    return sum%10 == 0
}

func IdentifyBrand(pan string) model.CardBrand {
    if len(pan) < 2 {
        return model.CardBrandUnknown
    }
    switch {
    case pan[0] == '4':
        return model.CardBrandVisa
    case pan[:2] >= "51" && pan[:2] <= "55":
        return model.CardBrandMastercard
    case pan[:2] == "62":
        return model.CardBrandUnionPay
    default:
        return model.CardBrandUnknown
    }
}
```

---

## Task 6: 复购 — Payment 侧改动

### 6.1 GatewayAuthResult 增加 RecurringToken

**File:** `internal/payment/domain/port/gateway.go` (修改)

```go
type GatewayAuthResult struct {
    ProviderRef    string
    AuthCode       string
    RecurringToken string // 渠道返回的复购 token（首次支付时非空）
    Channel        string // 实际使用的渠道标识 ("stripe", "paypal", ...)
}
```

### 6.2 SavedCardView 增加 ChannelTokens [W-3 修复]

**File:** `internal/payment/domain/port/card.go` (修改)

用 `map[string]string` 暴露全部渠道 token，payment UseCase 按目标渠道精确匹配，
不再盲取第一个 active token。

```go
type SavedCardView struct {
    CardID        string
    UserID        string
    ChannelTokens map[string]string // channel → recurring token [W-3]
    Last4         string
    Brand         string
    IsActive      bool
}
```

新增写入端口：

```go
// CardCommand payment 上下文对 card 上下文的写入端口
// 支付成功后回存 channel_token / 绑卡 / 解密PAN用
type CardCommand interface {
    StoreChannelToken(ctx context.Context, cardID, channel, token, shopperRef string) error
    BindCardFromToken(ctx context.Context, req BindFromTokenCommand) (cardID string, err error)
    // PrepareOneTimeToken 已存卡无 ChannelToken 时：解密 PAN → 生成一次性 token [W-6]
    // PAN 解密 + 重新缓存到 CardVault，返回 ct_xxx 临时 token，payment 按新卡路径使用
    PrepareOneTimeToken(ctx context.Context, cardID, userID string) (cardToken string, err error)
}

// BindFromTokenCommand 绑卡命令入参（与 CardUseCase.BindFromTokenRequest 对齐）[W-4]
type BindFromTokenCommand struct {
    CardToken    string
    Channel      string
    Token        string
    ShopperRef   string
}
```

### 6.3 PaymentTransaction 增加 Channel

**File:** `internal/payment/domain/model/transaction.go` (修改)

```go
type PaymentTransaction struct {
    // ...existing fields...
    Channel string // 实际扣款渠道 ("stripe", "adyen", ...)
}
```

### 6.4 ChargeUseCase 区分首购/复购 [W-6 修复]

**File:** `internal/payment/application/charge_usecase.go` (修改 Purchase 方法)

```go
type ChargeUseCase struct {
    // ...existing fields...
    cardCommand port.CardCommand // 新增：支付成功后回存 token / 绑卡 / 解密PAN
}

func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
    // ...省略 1-4（查商品、优惠券、税率、计算金额）...

    // 5. 获取商户 CARD 渠道凭据 → 确定目标渠道
    cred, err := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodCard)
    if err != nil {
        return nil, err
    }
    gateway, err := uc.gatewayFactory.BuildCardGateway(*cred)
    if err != nil {
        return nil, model.ErrMerchantGatewayBuildFailed
    }
    targetChannel := cred.Channel // "stripe", "adyen", etc.

    // ── 解析支付卡（区分首购 vs 复购）────────────
    var cardToken model.CardToken
    var isRecurring bool

    if req.SavedCardID != "" {
        // 已存卡路径
        cardView, err := uc.cardQuery.FindActiveCard(ctx, req.SavedCardID)
        if err != nil {
            return nil, err
        }
        if cardView.UserID != req.UserID {
            return nil, model.ErrCardNotFound
        }
        if !cardView.IsActive {
            return nil, model.ErrCardNotUsable
        }

        // [W-3] 按目标渠道精确匹配 ChannelToken
        if ct, ok := cardView.ChannelTokens[targetChannel]; ok {
            // 复购路径：用 channel_token 扣款，不解密 PAN
            cardToken = model.CardToken{
                TokenID: ct,
                Last4:   cardView.Last4,
                Brand:   cardView.Brand,
            }
            isRecurring = true
        } else {
            // [W-6] 已存卡但目标渠道无 channel_token：解密 PAN → 生成一次性 token
            // PAN 解密在 card 上下文内完成，payment 上下文不接触 PAN 明文
            oneTimeToken, err := uc.cardCommand.PrepareOneTimeToken(ctx, req.SavedCardID, req.UserID)
            if err != nil {
                return nil, err
            }
            cardToken = model.CardToken{
                TokenID: oneTimeToken,
                Last4:   cardView.Last4,
                Brand:   cardView.Brand,
            }
        }
    } else {
        // 新卡一次性 token 路径
        cardToken = req.Token
    }

    // ...创建交易、授权...
    result, err := gateway.Authorize(ctx, cardToken, amount)
    if err != nil {
        // ...error handling...
    }

    txn.Channel = result.Channel

    // ── 授权成功后回存 channel_token ────────────
    if result.RecurringToken != "" {
        if req.SavedCardID != "" && !isRecurring {
            // 已存卡 + 首次走此渠道 → 补存 channel_token
            if err := uc.cardCommand.StoreChannelToken(
                ctx, req.SavedCardID,
                result.Channel, result.RecurringToken, "",
            ); err != nil {
                log.Printf("[UseCase] StoreChannelToken failed (card=%s): %v", req.SavedCardID, err)
            }
        }
        if req.SavedCardID == "" && req.SaveCard {
            // 新卡 + save_card=true → 持久化卡 + 存 channel_token
            cardID, err := uc.cardCommand.BindCardFromToken(ctx, port.BindFromTokenCommand{
                CardToken:  cardToken.TokenID,
                Channel:    result.Channel,
                Token:      result.RecurringToken,
            })
            if err != nil {
                log.Printf("[UseCase] BindCardFromToken failed: %v", err)
            } else if cardID != "" {
                log.Printf("[UseCase] Card saved: %s", cardID)
            }
        }
    }

    // ...省略后续（MarkAuthorized, Save, publishEvents）...
}
```

### 6.5 PurchaseRequest 增加 SaveCard

```go
type PurchaseRequest struct {
    MerchantID  string
    UserID      string
    ProductID   string
    Token       model.CardToken
    SavedCardID string
    CouponCode  string
    SaveCard    bool   // 新增：新卡支付成功后是否保存（仅 Token 路径有效）
}
```

### 6.6 ChannelCredentialView 增加 Channel

`ChannelCredentialView` 需暴露渠道标识，供 ChargeUseCase 按渠道查 ChannelToken。

```go
// payment/domain/port/merchant.go (修改)
type ChannelCredentialView struct {
    // ...existing fields...
    Channel string // "stripe", "adyen", etc.（由 MerchantAdapter 从凭据元数据翻译）
}
```

---

## Task 7: Stripe Adapter 返回 RecurringToken

### 7.1 stripe.Client 增强

**File:** `internal/infra/stripe/client.go` (修改)

```go
type PaymentIntentResult struct {
    ID             string
    AuthCode       string
    RecurringToken string // Stripe 的 PaymentMethod ID (pm_xxx)
}
```

`CreatePaymentIntent` 方法解析响应时提取 `recurring_token` 字段。

### 7.2 StripeGatewayAdapter 传递 RecurringToken

**File:** `internal/payment/adapter/gateway/stripe_gateway.go` (修改)

```go
func (g *StripeGatewayAdapter) Authorize(...) (*port.GatewayAuthResult, error) {
    result, err := g.client.CreatePaymentIntent(...)
    // ...
    return &port.GatewayAuthResult{
        ProviderRef:    result.ID,
        AuthCode:       result.AuthCode,
        RecurringToken: result.RecurringToken,
        Channel:        "stripe",
    }, nil
}
```

### 7.3 Mock Server 返回 recurring_token

**File:** `internal/infra/stripe/mock_server.go` (修改)

在 PaymentIntent 成功响应中增加 `recurring_token: "pm_mock_" + UUID`。

---

## Task 8: CardAdapter & CardCommandAdapter [W-3, W-4 修复]

### 8.1 CardAdapter（查询 ACL）

**File:** `internal/payment/adapter/card/card_adapter.go` (修改)

```go
func (a *CardAdapter) FindActiveCard(ctx context.Context, cardID string) (*port.SavedCardView, error) {
    card, err := a.repo.FindByID(ctx, cardModel.SavedCardID(cardID))
    if err != nil {
        return nil, model.ErrCardNotFound
    }

    // [W-3] 暴露全部 active ChannelTokens，由 UseCase 按目标渠道匹配
    tokens := make(map[string]string)
    for _, ct := range card.ChannelTokens {
        if ct.Status == cardModel.TokenStatusActive {
            tokens[ct.Channel] = ct.Token
        }
    }

    return &port.SavedCardView{
        CardID:        string(card.ID),
        UserID:        card.UserID,
        ChannelTokens: tokens,
        Last4:         card.Mask.Last4,
        Brand:         card.Mask.Brand,
        IsActive:      card.Status == cardModel.CardStatusActive,
    }, nil
}
```

### 8.2 CardCommandAdapter（写入 ACL）[W-4 修复]

**File:** `internal/payment/adapter/card/card_command_adapter.go` (新建)

```go
package card

import (
    "context"

    cardApp "payment-demo/internal/card/application"
    cardModel "payment-demo/internal/card/domain/model"
    "payment-demo/internal/payment/domain/port"
)

// CardCommandAdapter 实现 payment.port.CardCommand 接口
// 桥接 payment → card UseCase，翻译入参/返回值
type CardCommandAdapter struct {
    cardUC *cardApp.CardUseCase
}

var _ port.CardCommand = (*CardCommandAdapter)(nil)

func NewCardCommandAdapter(cardUC *cardApp.CardUseCase) *CardCommandAdapter {
    return &CardCommandAdapter{cardUC: cardUC}
}

func (a *CardCommandAdapter) StoreChannelToken(ctx context.Context, cardID, channel, token, shopperRef string) error {
    return a.cardUC.StoreChannelToken(ctx, cardModel.SavedCardID(cardID), channel, token, shopperRef)
}

func (a *CardCommandAdapter) BindCardFromToken(ctx context.Context, req port.BindFromTokenCommand) (string, error) {
    card, err := a.cardUC.BindCardFromToken(ctx, cardApp.BindFromTokenRequest{
        CardToken:    req.CardToken,
        ChannelToken: req.Token,
        Channel:      req.Channel,
        ShopperRef:   req.ShopperRef,
    })
    if err != nil {
        return "", err
    }
    return string(card.ID), nil
}

func (a *CardCommandAdapter) PrepareOneTimeToken(ctx context.Context, cardID, userID string) (string, error) {
    return a.cardUC.PrepareOneTimeToken(ctx, cardModel.SavedCardID(cardID), userID)
}
```

---

## Task 9: Handler & API 安全边界

### 9.1 令牌化 API

**File:** `internal/card/handler/http/card_handler.go` (修改)

新增 `POST /cards/tokenize` 路由：

```go
type TokenizeRequest struct {
    PAN            string `json:"pan"`
    ExpiryMonth    int    `json:"expiry_month"`
    ExpiryYear     int    `json:"expiry_year"`
    CVV            string `json:"cvv"`
    CardholderName string `json:"cardholder_name,omitempty"`
}

type TokenizeResponse struct {
    CardToken      string `json:"card_token"`
    MaskedPAN      string `json:"masked_pan"`
    Brand          string `json:"brand"`
    ExistingCardID string `json:"existing_card_id,omitempty"`
}
```

### 9.2 CardResponse 不暴露敏感字段

确认 `CardResponse` 不包含 `EncryptedPAN`、`PANHash`、`ChannelToken` 等敏感字段。
当前 `toResponse()` 只映射脱敏字段（last4, brand, expiry, holder, status），**已满足要求**。

### 9.3 日志脱敏

对应 `pci-ddd-analysis.md` §6.1.3 日志脱敏要求：
- `StripeVaultAdapter` / `LocalVault` 日志中禁止输出 card_token 完整值
- `stripe.Client` 日志中 API Key 已做 `safePrefix` 截断（已实现）
- PAN 永远不出现在日志中（`RawCardData` 不实现 `String()` / `GoString()`）

---

## Task 10: 密钥轮换（Key Rotation）

对应 `pci-ddd-analysis.md` §3.5 `KeyRotationService`、§6.1.2 信封加密架构密钥轮换流程、§8.1.2 管理 API。

### 10.1 KeyManager 端口增强（支持多版本 DEK 共存）

**File:** `internal/card/domain/port/key_manager.go` (修改)

```go
package port

// KeyVersion DEK 版本元数据
type KeyVersion struct {
    Version   int
    Status    string // "active" | "retiring" | "retired"
}

type KeyManager interface {
    // CurrentDEK 获取当前活跃的 DEK（最新版本）
    CurrentDEK() (dek []byte, version int, err error)

    // DEKByVersion 按版本号获取 DEK（新旧版本共存期间解密旧数据用）
    DEKByVersion(version int) ([]byte, error)

    // HMACKey 获取 HMAC 密钥
    HMACKey() ([]byte, error)

    // RotateDEK 生成新 DEK 版本，旧 DEK 保留（状态→retiring）
    // 返回新 DEK 版本号
    RotateDEK() (newVersion int, err error)

    // RetireDEK 将指定版本 DEK 标记为 retired（所有数据已迁移后调用）
    RetireDEK(version int) error

    // ListVersions 列出所有 DEK 版本及状态
    ListVersions() ([]KeyVersion, error)
}
```

**设计要点：**
- 轮换时新旧 DEK 必须共存。旧版本状态从 `active` → `retiring`（迁移中）→ `retired`（数据已全部重加密）
- `retired` 状态的 DEK 仍可通过 `DEKByVersion` 获取（容错：万一有遗漏的旧数据）
- 生产环境：`RotateDEK` 内部调用 KMS 生成新 Master Key → 加密新 DEK → 存入 `encryption_keys` 表

### 10.2 InMemKeyManager 增强（支持多版本）

**File:** `internal/card/adapter/keymanager/inmem_key_manager.go` (重构)

```go
package keymanager

import (
    "crypto/rand"
    "fmt"
    "sync"

    "payment-demo/internal/card/domain/port"
)

type dekEntry struct {
    dek     []byte
    status  string // "active", "retiring", "retired"
}

type InMemKeyManager struct {
    mu             sync.RWMutex
    deks           map[int]*dekEntry // version → DEK
    currentVersion int
    hmacKey        []byte
}

var _ port.KeyManager = (*InMemKeyManager)(nil)

func NewInMemKeyManager() *InMemKeyManager {
    dek := mustRandBytes(32)
    return &InMemKeyManager{
        deks: map[int]*dekEntry{
            1: {dek: dek, status: "active"},
        },
        currentVersion: 1,
        hmacKey:        mustRandBytes(32),
    }
}

func (m *InMemKeyManager) CurrentDEK() ([]byte, int, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    entry := m.deks[m.currentVersion]
    return entry.dek, m.currentVersion, nil
}

func (m *InMemKeyManager) DEKByVersion(version int) ([]byte, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    entry, ok := m.deks[version]
    if !ok {
        return nil, fmt.Errorf("DEK version %d not found", version)
    }
    return entry.dek, nil
}

func (m *InMemKeyManager) HMACKey() ([]byte, error) {
    return m.hmacKey, nil
}

// RotateDEK 生成新 DEK，旧版本状态 → retiring
func (m *InMemKeyManager) RotateDEK() (int, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 旧版本标记为 retiring
    if old, ok := m.deks[m.currentVersion]; ok {
        old.status = "retiring"
    }

    newVersion := m.currentVersion + 1
    m.deks[newVersion] = &dekEntry{
        dek:    mustRandBytes(32),
        status: "active",
    }
    m.currentVersion = newVersion
    return newVersion, nil
}

// RetireDEK 所有数据迁移完成后，旧 DEK 标记为 retired
func (m *InMemKeyManager) RetireDEK(version int) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    entry, ok := m.deks[version]
    if !ok {
        return fmt.Errorf("DEK version %d not found", version)
    }
    entry.status = "retired"
    return nil
}

func (m *InMemKeyManager) ListVersions() ([]port.KeyVersion, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    var versions []port.KeyVersion
    for v, entry := range m.deks {
        versions = append(versions, port.KeyVersion{
            Version: v,
            Status:  entry.status,
        })
    }
    return versions, nil
}

func mustRandBytes(n int) []byte {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        panic(fmt.Sprintf("failed to generate random bytes: %v", err))
    }
    return b
}
```

### 10.3 密钥轮换用例 [R-2 修复]

**File:** `internal/card/application/key_rotation_usecase.go` (新建)

`RotateAndMigrate` 是编排逻辑（查数据→解密→重加密→存储→更新状态），依赖 Repository IO，
不符合 CLAUDE.md "domain/service — 纯业务规则，不做编排" 约束。移到 application 层。

```go
package application

import (
    "context"
    "log"

    "payment-demo/internal/card/domain/model"
    "payment-demo/internal/card/domain/port"
)

// KeyRotationUseCase 密钥轮换用例（编排层）
// 对应 pci-ddd-analysis.md §3.5 KeyRotationService 的编排职责
type KeyRotationUseCase struct {
    keyMgr    port.KeyManager
    repo      port.CardRepository
    encrypter port.Encrypter
}

func NewKeyRotationUseCase(
    keyMgr port.KeyManager,
    repo port.CardRepository,
    enc port.Encrypter,
) *KeyRotationUseCase {
    return &KeyRotationUseCase{keyMgr: keyMgr, repo: repo, encrypter: enc}
}

// RotationResult 轮换结果
type RotationResult struct {
    OldVersion int
    NewVersion int
    Migrated   int
    Failed     int
}

// RotateAndMigrate 执行密钥轮换 + 数据重加密
// PCI 文档 §6.1.2 密钥轮换流程:
//   1. KMS 生成新 DEK (RotateDEK)
//   2. 后台逐条解密旧数据 → 用新 DEK 重新加密
//   3. 更新 key_version 字段
//   4. 旧 DEK 保留至所有数据迁移完成后标记 retired
//
// 注：Demo 全量同步执行；生产环境应改为分批迁移（batch + cursor）[S-4]
func (uc *KeyRotationUseCase) RotateAndMigrate(ctx context.Context) (*RotationResult, error) {
    _, oldVersion, err := uc.keyMgr.CurrentDEK()
    if err != nil {
        return nil, err
    }

    newVersion, err := uc.keyMgr.RotateDEK()
    if err != nil {
        return nil, err
    }
    log.Printf("[KeyRotation] Rotated DEK: v%d → v%d", oldVersion, newVersion)

    cards, err := uc.repo.FindByKeyVersion(ctx, oldVersion)
    if err != nil {
        return nil, err
    }

    oldDEK, err := uc.keyMgr.DEKByVersion(oldVersion)
    if err != nil {
        return nil, err
    }
    newDEK, err := uc.keyMgr.DEKByVersion(newVersion)
    if err != nil {
        return nil, err
    }

    result := &RotationResult{OldVersion: oldVersion, NewVersion: newVersion}
    for _, card := range cards {
        if err := uc.reencryptCard(ctx, card, oldDEK, newDEK, newVersion); err != nil {
            log.Printf("[KeyRotation] Failed to re-encrypt card %s: %v", card.ID, err)
            result.Failed++
            continue
        }
        result.Migrated++
    }

    if result.Failed == 0 {
        if err := uc.keyMgr.RetireDEK(oldVersion); err != nil {
            log.Printf("[KeyRotation] Failed to retire DEK v%d: %v", oldVersion, err)
        }
    }

    log.Printf("[KeyRotation] Complete: migrated=%d, failed=%d", result.Migrated, result.Failed)
    return result, nil
}

// [R-3 修复] 通过聚合根公开方法变更状态，不绕过私有 addEvent
func (uc *KeyRotationUseCase) reencryptCard(
    ctx context.Context,
    card *model.SavedCard,
    oldDEK, newDEK []byte,
    newVersion int,
) error {
    plaintext, err := uc.encrypter.Decrypt(card.EncryptedPAN.Ciphertext, oldDEK)
    if err != nil {
        return err
    }

    newCiphertext, err := uc.encrypter.Encrypt(plaintext, newDEK)
    if err != nil {
        return err
    }

    // 通过聚合根公开行为方法变更状态
    card.ReEncrypt(model.EncryptedPAN{
        Ciphertext: newCiphertext,
        KeyVersion: newVersion,
    })
    card.RecordPANDecryption("key_rotation")

    return uc.repo.Save(ctx, card)
}
```

### 10.4 CardRepository 增加按 KeyVersion 查询

**File:** `internal/card/domain/port/repository.go` (修改，追加)

```go
type CardRepository interface {
    // ...existing methods...

    // FindByKeyVersion 查询使用指定 DEK 版本的所有 active 卡（密钥轮换迁移用）
    FindByKeyVersion(ctx context.Context, keyVersion int) ([]*model.SavedCard, error)
}
```

### 10.5 HMAC Key 轮换的特殊处理

**设计决策：HMAC Key 不轮换（或极低频率轮换）。**

PCI 文档 §6.1.2 的密钥轮换流程仅针对 **DEK（加密密钥）**，因为：
- DEK 轮换后，旧密文用旧 DEK 解密、新 DEK 重加密即可，`pan_hash` 不变
- HMAC Key 轮换会导致所有 `pan_hash` 失效，查重索引 `UNIQUE(user_id, pan_hash)` 全部需要重算
- 重算 pan_hash 需要解密所有 PAN → 重新 HMAC → 更新哈希，代价远高于 DEK 轮换

**Demo 策略：**
- `HMACKey()` 返回固定密钥，不参与轮换
- 如果未来确需 HMAC Key 轮换（极端安全事件），采用"双 hash 查重"过渡期：
  新建 `pan_hash_v2` 列 → 重加密时同步计算新 HMAC → 过渡期内查重查两列 → 全量迁移后删旧列

### 10.6 密钥轮换时序

```
运维 → POST /internal/v1/keys/rotate

  KeyRotationService.RotateAndMigrate():
    1. keyMgr.CurrentDEK() → v1
    2. keyMgr.RotateDEK() → v2 (v1 status: active→retiring)
    3. repo.FindByKeyVersion(ctx, 1) → [card_A, card_B, card_C]
    4. for each card:
       a. Decrypt(card.EncryptedPAN.Ciphertext, DEK_v1) → PAN 明文
       b. Encrypt(PAN, DEK_v2) → 新密文
       c. card.EncryptedPAN = {newCiphertext, KeyVersion: 2}
       d. repo.Save(card)                // 审计事件: PANDecrypted(reason=key_rotation)
    5. 全部成功 → keyMgr.RetireDEK(1)    // v1 status: retiring→retired

  ── 解密时的兼容性 ──────────────────────
  迁移期间，card_A (v2) 和 card_D (v1, 尚未迁移) 共存：
    EncryptionService.DecryptPAN(card.EncryptedPAN):
      → keyMgr.DEKByVersion(card.EncryptedPAN.KeyVersion)
      → 自动选择正确的 DEK 版本解密
```

---

## 完整流程时序

### 首购流程（新卡 + save_card=true）

```
前端 → POST /cards/tokenize {pan, expiry, cvv}
     ← {card_token: "ct_xxx", masked_pan: "****1234", brand: "visa"}

前端 → POST /purchase {card_token: "ct_xxx", save_card: true, product_id, merchant_id}

  UseCase:
    1. 查商品、计算金额
    2. card_token 路径（非 SavedCardID）
    3. Gateway.Authorize(card_token, amount)
    4. Stripe 返回 {provider_ref, auth_code, recurring_token: "pm_xxx"}
    5. save_card=true → CardCommand.BindCardFromToken(card_token, "stripe", "pm_xxx")
       → ConsumeCardToken(ct_xxx) 取出加密数据
       → 创建 SavedCard 聚合（含 EncryptedPAN + PANHash + ChannelToken[stripe=pm_xxx]）
       → 持久化
    6. MarkAuthorized → Save → 返回 txn
```

### 复购流程（已存卡 + 有 channel_token）

```
前端 → POST /purchase {saved_card_id: "xxx", product_id, merchant_id}

  UseCase:
    1. CardQuery.FindActiveCard(xxx)
       → SavedCardView{ChannelToken: "pm_xxx", Last4: "1234", ...}
    2. ChannelToken 非空 → 复购路径
       cardToken = CardToken{TokenID: "pm_xxx", ...}
    3. Gateway.Authorize(pm_xxx, amount)
       → 用 recurring token 扣款，不解密 PAN
    4. 授权成功（isRecurring=true，无需再存 token）
    5. MarkAuthorized → Save → 返回 txn
```

### 已存卡首次走新渠道（无 channel_token）[W-6]

```
前端 → POST /purchase {saved_card_id: "xxx", product_id, merchant_id}

  UseCase:
    1. CardQuery.FindActiveCard(xxx)
       → SavedCardView{ChannelTokens: {}, ...}
    2. ChannelTokens["stripe"] 不存在 → 无 channel_token
    3. CardCommand.PrepareOneTimeToken(xxx, userID)
       → card 上下文内部: 解密 PAN (+ 审计事件) → 缓存到 CardVault → 返回 "ct_yyy"
    4. Gateway.Authorize("ct_yyy", amount)
       → Stripe 用一次性 token 扣款
    5. Stripe 返回 {recurring_token: "pm_yyy"}
    6. isRecurring=false → CardCommand.StoreChannelToken(xxx, "stripe", "pm_yyy")
    7. 下次复购时 ChannelTokens["stripe"] = "pm_yyy"，走复购路径
```

---

## 测试策略

| 层级 | 测试重点 | 示例 |
|------|---------|------|
| **加密服务** | AES-256-GCM 加密→解密一致性、HMAC 确定性 | `TestEncryptDecryptRoundTrip`, `TestHMACDeterministic` |
| **Card 聚合** | ChannelToken CRUD 不变量、状态机、删卡批量 revoke | `TestStoreChannelToken_Overwrite`, `TestDeleteCard_RevokesAllTokens` |
| **CardUseCase** | 令牌化查重、支付后绑卡竞态、ConsumeToken 一次性 | `TestTokenize_DuplicateCard_ReturnsExisting`, `TestBindFromToken_ConcurrentDedup` |
| **ChargeUseCase** | 复购路径用 ChannelToken、首购回存 token | `TestPurchase_Recurring_UsesChannelToken`, `TestPurchase_FirstPurchase_StoresRecurringToken` |
| **API Handler** | 响应不含敏感字段、令牌化参数校验 | `TestCardResponse_NoSensitiveFields`, `TestTokenize_InvalidPAN_Returns400` |
| **密钥轮换** | 多版本 DEK 共存、轮换后旧数据可解密、重加密一致性 | `TestRotateDEK_OldDataStillDecryptable`, `TestReencryptCard_RoundTrip`, `TestRotateAndMigrate_AllCardsUpdated` |
| **HMAC 稳定性** | 轮换 DEK 后 pan_hash 不变、查重仍有效 | `TestDEKRotation_PANHashUnchanged` |

---

## 实现顺序（依赖拓扑）

```
Task 1 (值对象+端口)
  └── Task 2 (加密服务+适配器)
        └── Task 3 (聚合根重构: SavedCard + ChannelToken)
              ├── Task 4 (Vault 端口+适配器)
              │     └── Task 5 (Card UseCase 重构: 令牌化+绑卡)
              │           └── Task 6 (Payment 侧端口改动)
              │                 └── Task 7 (Stripe Adapter 增强)
              │                       └── Task 8 (ACL Adapter 增强)
              │                             └── Task 9 (Handler + API)
              └── Task 10 (密钥轮换: KeyRotationService + 多版本 DEK)
                    ↑ 依赖 Task 2 (Encrypter) + Task 3 (聚合根) + Task 4 (Repository)
```

Task 10 可与 Task 6-9 并行开发（无交叉依赖）。

每个 Task 完成后运行 `go build ./...` + 对应 `go test` 确认无回退。
