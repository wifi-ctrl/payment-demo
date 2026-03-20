# DDD + 六边形架构实战：从零搭建游戏支付商城

## 目录

- [第一部分：理论基础](#第一部分理论基础)
  - [1. DDD 核心概念](#1-ddd-核心概念)
  - [2. 六边形架构](#2-六边形架构)
- [第二部分：实战 — 游戏支付商城](#第二部分实战--游戏支付商城)
  - [3. 战略设计：限界上下文划分](#3-战略设计限界上下文划分)
    - [3.4 Guide ↔ Demo 映射](#34-guide--demo-映射)
  - [4. 用户认证上下文（Identity）](#4-用户认证上下文identity)
  - [5. 商品目录上下文（Catalog）](#5-商品目录上下文catalog)
  - [6. 定价上下文（Pricing）](#6-定价上下文pricing)
  - [7. 支付上下文（Payment）](#7-支付上下文payment)
  - [8. 发货上下文（Fulfillment）](#8-发货上下文fulfillment)
  - [9. 上下文间协作](#9-上下文间协作)
- [第三部分：工程落地](#第三部分工程落地)
  - [10. 项目结构](#10-项目结构)
  - [11. Go 主流架构对比](#11-go-主流架构对比)
  - [12. 测试策略](#12-测试策略)
  - [13. 常见误区](#13-常见误区)

---

# 第一部分：理论基础

## 1. DDD 核心概念

### 1.1 什么是 DDD

领域驱动设计（Domain-Driven Design）的核心思想：**复杂软件的本质难题不在技术，而在业务领域本身。好的软件设计应该由领域模型驱动，而非由数据库表或 UI 驱动。**

| 层面 | 关注点 | 产出 |
|------|--------|------|
| **战略设计** | 系统如何拆分，团队如何协作 | 限界上下文、上下文映射 |
| **战术设计** | 单个上下文内部如何建模 | 实体、值对象、聚合、领域服务 |

### 1.2 战略设计

#### 统一语言（Ubiquitous Language）

团队在特定上下文内使用**同一套术语**。支付领域有一套精确的行业术语：

##### A. 行业标准术语

| 术语 | 含义 | 容易混淆的点 |
|------|------|-------------|
| **Charge（扣款）** | 向持卡人收取费用的完整动作 | 不等于 Authorization |
| **Authorization（预授权）** | 冻结持卡人额度，但不实际扣款 | 先 Auth 后 Capture 是常见模式 |
| **Capture（请款）** | 对已授权的金额实际扣款 | 超时未 Capture 则授权自动释放 |
| **Settlement（结算）** | 资金从发卡行转移到商户账户 | 与 Capture 不同步，通常 T+1~T+3 |
| **Refund（退款）** | 将已结算的资金退还持卡人 | 与 Void（撤销未结算授权）不同 |
| **Chargeback（拒付）** | 持卡人通过发卡行发起的争议退款 | 商户被动，需提供证据抗辩 |
| **Acquirer / Issuer** | 收单行 / 发卡行 | Stripe/Adyen 扮演 Acquirer 角色 |
| **PAN → Tokenization** | 卡号 → 令牌化 | PAN 严禁存储，必须 Token 化 |
| **BIN（银行识别号）** | PAN 前 6~8 位 | 可用于风控判断卡类型/地区 |
| **3DS（3D Secure）** | 持卡人身份验证协议 | 降低 Chargeback 风险，转移责任 |

##### B. 系统内部核心概念

以下术语在代码中有明确对应，需全团队统一理解：

| 术语 | 所属上下文 | 代码对应 | 定义 |
|------|-----------|---------|------|
| **CardToken（ct_*）** | Acquiring / Card | `model.CardToken` | 前端通过 `/cards/tokenize` 获得的临时令牌，以 `ct_` 前缀标识；包含加密的 PAN，有 TTL（默认 15 分钟） |
| **GatewayToken** | Acquiring | `ResolvedCard.GatewayToken` | card 服务在 Authorize 前解密 PAN 后生成的另一个临时 `ct_*`，含 RawPAN，仅供网关使用；原始 `ct_*` 保留给 Capture 后绑卡 |
| **PSP 原生 Token（tok_*）** | Acquiring | — | Stripe/Adyen 等 PSP 在客户端 SDK 完成令牌化后返回的 token（如 `tok_visa_xxx`），由 PSP 保管卡号，服务端直接传给网关 |
| **RecurringToken / ChannelToken** | Acquiring / Card | `txn.RecurringToken`, `ChannelToken` | 渠道返回的复购令牌。**RecurringToken** 是 Acquiring 上下文视角（记录在交易上），**ChannelToken** 是 Card 上下文视角（绑定在 SavedCard 上，按 channel 分组）。二者指同一个值 |
| **SavedCard** | Card | `model.SavedCard` | 经过支付验证（Capture 成功）后持久化的卡记录，聚合根。包含 EncryptedPAN、PANHash、CardMask、ChannelToken 列表 |
| **Card Vault** | Card | `port.CardVault` | 临时卡数据保险库（缓存层），存放 Tokenize 后尚未持久化的卡数据。支持 `Cache → Peek → Consume` 生命周期。Demo 用内存实现，生产用 Redis GETDEL |
| **ProviderRef** | Acquiring | `txn.ProviderRef` | 外部支付商返回的交易引用 ID（如 Stripe 的 `ch_xxx`、PayPal 的 `PAYID-xxx`），用于 Capture/Refund 时定位远端交易 |
| **ShopperRef** | Card | `ChannelToken.ShopperRef` | 渠道侧的"购物者引用"，用于关联同一持卡人的多次交易。通常等于 ProviderRef，但语义不同 |
| **Money** | Shared | `shared/money.Money` | 金额值对象：`Amount`（最小货币单位，如分/美分）+ `Currency`（ISO 4217）。禁止裸 `float64`/`int` 表示金额 |
| **Basis Point（基点）** | Acquiring / Coupon | `MultiplyBasisPoint` | 万分之一，用于税率和百分比折扣计算。1000 基点 = 10% |
| **DEK（Data Encryption Key）** | Card | `EncryptedPAN.KeyVersion` | 数据加密密钥，AES-256-GCM，由 KMS 管理。每个 `EncryptedPAN` 记录加密时使用的 DEK 版本号，支持密钥轮换时多版本共存 |
| **PANHash** | Card | `model.PANHash` | HMAC-SHA-256(hmac_key, PAN) 的不可逆哈希，用于查重（同一用户是否已存过该卡），不可用于逆推 PAN |
| **CardMask** | Card | `model.CardMask` | 卡的脱敏展示信息：Last4、Brand、ExpireMonth、ExpireYear。展示用，不含敏感数据 |
| **Channel** | Acquiring | `ChannelCredentialView.Channel` | 支付渠道标识（如 `stripe`、`adyen`、`paypal`）。一个商户可配多个 Channel，每个 Channel 有独立凭据 |
| **Order** | Order | `model.Order` | 订单聚合根，负责锁定定价明细（原价、折扣、税、最终金额）并关联 Payment 交易。状态机：PENDING_PAYMENT → AUTHORIZED → PAID → REFUNDED / FAILED |
| **PriceBreakdown** | Order | `model.PriceBreakdown` | 定价明细值对象：OriginalAmount + DiscountAmount + TaxAmount + FinalAmount，由 Order 在创建时计算并锁定 |
| **OrderStatus** | Order | `model.OrderStatus` | 订单状态枚举：PENDING_PAYMENT（等待支付）、AUTHORIZED（已授权）、PAID（已支付）、REFUNDED（已退款）、FAILED（失败） |
| **PaymentCommand** | Order | `port.PaymentCommand` | Order 上下文对 Acquiring 上下文的操作端口，包含 Charge、Capture、Refund 三个方法 |

##### C. 聚合根状态机

**PaymentTransaction（支付交易）**

```
CREATED ──Authorize──→ AUTHORIZED ──Capture──→ CAPTURED ──Refund──→ REFUNDED
   │                      │                       │
   └───── MarkFailed ─────┴────── MarkFailed ─────┘ → FAILED
```

- `CREATED → AUTHORIZED`：网关预授权成功，记录 ProviderRef / AuthCode / Channel / RecurringToken
- `AUTHORIZED → CAPTURED`：网关扣款成功，**触发绑卡 / 存 ChannelToken**（唯一同步触发点）
- `CAPTURED → REFUNDED`：网关退款成功
- 任意状态 → `FAILED`：记录失败原因

**SavedCard（已保存卡）**

```
[不存在] ──BindCardFromToken──→ ACTIVE ←→ SUSPENDED
                                  │
                                  └──Delete──→ DELETED
```

- 只有 Capture 成功后才会创建 SavedCard（`ACTIVE`）
- `ACTIVE ↔ SUSPENDED`：用户可暂停/恢复
- `ACTIVE/SUSPENDED → DELETED`：软删除

**Coupon（优惠券）**

```
ACTIVE ──Apply()──→ ACTIVE（UsedCount++）──达到 MaxUses──→ EXHAUSTED
  │
  └──超过 ValidUntil──→ EXPIRED
```

**Order（订单）**

```
PENDING_PAYMENT ──Authorize──→ AUTHORIZED ──Capture──→ PAID ──Refund──→ REFUNDED
       │
       └───── MarkFailed ──→ FAILED
```

- `PENDING_PAYMENT → AUTHORIZED`：PaymentCommand.Charge 成功，记录 TransactionID
- `AUTHORIZED → PAID`：PaymentCommand.Capture 成功
- `PAID → REFUNDED`：PaymentCommand.Refund 成功
- `PENDING_PAYMENT → FAILED`：Charge 失败

##### D. 跨上下文映射（ACL 投影对照）

同一业务实体在不同上下文中有不同的视图名称和字段子集：

| 源上下文 | 源类型 | 目标上下文 | ACL 视图类型 | 保留字段 |
|---------|--------|-----------|-------------|---------|
| Card | `SavedCard` | Acquiring | `SavedCardView` | CardID, UserID, ChannelTokens(channel→token), Last4, Brand, IsActive |
| Catalog | `Product` | Order | `ProductView` | ID, Name, Amount, Currency, IsActive |
| Coupon | `Coupon.Apply()` 结果 | Order | `AppliedCoupon` | CouponID, DiscountType, DiscountValue |
| Acquiring | `ChargeResult` | Order | `ChargeResult` | TransactionID, Status, ProviderRef |
| Card（Vault） | `CachedCardData` | Acquiring | `ResolvedCard` | Last4, Brand, GatewayToken |

**规则：Acquiring 上下文只依赖自己 `domain/port/` 中定义的视图类型，绝不 import 其他上下文的 `domain/model/`。ACL 适配器（`adapter/card/`）负责翻译。**

**同一个词在不同上下文中含义不同，这正是需要划分上下文的信号。** 比如 "Card" 在支付上下文中指信用卡（有 PAN、BIN、Expiry），在游戏上下文中可能指卡牌（有攻击力、稀有度）。

#### 限界上下文（Bounded Context）

一个明确的**语义边界**，边界内术语、模型、规则有唯一且一致的含义。

```
┌─ 支付上下文 ────────────────┐     ┌─ 发货上下文 ────────────────┐
│                             │     │                             │
│ Transaction = 一笔扣款记录   │     │ Transaction = 一次道具投递   │
│   有 Authorization          │     │   有 Recipient (角色)        │
│   有 Capture / Settlement   │     │   有 RetryPolicy            │
│   有 Chargeback 风险        │     │   有 GameOrderID            │
│                             │     │                             │
└─────────────────────────────┘     └─────────────────────────────┘
```

> 限界上下文不是微服务。它是逻辑边界，不是部署边界。

#### 上下文映射（Context Mapping）

用支付行业中真实存在的协作关系来说明：

| 关系模式 | 支付行业实例 |
|----------|-------------|
| **合作关系 (Partnership)** | **收单 ↔ 清结算**：Capture 后 Settlement 必须同步记账，双方同步迭代数据格式 |
| **客户-供应商 (Customer-Supplier)** | **Issuer → Acquirer**：Issuer 定义授权响应格式（AuthCode、Decline Reason），Acquirer 适配 |
| **遵奉者 (Conformist)** | **商户 → 卡组织 (Visa/Mastercard)**：ISO 8583 消息格式、Chargeback 流程，只能遵守。Apple IAP / Google Play Billing 的 Price Tier 同理 |
| **防腐层 (ACL)** | **我们 ← Stripe / Adyen / PayPal**：每家 PSP 的 API 模型不同，防腐层翻译为统一领域模型 |
| **开放主机服务 (OHS)** | **支付网关对外暴露统一 Authorize/Capture/Void/Refund 接口**，多个商户系统共享同一协议 |
| **发布语言 (Published Language)** | `PaymentCaptured`、`ChargebackReceived` 等事件用 JSON Schema 定义，下游各自订阅 |

```
┌──────────┐   Conformist    ┌──────────────┐
│  Visa /  │ ◀────────────── │   Issuer     │
│Mastercard│   ISO 8583      │  (发卡行)     │
│(卡组织)   │                 └──────┬───────┘
└──────────┘                        │ Customer-Supplier
                                    ▼
                            ┌───────────────┐        ACL
      Partnership           │   Acquirer    │ ◀──────────── ┌───────────┐
┌─────────────────────────▶│  (收单/PSP)    │               │  我们的    │
│    ┌──────────────┐       │ Stripe/Adyen  │               │  支付网关  │── OHS ── 商户系统
│    │  Settlement  │       └───────────────┘               └─────┬─────┘
│    │   (清结算)    │                                      Published Language
│    └──────────────┘                                             │
│                                     ┌───────────┬───────────┬───┴─────────┐
│                                     ▼           ▼           ▼             ▼
│                                ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐
│                                │  订单  │  │  发货  │  │  对账  │  │  风控  │
│                                └────────┘  └────────┘  └────────┘  └────────┘
```

### 1.3 战术设计

以下所有示例均来自支付领域，同时也是第二部分实战中各上下文会用到的核心模型。

#### 实体（Entity） — 有唯一标识，状态可变

```go
// PaymentTransaction — 通过 TransactionID 唯一标识
// 生命周期: Authorization → Capture → Settlement (或 Void / Refund)
type PaymentTransaction struct {
    ID              TransactionID
    MerchantOrderID OrderID
    Amount          Money
    PaymentMethodID PaymentMethodID
    Status          TransactionStatus  // Authorized / Captured / Settled / Voided / Refunded
    AuthorizedAt    *time.Time
    CapturedAt      *time.Time
    SettledAt       *time.Time
    CreatedAt       time.Time
}

func (t *PaymentTransaction) Capture(amount Money, now time.Time) error {
    if t.Status != StatusAuthorized {
        return ErrInvalidStateTransition
    }
    if amount.GreaterThan(t.Amount) {
        return ErrCaptureExceedsAuthorization
    }
    t.Status = StatusCaptured
    t.CapturedAt = &now
    t.addEvent(PaymentCaptured{TransactionID: t.ID, Amount: amount, CapturedAt: now})
    return nil
}
```

#### 值对象（Value Object） — 无标识，按值判等，不可变

```go
// Money — 支付系统最核心的值对象，杜绝裸 float
type Money struct {
    Amount   decimal.Decimal
    Currency Currency
}

func (m Money) Add(other Money) (Money, error) {
    if m.Currency != other.Currency {
        return Money{}, errors.New("cannot add different currencies")
    }
    return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}, nil
}

func (m Money) ToCents() int64 {
    return m.Amount.Mul(decimal.NewFromInt(100)).IntPart()
}

// CardToken — Tokenization 后的卡片引用，替代 PAN
type CardToken struct {
    ProviderRef string    // pm_xxx / tok_xxx
    Last4       string
    Brand       CardBrand // Visa, Mastercard, Amex
    ExpMonth    int
    ExpYear     int
    BIN         string    // 前 6~8 位，用于风控
}

func (c CardToken) IsExpired(now time.Time) bool {
    expiry := time.Date(c.ExpYear, time.Month(c.ExpMonth+1), 0, 0, 0, 0, 0, time.UTC)
    return now.After(expiry)
}
```

> **识别技巧**：9.99 USD 和另一个 9.99 USD 没区别（Money 是值对象），但交易 A 和交易 B 即使金额相同也不能互换（Transaction 是实体）。

#### 聚合（Aggregate） — 事务一致性边界

```
┌─ PaymentTransaction 聚合 ─────────────────────────────────┐
│                                                            │
│  PaymentTransaction (聚合根)                                │
│    ├── Money (值对象 — 交易金额)                             │
│    ├── CardToken (值对象 — Tokenized 卡信息)                 │
│    └── AuthorizationResult (值对象 — Issuer 授权结果)        │
│                                                            │
│  规则: Capture 金额 ≤ Authorization 金额                     │
│  规则: 只有 Authorized 才能 Capture 或 Void                  │
│  规则: 只有 Settled 才能 Refund                              │
└────────────────────────────────────────────────────────────┘

设计原则:
• 聚合尽量小 — PaymentMethod 和 PaymentTransaction 是两个独立聚合
• 跨聚合通过 ID 引用 — Transaction 用 PaymentMethodID，不嵌入 PaymentMethod
• 跨聚合一致性通过领域事件 — PaymentCaptured 事件通知订单上下文
```

#### 领域服务 — 跨聚合的业务逻辑

```go
// AuthorizationService — 预授权决策涉及风控、BIN 检查、3DS 判定
// 不属于 Transaction 实体，也不属于 CardToken 值对象
type AuthorizationService struct{}

func (s *AuthorizationService) EvaluateAuthorization(
    card CardToken, amount Money, riskProfile MerchantRiskProfile, device DeviceFingerprint,
) AuthorizationDecision {
    decision := AuthorizationDecision{Approved: true}
    if card.BIN.IsHighRiskRegion() {
        decision.Require3DS = true
    }
    if amount.Amount.GreaterThan(riskProfile.AutoApproveLimit) {
        decision.RequireManualReview = true
    }
    if device.RecentTransactionCount > 3 {
        decision.Approved = false
        decision.DeclineReason = "velocity_check_failed"
    }
    return decision
}
```

#### 领域事件 — 交易状态机每一步跃迁

```go
type PaymentAuthorized struct {
    TransactionID TransactionID
    OrderID       OrderID
    Amount        Money
    AuthCode      string    // 发卡行返回的授权码
    AuthorizedAt  time.Time
}

type PaymentCaptured struct {
    TransactionID TransactionID
    OrderID       OrderID
    CapturedAmount Money  // 可以 ≤ 授权金额 (部分请款)
    CapturedAt    time.Time
}

type ChargebackReceived struct {
    TransactionID TransactionID
    OrderID       OrderID
    DisputeAmount Money
    Reason        ChargebackReason // Fraudulent, ProductNotReceived, Duplicate...
    RespondBy     time.Time        // 商户需在此日期前提交证据
}
```

#### 仓储 & 端口 — 领域层定义接口，基础设施层实现

```go
type TransactionRepository interface {
    FindByID(id TransactionID) (*PaymentTransaction, error)
    Save(txn *PaymentTransaction) error
}

// 支付网关端口 — 核心不知道背后是 Stripe 还是 Adyen
type PaymentGateway interface {
    Authorize(token CardToken, amount Money, idempotencyKey string) (*AuthorizationResult, error)
    Capture(providerTxnID string, amount Money) (*CaptureResult, error)
    Void(providerTxnID string) error
    Refund(providerTxnID string, amount Money) (*RefundResult, error)
}
```

---

## 2. 六边形架构

### 2.1 什么是六边形架构

六边形架构（Hexagonal Architecture），又名**端口与适配器**（Ports and Adapters），由 Alistair Cockburn 在 2005 年提出。核心思想：

> **应用程序的核心业务逻辑不应依赖于任何外部技术细节。外部世界通过"端口"与核心交互，每个端口可以有多个"适配器"实现。**

为什么叫"六边形"？六边形只是一种视觉隐喻——用多边形表示核心有**多个面（端口）**可以与外部对接，而不是传统三层架构那样只有上下两个方向。边数本身不重要，重要的是**内外分离**的对称结构：

```
                        ┌───────────────────────┐
     HTTP Handler ─────▶│ Port                  │
                        │  ┌─────────────────┐  │
     Stripe Webhook ───▶│  │   Domain +      │  │◀──────── PostgreSQL
                        │  │   Application   │  │
     MQ Consumer ──────▶│  │   (纯业务逻辑)   │  │◀──────── Stripe API (ACL)
                        │  │                 │  │
     Cron Job ─────────▶│  └─────────────────┘  │◀──────── Game Server (ACL)
                        │                  Port │
                        └───────────────────────┘
                         左侧: 驱动侧            右侧: 被驱动侧
                      (谁来调用我)               (我要调用谁)
```

### 2.2 端口与适配器

#### 端口（Port）

端口是**接口定义**，由核心定义，描述"核心提供什么能力"或"核心需要什么能力"。

| 类型 | 方向 | 说明 | 支付实例 |
|------|------|------|---------|
| **驱动端口 (Driving Port)** | 外 → 内 | 用例接口："外部可以要求核心做什么" | `ChargeUseCase`, `RefundUseCase` |
| **被驱动端口 (Driven Port)** | 内 → 外 | 依赖接口："核心需要什么基础设施能力" | `TransactionRepository`, `PaymentGateway` |

#### 适配器（Adapter）

适配器是端口的**具体实现**，负责技术细节。核心层对具体适配器一无所知。

| 类型 | 说明 | 支付实例 |
|------|------|---------|
| **驱动适配器** | 调用核心的外部入口 | HTTP Handler, Stripe Webhook, MQ Consumer, Cron Job |
| **被驱动适配器** | 核心所依赖的外部实现 | PostgresRepo, StripeGateway(ACL), GameServerClient(ACL) |

```go
// ===== 驱动端口 — "外部可以要求核心做什么" =====
type ChargeUseCase interface {
    Charge(cmd ChargeCommand) (*TransactionDTO, error)
}

// ===== 被驱动端口 — "核心需要什么能力" =====
// TransactionRepository, PaymentGateway (见 1.3 节)

// ===== 驱动适配器: HTTP Handler =====
type PaymentHTTPHandler struct {
    chargeUseCase ChargeUseCase
}
func (h *PaymentHTTPHandler) HandleCharge(w http.ResponseWriter, r *http.Request) {
    var req ChargeRequest
    json.NewDecoder(r.Body).Decode(&req)
    result, err := h.chargeUseCase.Charge(toCommand(req))
    // ...
}

// ===== 被驱动适配器: Stripe 支付网关 (防腐层) =====
type StripePaymentGateway struct { client *stripe.Client }

func (g *StripePaymentGateway) Authorize(token CardToken, amount Money, key string) (*AuthorizationResult, error) {
    // 领域模型 → Stripe API 模型
    params := &stripe.PaymentIntentParams{
        Amount:        stripe.Int64(amount.ToCents()),
        Currency:      stripe.String(string(amount.Currency)),
        PaymentMethod: stripe.String(token.ProviderRef),
        CaptureMethod: stripe.String("manual"), // 仅 Auth，不立即 Capture
        Confirm:       stripe.Bool(true),
    }
    params.SetIdempotencyKey(key)
    pi, err := g.client.PaymentIntents.New(params)
    if err != nil {
        return nil, mapStripeError(err) // Stripe 错误 → 领域错误
    }
    // Stripe 模型 → 领域模型
    return &AuthorizationResult{ProviderTxnID: pi.ID, AuthCode: pi.LatestCharge.AuthorizationCode}, nil
}
```

### 2.3 依赖规则

```
依赖方向: 外层 → 内层 (永远不反过来)

  ┌──────────────────────────────────┐
  │  适配器层 (Adapters)              │  HTTP Handler, Stripe Client, PostgreSQL Repo
  │  ┌────────────────────────────┐  │
  │  │  应用层 (Application)       │  │  ChargeService — 编排领域对象
  │  │  ┌──────────────────────┐  │  │
  │  │  │  领域层 (Domain)      │  │  │  Transaction, Money, PaymentGateway(Port)
  │  │  └──────────────────────┘  │  │
  │  └────────────────────────────┘  │
  └──────────────────────────────────┘

  领域层: 零外部依赖, 无框架/ORM/HTTP
  应用层: 仅依赖领域层
  适配器层: 依赖应用层和领域层
```

### 2.4 vs 传统三层架构

```
传统三层 (支付系统):                   六边形 (支付系统):

PaymentController                    Driving Adapter (HTTP / Webhook)
    ▼                                     ▼
PaymentService                       Application Service (编排 Auth→Capture)
  - 直接 import Stripe SDK                ▼
  - SQL 混在业务逻辑中               Domain (Transaction 状态机, Money, AuthorizationService)
    ▼                                     ▼ (通过 Port 接口)
PaymentDAO → Database                Driven Adapter (StripeGateway, PostgresRepo)

问题: 换 Stripe 为 Adyen 要改          优势: 换 Adyen 只改一个 Adapter
     Service 层大量代码                     领域层可纯单元测试
```

---

# 第二部分：实战 — 游戏支付商城

## 3. 战略设计：限界上下文划分

### 3.1 事件风暴

**参考架构**（完整支付系统含 Order + Fulfillment）：

```
[用户登录] → [浏览商品] → [选择SKU] → [计算价格] → [选择支付方式]
    │            │            │           │              │
 Identity     Catalog      Catalog     Pricing        Payment

→ [创建订单] → [发起支付] → [支付成功] → [发货] → [发货成功]
     │            │            │          │           │
  Order        Payment     Payment   Fulfillment  Fulfillment
```

**Demo 实现**（聚焦支付核心，省略 Fulfillment）：

```
[认证] → [浏览商品] → [选择商品] → [应用优惠券] → [创建订单]  → [Card/PayPal 支付]
  │          │            │            │              │                │
Identity  Catalog      Catalog      Coupon          Order         Acquiring
                                               (定价计算+订单管理)  (按金额扣款)
→ [授权] → [扣款] → [绑卡]  → [退款]
    │        │        │        │
Acquiring Acquiring  Card   Acquiring
          (→Order)  (→Card)  (→Order)
```

### 3.2 上下文全景

**Demo 实现的 6 个上下文：**

```
internal/
├── identity/      用户认证 — AuthMiddleware、UserID 注入 ctx
├── catalog/       商品目录 — 商品 CRUD、价格查询
├── card/          卡管理   — 绑卡（Stripe Vault tokenization）、挂起/激活/删除
├── acquiring/     收单核心 — 商户管理+渠道凭据+Card/PayPal 授权→扣款→退款（绑卡/存 ChannelToken）
├── coupon/        优惠券   — 创建、核销（Apply/Rollback Saga 补偿）
├── order/         订单管理 — 创建订单（查商品+应用优惠券+算税+锁定金额）、Capture、Refund
└── shared/        Shared Kernel — money, event, auth
```

```
┌──────────┐                                           ┌──────────┐
│ identity │─── AuthMiddleware ──────────────────────▶ 所有 handler
└──────────┘
                    ┌───────────┐        ┌──────────┐
                    │           │        │ catalog  │
                    │ acquiring │        └─────┬────┘
                    │ (商户管理   │              │ ACL
┌──────────┐  ACL   │ +按金额扣款)│        ┌─────┴────┐  ACL   ┌──────────┐
│   card   │◀───────│           │◀───────│  order   │───────▶│  coupon  │
└──────────┘        └─────┬─────┘        │(定价+订单)│        └──────────┘
                          │               └──────────┘
                          │ ACL (adapter/gateway/stripe, paypal)
                          ▼
                   ┌──────────────┐
                   │ 外部网关      │
                   │ Stripe/PayPal│
                   └──────────────┘
```

> **参考架构**中还包含 Pricing（独立定价）、Fulfillment（发货重试）两个上下文。
> Demo 已实现 Order 上下文负责定价编排；Pricing 简化为 `order/domain/service/price_calculator.go`（纯函数：原价-折扣+税）。Fulfillment 属于扩展方向。

### 3.3 上下文映射

**Demo 实际协作关系：**

```
identity  ──Shared Kernel──▶ 所有上下文    shared/auth（ctx 传递 userID）
order     ──ACL adapter────▶ catalog       adapter/catalog/ → CatalogQuery
order     ──ACL adapter────▶ coupon        adapter/coupon/ → CouponApplier
order     ──ACL adapter────▶ acquiring     adapter/acquiring/ → PaymentCommand（调 ChargeUseCase）
acquiring ──ACL adapter────▶ card          adapter/card/ → CardQuery, CardCommand
acquiring ──adapter/gateway─▶ Stripe        adapter/gateway/stripe/ (Client + GatewayAdapter)
acquiring ──adapter/gateway─▶ PayPal        adapter/gateway/paypal/ (Client + GatewayAdapter)
card      ──ACL adapter────▶ Stripe        card/adapter/ → StripeVaultAdapter
```

**参考架构**扩展后的事件流（Demo 未实现）：

```
Order    ──事件──────▶ Payment       OrderCreated → 发起支付
Payment  ──事件──────▶ Fulfillment   PaymentCaptured → 触发发货
Fulfillment ─事件───▶ Order         DeliverySucceeded → 更新订单
```

### 3.4 Guide ↔ Demo 映射

> **AI 迭代参考**：以下表格说明本 Guide 的参考设计与 Demo 实际实现的对应关系。
> 迭代 Demo 代码时，以 Demo 列为准；设计新功能时，以参考设计列为蓝图。

| 维度 | 参考设计（本 Guide） | Demo 实现（payment-demo/） | 说明 |
|---|---|---|---|
| **上下文** | Identity, Catalog, Pricing, Payment, Fulfillment, Order | Identity, Catalog, Card, Acquiring, Coupon, Order | Demo 增加了 Card/Coupon 独立上下文，Merchant+Payment 合并为 Acquiring，已实现 Order，省略了 Fulfillment |
| **Order** | 订单流转 + 状态管理 | `internal/order/`（创建订单+定价编排+Capture/Refund 委派） | Demo 实现了 Order 编排定价并委派 Acquiring 扣款 |
| **Pricing** | 独立上下文：PriceTable + Discount 聚合 | 合并到 `order/domain/service/price_calculator.go` | Demo 的定价逻辑简单（原价-折扣+税），合并到 Order 上下文 |
| **卡管理** | Payment 内部的 PaymentMethod 聚合 | 独立的 Card 上下文（SavedCard 聚合） | Demo 将卡管理拆为独立上下文，有独立生命周期和不变式 |
| **Money** | `decimal.Decimal` | `int64`（最小货币单位 cents） | Demo 用整数运算避免浮点问题，生产环境可替换为 decimal |
| **Gateway 接口** | `Authorize(token, amount, idempotencyKey)` + `Void` | `Authorize(ctx, token, amount)` + 无 Void | Demo 简化：无幂等键、无 Void，有 `context.Context` |
| **入口** | `cmd/server/main.go` | `main.go` + `internal/bootstrap/` | Demo 用 bootstrap 包拆分 Composition Root |
| **基础设施** | PostgreSQL + Redis + Kafka + Outbox | 全内存 + `adapter/gateway/stripe` + `adapter/gateway/paypal` | Demo 用内存仓储 + mock server，替换为真实实现只需换适配器 |
| **事件通信** | Kafka + Outbox 模式 | log 输出（聚合根产生 → ClearEvents → log） | Demo 的事件机制是占位实现 |
| **Shared Kernel** | `shared/events/` | `shared/event/` + `shared/money/` + `shared/auth/` | Demo 用单数命名（Go 规范），并增加了 money 和 auth |

---

## 4. 用户认证上下文（Identity）

### 4.1 领域模型

```go
// --- 聚合 ---
type User struct {
    ID         UserID
    ExternalID string      // 游戏平台账号 ID
    GameID     GameID
    Status     UserStatus  // Active, Banned
}

type Session struct {
    ID           SessionID
    UserID       UserID
    AccessToken  Token       // 值对象
    RefreshToken Token
    DeviceInfo   DeviceInfo  // 值对象
    ExpiresAt    time.Time
}

// --- 值对象 ---
type GameIdentity struct {
    GameID   GameID
    ServerID string
    RoleID   string
}

// --- 端口 ---
type UserRepository interface {
    FindByExternalID(externalID string, gameID GameID) (*User, error)
    Save(user *User) error
}
type SessionRepository interface {
    Save(session *Session) error
    FindByAccessToken(token string) (*Session, error)
}
type GameAuthVerifier interface {
    Verify(gameToken string) (*GameIdentity, error)
}
```

### 4.2 应用层

```go
type AuthService struct {
    users    UserRepository
    sessions SessionRepository
    verifier GameAuthVerifier   // ACL: 翻译游戏服务器响应 → GameIdentity
    tokenGen TokenGenerator
}

func (s *AuthService) LoginWithGameToken(cmd LoginCommand) (*AuthResult, error) {
    identity, err := s.verifier.Verify(cmd.GameToken)
    if err != nil { return nil, ErrInvalidGameToken }

    user, err := s.users.FindByExternalID(identity.RoleID, identity.GameID)
    if err != nil {
        user = NewUser(identity)
        s.users.Save(user)
    }
    if user.IsBanned() { return nil, ErrUserBanned }

    session := NewSession(user.ID, cmd.DeviceInfo, s.tokenGen)
    s.sessions.Save(session)
    return toAuthResult(user, session), nil
}
```

### 4.3 六边形全景

```
                ┌─ Identity Context ──────────────────────────┐
                │                                              │
 HTTP Handler ─▶│ AuthService                                  │
                │   ├── User / Session (聚合)                   │
                │   ├── Port: UserRepository ──────────────────│──▶ PostgreSQL
                │   ├── Port: SessionRepository ──────────────│──▶ Redis
                │   └── Port: GameAuthVerifier ────────────────│──▶ Game Server (ACL)
                └──────────────────────────────────────────────┘
```

---

## 5. 商品目录上下文（Catalog）

### 5.1 领域模型

```go
// --- 聚合根: Product ---
type Product struct {
    ID            ProductID
    GameID        GameID
    Type          ProductType     // VirtualCurrency, Bundle, Pass, Subscription
    Name          LocalizedText   // 值对象
    Status        ProductStatus   // Draft, Active, Offline
    PurchaseLimit PurchaseLimit   // 值对象
    Availability  TimeWindow      // 值对象
    SKUs          []SKU           // 聚合内实体
}

func (p *Product) CanPurchase(userPurchaseCount int, now time.Time) error {
    if !p.IsAvailable(now) { return ErrProductNotAvailable }
    if p.PurchaseLimit.IsExceeded(userPurchaseCount) { return ErrPurchaseLimitExceeded }
    return nil
}

// --- 聚合内实体: SKU ---
type SKU struct {
    ID          SKUID
    Tier        string         // "60钻石", "300钻石"
    RewardItems []RewardItem   // 值对象: 发货内容
    BonusItems  []RewardItem   // 额外赠送 (如首充双倍)
}

// --- 值对象 ---
type RewardItem struct { ItemID string; ItemType ItemType; Quantity int }
type PurchaseLimit struct { MaxCount int; Period LimitPeriod }
type TimeWindow struct { Start *time.Time; End *time.Time }

// --- 端口 ---
type ProductRepository interface {
    FindByID(id ProductID) (*Product, error)
    FindActiveByGameID(gameID GameID) ([]*Product, error)
    Save(product *Product) error
}
```

### 5.2 六边形全景

```
                ┌─ Catalog Context ─────────────────────────────┐
                │                                                │
 HTTP Handler ─▶│ CatalogService                                 │
 (玩家浏览)      │   ├── Product (聚合根) → SKU, RewardItem        │
 Admin API ────▶│   ├── Port: ProductRepository ─────────────────│──▶ PostgreSQL
 (运营后台)      │   └── Port: PurchaseCountQuery ────────────────│──▶ Redis
                └────────────────────────────────────────────────┘
```

---

## 6. 定价上下文（Pricing）

> **Demo 状态**：Demo 中定价逻辑已迁移到 Order 上下文的 `order/domain/service/price_calculator.go`（纯函数：原价-折扣+税）。
> Order 上下文负责查商品、应用优惠券、算税、锁定最终金额，然后调用 Acquiring 扣款。
> 以下是定价规则复杂化后独立为上下文的参考设计。

### 6.1 领域模型

定价独立为上下文，因为定价规则的复杂度值得单独封装。

```go
// --- 聚合根: PriceTable ---
type PriceTable struct {
    SKUID   SKUID
    Entries []PriceEntry // { Currency, Region, Amount }
}

func (t *PriceTable) GetPrice(currency Currency, region Region) (Money, error) {
    // 优先查区域价格，回退到全球默认
    for _, e := range t.Entries {
        if e.Currency == currency && e.Region == region { return e.Amount, nil }
    }
    for _, e := range t.Entries {
        if e.Currency == currency && e.Region == "" { return e.Amount, nil }
    }
    return Money{}, ErrPriceNotFound
}

// --- 聚合根: Discount ---
type Discount struct {
    ID         DiscountID
    Type       DiscountType  // Percentage, FixedAmount, PriceOverride
    Value      decimal.Decimal
    Scope      DiscountScope // All, Category, Product, SKU
    TargetIDs  []string
    Conditions DiscountConditions
    Stackable  bool
    Priority   int
    TimeWindow TimeWindow
}

func (d *Discount) Apply(price Money) Money {
    switch d.Type {
    case Percentage:   return price.ApplyDiscount(int(d.Value.IntPart()))
    case FixedAmount:  return price.Subtract(Money{Amount: d.Value, Currency: price.Currency})
    case PriceOverride: return Money{Amount: d.Value, Currency: price.Currency}
    }
    return price
}

// --- 领域服务: 计算最终价格 ---
type PricingService struct{}

func (s *PricingService) CalculateFinalPrice(
    table *PriceTable, skuID SKUID, currency Currency, region Region,
    discounts []*Discount, userSegment string, now time.Time,
) (PriceBreakdown, error) {
    basePrice, err := table.GetPrice(currency, region)
    if err != nil { return PriceBreakdown{}, err }

    applicable := filterApplicable(discounts, skuID, userSegment, now)
    sort.Slice(applicable, func(i, j int) bool { return applicable[i].Priority > applicable[j].Priority })

    finalPrice := basePrice
    for _, d := range applicable {
        finalPrice = d.Apply(finalPrice)
        if !d.Stackable { break }
    }
    return PriceBreakdown{BasePrice: basePrice, FinalPrice: finalPrice}, nil
}
```

### 6.2 六边形全景

```
                ┌─ Pricing Context ─────────────────────────────┐
                │                                                │
 HTTP Handler ─▶│ PricingAppService                              │
 (订单服务调用)   │   ├── PriceTable (聚合根)                       │
 Admin API ────▶│   ├── Discount (聚合根)                         │
 (运营配折扣)    │   ├── PricingService (领域服务)                   │
                │   ├── Port: PriceTableRepository ──────────────│──▶ PostgreSQL
                │   └── Port: DiscountRepository ────────────────│──▶ PostgreSQL
                └────────────────────────────────────────────────┘
```

---

## 6.5 订单上下文（Order）

> **Demo 实现**：`internal/order/` — 完整 DDD 分层，负责定价编排和订单生命周期管理。

### 6.5.1 领域模型

Order 聚合根负责：查商品 → 应用优惠券 → 算税 → 锁定 FinalAmount → 关联 Payment 交易。

```go
type Order struct {
    ID            OrderID
    UserID        string
    MerchantID    string
    ProductID     string
    ProductName   string
    Status        OrderStatus   // PENDING_PAYMENT → AUTHORIZED → PAID → REFUNDED / FAILED
    Price         PriceBreakdown // OriginalAmount, DiscountAmount, TaxAmount, FinalAmount
    CouponID      string
    TransactionID string        // 关联的 Payment 交易 ID
}
```

状态机：
```
PENDING_PAYMENT ──Authorize──→ AUTHORIZED ──Capture──→ PAID ──Refund──→ REFUNDED
       │
       └───── MarkFailed ──→ FAILED
```

端口（消费方定义）：
- `CatalogQuery` — 查商品原价
- `CouponApplier` — 应用优惠券（Apply + Rollback Saga 补偿）
- `TaxRateQuery` — 查税率
- `PaymentCommand` — 调用 Acquiring 上下文（Charge, Capture, Refund）
- `OrderRepository` — 订单持久化

### 6.5.2 六边形全景

```
                ┌─ Order Context ──────────────────────────────────┐
                │                                                   │
 HTTP Handler ─▶│ OrderUseCase                                      │
 POST /orders   │   ├── Order (聚合根) → PriceBreakdown 值对象       │
 POST /orders/  │   ├── Port: OrderRepository ─────────────────────│──▶ InMemory
   {id}/capture │   ├── Port: CatalogQuery ────────────────────────│──▶ Catalog (ACL)
 POST /orders/  │   ├── Port: CouponApplier ───────────────────────│──▶ Coupon (ACL)
   {id}/refund  │   ├── Port: TaxRateQuery ────────────────────────│──▶ Static Config
                │   └── Port: PaymentCommand ──────────────────────│──▶ Acquiring (ACL → ChargeUseCase)
                └───────────────────────────────────────────────────┘
```

---

## 7. 支付上下文（Payment）

> **Demo 差异**：
> - Demo 将卡管理拆为独立的 **Card** 上下文（`internal/card/`），而非本节的 PaymentMethod 子聚合
> - Demo 将 Merchant + Payment 合并为 **Acquiring**（收单）上下文（`internal/acquiring/`），商户管理与支付流程共享同一上下文
> - Demo 新增 **Coupon** 上下文，UseCase 中有 Apply/Rollback Saga 补偿
> - Demo 引入了 **Order** 上下文负责定价编排，Acquiring 只按 Order 给定的 FinalAmount 扣款
> - Acquiring 不再依赖 CatalogQuery、CouponApplier、TaxRateQuery
> - `PurchaseRequest` 接收 `OrderID` + `Amount`（由 Order 锁定的最终金额），不再含 `ProductID`/`CouponCode`
> - `PaymentTransaction` 用 `OrderID` 替代了 `ProductID`，移除了 `DiscountAmount`/`TaxAmount`/`CouponID`
> - Demo 的 `PaymentGateway` 接口带 `context.Context`，无 `idempotencyKey` 和 `Void`
> - Demo 的渠道 HTTP 客户端（Stripe / PayPal）归属 `acquiring/adapter/gateway/` 子包，与 GatewayAdapter 同包
> - 详细实现见 `README.md` 和 `internal/acquiring/` 源码

领域模型（`PaymentTransaction`, `Money`, `CardToken`, `PaymentGateway` 端口）已在 1.3 节完整定义，此处聚焦**应用层编排**和**适配器层**中 1.3 节未覆盖的内容。

### 7.1 卡管理 — PaymentMethod 聚合

```go
type PaymentMethod struct {
    ID          PaymentMethodID
    UserID      UserID
    Type        PaymentType       // Card, PayPal, Alipay, GooglePay
    Provider    PaymentProvider   // Stripe, Adyen
    Token       CardToken         // 值对象 (见 1.3)
    IsDefault   bool
    Status      PaymentMethodStatus // Active, Expired, Removed
}

func (pm *PaymentMethod) Remove() error {
    if pm.Status == Removed { return ErrAlreadyRemoved }
    pm.Status = Removed
    return nil
}

type PaymentMethodRepository interface {
    FindByUserID(userID UserID) ([]*PaymentMethod, error)
    FindByID(id PaymentMethodID) (*PaymentMethod, error)
    Save(pm *PaymentMethod) error
}
```

### 7.2 应用层 — ChargeService

```go
type ChargeService struct {
    methods      PaymentMethodRepository
    transactions TransactionRepository  // 见 1.3
    gateway      PaymentGateway          // 见 1.3, 由 StripePaymentGateway 实现
    eventBus     EventPublisher
}

func (s *ChargeService) Charge(cmd ChargeCommand) (*TransactionDTO, error) {
    // 1. 查找支付方式
    pm, err := s.methods.FindByID(cmd.PaymentMethodID)
    if err != nil { return nil, err }
    if pm.UserID != cmd.UserID { return nil, ErrUnauthorized }
    if pm.Token.IsExpired(time.Now()) { return nil, ErrCardExpired }

    // 2. 创建交易记录
    txn := NewTransaction(cmd.OrderID, cmd.PaymentMethodID, cmd.Amount)
    s.transactions.Save(txn)

    // 3. 调用支付网关 Authorize (通过端口, ACL 隔离 Stripe)
    authResult, err := s.gateway.Authorize(pm.Token, cmd.Amount, cmd.IdempotencyKey)
    if err != nil {
        txn.MarkFailed(err.Error())
        s.transactions.Save(txn)
        s.eventBus.PublishAll(txn.Events())
        return toDTO(txn), nil
    }

    // 4. 虚拟商品即时交付 → 立即 Capture
    txn.Authorize(authResult)
    txn.Capture(cmd.Amount, time.Now())
    s.transactions.Save(txn)

    // 5. 发布 PaymentCaptured 事件 → 触发发货
    s.eventBus.PublishAll(txn.Events())
    return toDTO(txn), nil
}
```

### 7.3 Webhook 驱动适配器

```go
// Stripe 异步通知 — 另一个驱动适配器 (不是 HTTP API, 而是 Webhook)
type StripeWebhookHandler struct {
    methods       PaymentMethodRepository
    webhookSecret string
}

func (h *StripeWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
    event, err := webhook.ConstructEvent(readBody(r),
        r.Header.Get("Stripe-Signature"), h.webhookSecret)
    if err != nil { http.Error(w, "Invalid signature", 400); return }

    switch event.Type {
    case "setup_intent.succeeded":
        // 绑卡成功 → 保存 PaymentMethod
        h.handleSetupSucceeded(event.Data)
    case "charge.dispute.created":
        // Chargeback → 发布 ChargebackReceived 事件
        h.handleChargeback(event.Data)
    }
    w.WriteHeader(200)
}
```

### 7.4 六边形全景

```
                ┌─ Payment Context ────────────────────────────────┐
                │                                                   │
 HTTP Handler ─▶│ ChargeService / BindCardService                   │
 Stripe Webhook▶│   ├── PaymentTransaction (聚合, 见 1.3)            │
                │   ├── PaymentMethod (聚合)                         │
                │   ├── Money / CardToken (值对象, 见 1.3)            │
                │   ├── Port: TransactionRepo ──────────────────────│──▶ PostgreSQL
                │   ├── Port: PaymentMethodRepo ────────────────────│──▶ PostgreSQL
                │   ├── Port: PaymentGateway ───────────────────────│──▶ Stripe (ACL)
                │   └── Port: EventPublisher ───────────────────────│──▶ Kafka
                └───────────────────────────────────────────────────┘
```

---

## 8. 发货上下文（Fulfillment）

> **Demo 状态**：Demo 未实现 Fulfillment 上下文。以下是生产环境中发货上下文的参考设计。
> 扩展时，新建 `internal/fulfillment/` 完整分层 + `bootstrap/wire_fulfillment.go`，
> 监听 `PaymentCaptured` 事件触发发货。

### 8.1 领域模型

```go
// --- 聚合根: Delivery ---
type Delivery struct {
    ID            DeliveryID
    OrderID       OrderID
    Recipient     GameRecipient  // 值对象
    Items         []DeliveryItem // 值对象
    Status        DeliveryStatus // Pending, Processing, Succeeded, Failed
    RetryPolicy   RetryPolicy    // 值对象
    RetryCount    int
    NextRetryAt   *time.Time
    GameOrderID   string
    FailureReason string
    events        []DomainEvent
}

type RetryPolicy struct {
    MaxRetries int
    Intervals  []time.Duration // [0s, 10s, 30s, 2m, 10m]
}

// --- 领域行为 ---
func (d *Delivery) Succeed(gameOrderID string, now time.Time) {
    d.Status = Succeeded
    d.GameOrderID = gameOrderID
    d.events = append(d.events, DeliverySucceeded{DeliveryID: d.ID, OrderID: d.OrderID})
}

func (d *Delivery) Fail(reason string, now time.Time) {
    d.FailureReason = reason
    interval, canRetry := d.RetryPolicy.NextInterval(d.RetryCount)
    if canRetry {
        d.Status = Pending
        next := now.Add(interval)
        d.NextRetryAt = &next
    } else {
        d.Status = Failed
        d.events = append(d.events, DeliveryFailed{DeliveryID: d.ID, OrderID: d.OrderID, Reason: reason})
    }
}

// --- 端口 ---
type DeliveryRepository interface {
    Save(delivery *Delivery) error
    FindPendingRetries(now time.Time, limit int) ([]*Delivery, error)
}

type GameDeliveryGateway interface {
    Deliver(recipient GameRecipient, items []DeliveryItem, deliveryID DeliveryID) (*GameDeliveryResult, error)
}
```

### 8.2 应用层

```go
type FulfillmentService struct {
    deliveries DeliveryRepository
    gateway    GameDeliveryGateway  // ACL: 翻译领域模型 → 游戏服务器协议 (含 HMAC 签名)
    eventBus   EventPublisher
}

// 事件驱动: 监听 PaymentCaptured
func (s *FulfillmentService) OnPaymentCaptured(event PaymentCaptured) error {
    delivery := NewDelivery(event.OrderID, event.Recipient, event.Items)
    s.deliveries.Save(delivery)
    return s.executeDelivery(delivery)
}

func (s *FulfillmentService) executeDelivery(d *Delivery) error {
    d.Attempt()
    result, err := s.gateway.Deliver(d.Recipient, d.Items, d.ID)
    if err != nil || !result.Success {
        d.Fail(errorReason(err, result), time.Now())
    } else {
        d.Succeed(result.GameOrderID, time.Now())
    }
    s.deliveries.Save(d)
    s.eventBus.PublishAll(d.Events())
    return nil
}

// 定时任务: 处理待重试的发货
func (s *FulfillmentService) ProcessPendingRetries() error {
    deliveries, _ := s.deliveries.FindPendingRetries(time.Now(), 100)
    for _, d := range deliveries { s.executeDelivery(d) }
    return nil
}
```

### 8.3 六边形全景

```
                ┌─ Fulfillment Context ──────────────────────────────┐
                │                                                     │
 MQ Consumer ──▶│ FulfillmentService                                  │
 (PaymentCaptured)│ ├── Delivery (聚合根)                              │
 Cron Job ─────▶│   │   ├── RetryPolicy (值对象)                      │
 (定时重试)      │   │   └── GameRecipient (值对象)                     │
 HTTP (补发) ──▶│   ├── Port: DeliveryRepository ──────────────────────│──▶ PostgreSQL
                │   ├── Port: GameDeliveryGateway ─────────────────────│──▶ Game Server (ACL)
                │   └── Port: EventPublisher ──────────────────────────│──▶ Kafka
                └─────────────────────────────────────────────────────┘
```

---

## 9. 上下文间协作

> **Demo 状态**：Demo 中上下文间通过 ACL adapter 直接调用（同进程），事件仅 log 输出。
> 以下事件驱动流程、Outbox 模式、事件格式规范属于生产环境的参考设计。

### 9.1 事件驱动流程

```
Order ──OrderCreated──▶ Payment ──PaymentCaptured──▶ Fulfillment
  ▲                                                       │
  └──────────── DeliverySucceeded / DeliveryFailed ────────┘

如果发货失败:
  Fulfillment: 按 RetryPolicy 指数退避重试 [0s, 10s, 30s, 2m, 10m]
  超过上限 → 发布 DeliveryFailed → Order 更新状态 → Payment 自动 Refund
```

### 9.2 Outbox 模式保证事件投递

```
┌─────────────────────────────────────┐
│  同一个数据库事务                      │
│  1. UPDATE orders SET status='paid' │
│  2. INSERT INTO outbox (event)      │
│  COMMIT                             │
└──────────────┬──────────────────────┘
               ▼
┌─────────────────────────────────────┐
│  Outbox Poller (独立进程)             │
│  1. SELECT FROM outbox WHERE !published │
│  2. 发布到 Kafka                     │
│  3. UPDATE outbox SET published=true│
└─────────────────────────────────────┘
```

### 9.3 事件格式规范

```json
{
  "event_id": "evt_abc123",
  "event_type": "payment.captured",
  "aggregate_id": "txn_456",
  "occurred_at": "2024-03-10T12:00:00Z",
  "payload": {
    "order_id": "ord_789",
    "captured_amount": { "value": "9.99", "currency": "USD" }
  },
  "metadata": { "correlation_id": "corr_xyz" }
}
```

---

# 第三部分：工程落地

## 10. 项目结构

### 10.1 项目结构（模块化单体）

多个限界上下文在同一个服务中，按上下文顶层分包，每个上下文内部遵循相同的分层。

**Demo 实际目录结构（`payment-demo/`）：**

```
payment-demo/
├── main.go                                       # 入口（调用 bootstrap.New）
│
├── internal/
│   ├── bootstrap/                                # Composition Root — 组装所有上下文
│   │   ├── app.go                                #   App struct + New() 总编排
│   │   ├── wire_identity.go                      #   identity 上下文组装
│   │   ├── wire_catalog.go                       #   catalog 上下文组装
│   │   ├── wire_card.go                          #   card 上下文组装（注入 stripe.Client）
│   │   ├── wire_acquiring.go                     #   acquiring 上下文组装（商户+支付）
│   │   ├── wire_coupon.go                        #   coupon 上下文组装
│   │   └── wire_order.go                         #   order 上下文组装
│   │
│   ├── identity/                                 # ─── Identity Context ───
│   │   ├── domain/model/                         #   User, errors
│   │   ├── domain/port/                          #   UserRepository
│   │   ├── application/                          #   AuthUseCase
│   │   ├── adapter/persistence/                  #   InMemoryUserRepository
│   │   └── handler/middleware/                   #   AuthMiddleware（Token → userID 注入 ctx）
│   │
│   ├── catalog/                                  # ─── Catalog Context ───
│   │   ├── domain/model/                         #   Product, errors
│   │   ├── domain/port/                          #   ProductRepository
│   │   ├── application/                          #   CatalogUseCase
│   │   ├── adapter/persistence/                  #   InMemoryProductRepository
│   │   └── handler/http/                         #   CatalogHandler
│   │
│   ├── card/                                     # ─── Card Context ───
│   │   ├── domain/model/                         #   SavedCard, VaultToken, CardMask, errors
│   │   ├── domain/port/                          #   CardRepository, CardVault
│   │   ├── domain/event/                         #   CardBound, CardSuspended, CardDeleted...
│   │   ├── application/                          #   CardUseCase
│   │   ├── adapter/persistence/                  #   InMemoryCardRepository
│   │   ├── adapter/vault/                        #   StripeVaultAdapter（ACL: stripe.Client）
│   │   └── handler/http/                         #   CardHandler
│   │
│   ├── acquiring/                                # ─── Acquiring Context（商户管理+按订单金额扣款）───
│   │   ├── domain/
│   │   │   ├── model/                            #   Merchant, ChannelCredential, PaymentTransaction,
│   │   │   │                                     #   CardToken, PayPalToken, Money(alias), errors
│   │   │   ├── port/                             #   MerchantRepository, PaymentGateway, PayPalGateway,
│   │   │   │                                     #   GatewayFactory, TransactionRepository, CardQuery
│   │   │   └── event/                            #   MerchantRegistered, PaymentAuthorized, PaymentCaptured...
│   │   ├── application/                          #   MerchantUseCase, ChargeUseCase（Card + PayPal 购买/扣款/退款）
│   │   ├── adapter/
│   │   │   ├── gateway/                          #   MultiChannelGatewayFactory
│   │   │   │   ├── stripe/                       #   Stripe HTTP Client + GatewayAdapter（ACL）
│   │   │   │   └── paypal/                       #   PayPal HTTP Client + GatewayAdapter（ACL）
│   │   │   ├── persistence/                      #   InMemoryMerchantRepository, InMemoryTransactionRepository
│   │   │   └── card/                             #   CardAdapter（ACL: card → acquiring port）
│   │   └── handler/http/                         #   MerchantHandler, PaymentHandler
│   │
│   ├── coupon/                                   # ─── Coupon Context ───
│   │   ├── domain/model/                         #   Coupon, DiscountRule, errors
│   │   ├── domain/port/                          #   CouponRepository
│   │   ├── domain/event/                         #   CouponApplied
│   │   ├── application/                          #   CouponUseCase
│   │   ├── adapter/inmem/                        #   InMemoryCouponRepository
│   │   └── handler/http/                         #   CouponHandler
│   │
│   ├── order/                                    # ─── Order Context ───
│   │   ├── domain/
│   │   │   ├── model/                            #   Order, PriceBreakdown, errors
│   │   │   ├── port/                             #   OrderRepository, CatalogQuery, CouponApplier,
│   │   │   │                                     #   TaxRateQuery, PaymentCommand
│   │   │   ├── event/                            #   OrderCreated, OrderPaid, OrderRefunded
│   │   │   └── service/                          #   CalculateFinalAmount（纯函数：原价-折扣+税）
│   │   ├── application/                          #   OrderUseCase（定价编排 + 调 Acquiring）
│   │   ├── adapter/
│   │   │   ├── persistence/                      #   InMemoryOrderRepository
│   │   │   ├── catalog/                          #   CatalogAdapter（ACL: catalog → order port）
│   │   │   ├── coupon/                           #   CouponAdapter（ACL: coupon → order port）
│   │   │   ├── tax/                              #   StaticTaxQuery
│   │   │   └── acquiring/                        #   AcquiringAdapter（ACL: acquiring → order port）
│   │   └── handler/http/                         #   OrderHandler（POST /orders, /orders/{id}/capture...）
│   │
│   ├── shared/                                   # ─── Shared Kernel ───
│   │   ├── money/                                #   Money 值对象（int64 + Currency）
│   │   ├── event/                                #   DomainEvent 标记接口
│   │   └── auth/                                 #   WithUserID / UserIDFromContext
│   │
│   └── config/                                    #   环境变量加载（原 infra/config，简化后提升一层）
```

> **参考架构**扩展方向：生产环境增加 `infra/database/`（DB 连接池）、`infra/cache/`（Redis）、
> `pkg/outbox/`（Outbox 模式）、`internal/fulfillment/`、`internal/order/` 等。
> 所有内存仓储替换为 PostgreSQL 实现，`NewMockClient` 替换为 `NewClient`。

#### 各层职责

每个上下文内部的分层职责：

| 层 | 包含 | 职责 | 依赖谁 |
|---|---|---|---|
| **domain/model** | 聚合根、实体、值对象、领域错误 | 纯数据 + 状态机 + 业务规则 | 无外部依赖 |
| **domain/port** | 端口接口 + 接口绑定的 DTO | 定义"核心需要什么能力" | domain/model |
| **domain/event** | 领域事件 struct | 表示"发生了什么" | 无 |
| **domain/service** | 领域服务（纯函数） | 跨聚合的纯业务规则 | domain/model |
| **application/** | 用例编排 + 用例入参 DTO | 串联 domain + port，管理流程 | domain/* |
| **adapter/*** | 被驱动适配器 | 实现 port 接口（ACL 翻译） | domain/port, 可跨上下文 |
| **handler/http** | 驱动适配器 + HTTP 请求/响应 DTO | HTTP 解析 → DTO → usecase 调用 | application/ |
| **handler/middleware** | 中间件 | 认证、商城校验等横切关注点 | domain/port（可选） |
| **config/** | 应用配置 | 环境变量加载 | 标准库 |
| **bootstrap/** | Composition Root | 组装所有上下文（wire 函数） | 所有包 |
| **main.go** | 入口 | 加载配置 → 调用 bootstrap.New → 启动 HTTP | bootstrap, config |

#### struct 放哪里

不是所有 struct 都放 `model/`，按职责分布在对应的包中：

| 包 | 放什么 |
|---|---|
| `domain/model/` | 聚合根、实体、值对象、领域错误 |
| `domain/port/` | 端口接口 + 脱离接口无独立意义的 DTO |
| `domain/event/` | 领域事件 |
| `application/` | 用例入参 DTO |
| `handler/http/` | HTTP 请求/响应 DTO |

handler 和 application 中可以有同名 struct，handler 的带 JSON tag 负责序列化，application 的是纯领域入参，handler 负责转换。

#### 跨上下文规则

跨上下文的 import 只允许出现在 `adapter/` 层和 `bootstrap/`（Composition Root）。`domain/` 和 `application/` 永远只依赖自己上下文的包。

#### 缓存的位置（装饰器模式）

Redis 缓存不改变端口定义，用装饰器在适配器层解决：

```
domain/port/catalog.go             ← CatalogQuery 接口不变
adapter/query/cached_catalog.go    ← 装饰器：Redis → miss → inner(DB) → 回写 Redis
infra/cache/redis.go               ← Redis 连接池（共享基础设施）
```

#### Middleware 的位置

```
纯 HTTP 关注点（CORS、日志、超时）   → handler/middleware，无依赖
需要查数据的校验（商城是否存在）       → handler/middleware，依赖 port 接口
具体业务规则（VIP 才能买）            → 不放 middleware，放 application/
```

### 10.2 中间件链

`bootstrap/`（Composition Root）创建适配器并注入，请求经过的完整链路：

```
请求 → Auth Middleware → Store Middleware → HTTP Handler → UseCase → Domain
         │                   │                  │             │         │
    验证 Token         验证商城存在          DTO 转换      编排流程    业务规则
    userID → ctx       storeID → ctx                      调端口     状态机
```

完整示例见 `main.go` + `internal/bootstrap/app.go`。

### 10.3 从单体到微服务

由于每个上下文内部已经通过端口与适配器解耦，拆分时**领域层和用例层代码不需要修改**，只需替换适配器：

```
阶段 1: 模块化单体 → 内存事件总线，单一 main.go 组装所有上下文
阶段 2: 支付率先拆出 → Kafka 事件替代内存事件，跨服务 HTTP/gRPC 调用
阶段 3: 完全微服务 → 独立进程、独立 DB、API 网关
```

---

## 11. Go 主流架构对比

### 11.1 四种常见架构

| 架构 | 结构 | 适用场景 |
|---|---|---|
| **Flat（扁平）** | 所有 `.go` 文件在一个包里 | 小工具、CLI、库 |
| **按功能分包** | `user/`, `order/`, `payment/` 每包含完整 handler+service+repo | 中小项目，CRUD 为主 |
| **golang-standards/project-layout** | `cmd/`, `internal/`, `pkg/` | 社区约定（非官方），偏大型项目骨架 |
| **DDD + 六边形（本文）** | `domain/`, `application/`, `adapter/`, `handler/` | 业务复杂，需要隔离外部依赖 |

### 11.2 结构对比

```
Flat:                         按功能分包:

main.go                       payment/
handler.go                      handler.go
service.go                      service.go
repo.go                         repo.go
model.go                        model.go
                               order/
                                 handler.go
                                 service.go
                                 ...
```

DDD + 六边形结构见 [10.1 节](#101-项目结构模块化单体)。

### 11.3 适用场景决策

```
你的项目是什么类型？

├── 单一功能的小工具/CLI
│   └── 用 Flat，别犹豫
│
├── 标准 CRUD 业务（博客、CMS、管理后台）
│   └── 用「按功能分包」，每个功能自包含
│
├── 业务复杂 + 对接多个外部系统（支付、游戏服务器、IAP）
│   └── 用「DDD + 六边形」→ 10.1 模块化单体结构
│
└── 不确定？
    └── 从「按功能分包」开始，等痛点出现再演进到六边形
```

### 11.4 Go 社区共识（非官方标准，但大家都这么做）

| 共识 | 说明 |
|---|---|
| `internal/` 放不对外暴露的代码 | Go 编译器强制执行 |
| 包名用单数名词 | `model` 不是 `models`，`adapter` 不是 `adapters` |
| 不用 `utils`/`common`/`helpers` | 按职责命名，不按"杂项"命名 |
| 接口由消费方定义 | port 在 domain 中定义，不在 adapter 中 |
| `main.go` 做依赖注入 | 不用 DI 框架（wire/dig 可选），手动注入最透明 |
| 避免包循环依赖 | 依赖方向单向向内 |
| 不过度嵌套（3 层以内） | `internal/domain/model/` 最深 3 层 |

---

## 12. 测试策略

```
单元测试 (最多、最快、零依赖)
├── Transaction.Capture() 状态机规则
├── Money.ApplyDiscount() 精度
├── Delivery.Fail() → 重试策略
├── PricingService.CalculateFinalPrice()
└── 无 mock、无 DB、无网络

集成测试 (Mock 端口)
├── ChargeService + MockPaymentGateway
├── FulfillmentService + MockGameGateway
└── 验证用例编排逻辑

端到端测试 (最少、最慢)
├── HTTP → Service → Testcontainers(PostgreSQL)
└── 验证完整请求链路
```

```go
// 领域层测试 — 纯逻辑，零依赖
func TestDelivery_FailWithRetry(t *testing.T) {
    d := NewDelivery("ord_1", recipient, items)
    d.Attempt()
    d.Fail("timeout", time.Now())

    assert.Equal(t, Pending, d.Status)        // 还能重试
    assert.NotNil(t, d.NextRetryAt)
    assert.Empty(t, d.Events())               // 不发事件

    for i := 1; i < 5; i++ { d.Attempt(); d.Fail("timeout", time.Now()) }

    assert.Equal(t, Failed, d.Status)         // 耗尽重试
    assert.IsType(t, DeliveryFailed{}, d.Events()[0]) // 发失败事件
}

// 应用层测试 — Mock 端口
func TestChargeService_Success(t *testing.T) {
    svc := NewChargeService(mockMethodRepo, mockTxnRepo, mockGateway, mockEvents)
    result, err := svc.Charge(ChargeCommand{...})

    assert.NoError(t, err)
    assert.Equal(t, StatusCaptured, result.Status)
    assert.IsType(t, PaymentCaptured{}, mockEvents.Published[0])
}
```

---

## 13. 常见误区

### DDD 误区

| 误区 | 正确做法 |
|------|----------|
| **贫血模型** — 实体只有 getter/setter | 行为放在聚合里 (`txn.Capture()`)，Service 只做编排 |
| **大聚合** — 所有关联塞一起 | 聚合尽量小，跨聚合用 ID + 事件 |
| **到处用 DDD** | 只对核心域用，支撑域简单 CRUD 即可 |
| **上下文 = 微服务** | 上下文是逻辑边界，先模块化单体 |

### 六边形误区

| 误区 | 正确做法 |
|------|----------|
| **过度抽象** — 每个函数都定义接口 | 只为有多个实现或需要 Mock 的依赖定义端口 |
| **DTO 爆炸** | 领域对象在应用层内部可直接使用 |
| **忽视领域层** — 端口做得好但领域贫血 | 六边形保护的核心是领域逻辑 |

### 支付领域特有

| 要点 | 说明 |
|------|------|
| **金额用值对象** | `Money` 封装 Amount + Currency，杜绝裸 float |
| **幂等性是生命线** | 下单、支付、发货每个写操作都必须幂等 |
| **事件不能丢** | Outbox 模式保证事件与 DB 写入的原子性 |
| **防腐层必须有** | Stripe/游戏服务器的模型不要泄漏到领域层 |
| **对账是最后防线** | 定时对比：支付流水 vs 订单 vs 发货记录 |
| **状态机要严格** | 禁止非法跳转 (如 Settled 直接到 Authorized) |
