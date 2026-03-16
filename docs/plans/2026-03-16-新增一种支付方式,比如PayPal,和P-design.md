# 技术设计文档

- **需求**: 新增一种支付方式,比如PayPal,和PayPal交互部分mock即可,不需要真实实现
- **项目**: payment-demo
- **日期**: 2026-03-16
- **状态**: 待确认

---

## 一、领域设计

我先全面了解项目现有结构和代码风格。现在我已经完整了解了整个项目结构和风格，可以输出设计文档了。

---

# PayPal 支付方式 — 领域设计文档

## 1. 统一语言术语表

| 术语 | 含义 | 类型 |
|------|------|------|
| PayPalOrder | PayPal 侧的支付订单，对应 PayPal API 中的 Order 对象 | 值对象（DTO，在 port 层） |
| PayPalToken | 前端 PayPal JS SDK 返回的一次性授权令牌（`EC-XXXX`、`BA-XXXX` 等） | 值对象 |
| PaymentMethod | 支付方式枚举（Card / PayPal），区分同一笔交易走哪条 Gateway 路径 | 值对象（枚举） |
| PayPalGateway | 对 PayPal REST API 的防腐层适配器，实现 `port.PaymentGateway` | 适配器（Adapter） |
| GatewayAuthResult | 网关授权结果，已有，复用 | 值对象（port DTO） |
| PaymentAuthorized / PaymentCaptured / PaymentRefunded | 已有领域事件，复用 | 事件 |

> **关键设计决策**：PayPal 和 Card 支付**共用 `port.PaymentGateway` 接口**。`PaymentTransaction` 新增 `Method` 字段区分支付方式，`ChargeUseCase` 通过 **策略选择器** 按 `Method` 分派到不同 Gateway 实现，从而无需修改核心聚合根状态机。

---

## 2. 上下文归属

**归属：`payment` 现有上下文**。理由如下：

- PayPal 是另一种**支付网关实现**，与现有 Card 流程共享同一聚合根 `PaymentTransaction`、同一状态机（Created → Authorized → Captured → Refunded）、同一 `TransactionRepository`。
- 不需要新增上下文：核心领域概念（交易、授权、扣款、退款）完全不变，只是新增一个 Gateway Adapter 和少量路由扩展。

### 跨上下文交互

| 交互方向 | 方式 | 说明 |
|----------|------|------|
| PayPal 外部系统 → payment | ACL（Mock Adapter） | `payment/adapter/gateway/paypal_gateway.go` 实现 `port.PaymentGateway`，隔离外部 API 细节 |
| payment → catalog | 已有 `port.CatalogQuery`，**不变** | PayPal 购买同样需要查商品价格 |
| payment → card | **不适用** | PayPal 支付不需要 CardToken，CardQuery 不调用 |

---

## 3. 共享类型复用分析

### 复用清单（已有，直接使用）

| 类型 | 所在位置 | 说明 |
|------|----------|------|
| `shared/event.DomainEvent` | `internal/shared/event/event.go` | 全局领域事件接口，已统一 ✅ |
| `payment/domain/event.DomainEvent` (alias) | `internal/payment/domain/event/event.go` | 本上下文别名，直接使用 ✅ |
| `event.PaymentAuthorized/Captured/Refunded` | `internal/payment/domain/event/event.go` | PayPal 流程触发相同事件 ✅ |
| `model.Money` | `internal/payment/domain/model/money.go` | 金额值对象，不变 ✅ |
| `model.PaymentTransaction` | `internal/payment/domain/model/transaction.go` | 聚合根，**扩展** `Method` 字段 ✅ |
| `port.PaymentGateway` | `internal/payment/domain/port/gateway.go` | PayPal Gateway 实现此接口 ✅ |
| `port.GatewayAuthResult` | `internal/payment/domain/port/gateway.go` | 授权结果 DTO，复用 ✅ |
| `port.TransactionRepository` | `internal/payment/domain/port/repository.go` | 仓储不变 ✅ |
| `model.ErrInvalidStateTransition` / `ErrAuthorizationDeclined` 等 | `internal/payment/domain/model/errors.go` | **扩展**新增 PayPal 专属错误 ✅ |

### 新增清单（需要新定义）

| 类型 | 位置 | 说明 |
|------|------|------|
| `model.PaymentMethod` (枚举) | `payment/domain/model/payment_method.go` | 区分 Card / PayPal |
| `model.PayPalToken` (值对象) | `payment/domain/model/paypal_token.go` | 承载 PayPal 前端令牌，类比 `CardToken` |
| `port.PayPalGateway` (接口) | `payment/domain/port/paypal_gateway.go` | **独立接口**，见第 5 节分析 |
| `application.PayPalPurchaseRequest` | `payment/application/charge_usecase.go` | 新增 PayPal 入参 |
| `adapter/gateway/paypal_gateway.go` | `payment/adapter/gateway/` | Mock 实现 |
| `model.ErrPayPalTokenInvalid` | `payment/domain/model/errors.go` 扩展 | PayPal token 无效 |

> **关于 `port.PayPalGateway` vs 复用 `port.PaymentGateway`**：
> PayPal 的授权入参是 `PayPalToken`（不是 `CardToken`），若强行复用 `PaymentGateway.Authorize(CardToken, Money)` 需要滥用 CardToken 字段，违反语义。因此**新增 `port.PayPalGateway` 接口**，UseCase 通过组合两个 Gateway 端口分派，保持接口语义清晰。

---

## 4. 聚合根设计（Go 伪代码）

### 4.1 新增值对象

```go
// payment/domain/model/payment_method.go

package model

// PaymentMethod 支付方式枚举
type PaymentMethod string

const (
    PaymentMethodCard   PaymentMethod = "CARD"
    PaymentMethodPayPal PaymentMethod = "PAYPAL"
)
```

```go
// payment/domain/model/paypal_token.go

package model

// PayPalToken PayPal 前端 JS SDK 返回的一次性授权令牌
// 类比 CardToken，由前端 tokenization 生成
type PayPalToken struct {
    OrderID string // PayPal Order ID，如 "5O190127TN364715T"
    PayerID string // PayPal Payer ID，如 "FSMVU44LF3YUS"
}
```

### 4.2 聚合根扩展

> **原则**：最小侵入，只新增字段，不改状态机。

```go
// payment/domain/model/transaction.go（差量修改，非全量）

// PaymentTransaction 支付交易聚合根（扩展 PayPal 支持）
type PaymentTransaction struct {
    ID          TransactionID
    UserID      string
    ProductID   string
    Amount      Money
    Method      PaymentMethod  // ← 新增：CARD / PAYPAL
    CardToken   CardToken      // Method==CARD 时有效
    PayPalToken PayPalToken    // Method==PAYPAL 时有效
    Status      TransactionStatus
    ProviderRef string
    AuthCode    string
    FailReason  string
    // ... 时间字段不变 ...
    Events      []event.DomainEvent
}

// NewPayPalTransaction 工厂方法：创建 PayPal 支付交易
// 对应已有的 NewPaymentTransaction（Card 流程）
func NewPayPalTransaction(userID, productID string, amount Money, token PayPalToken) *PaymentTransaction {
    return &PaymentTransaction{
        ID:          NewTransactionID(),
        UserID:      userID,
        ProductID:   productID,
        Amount:      amount,
        Method:      PaymentMethodPayPal,
        PayPalToken: token,
        Status:      StatusCreated,
        CreatedAt:   time.Now(),
    }
}

// 状态转换方法完全复用：MarkAuthorized / MarkCaptured / MarkRefunded / MarkFailed
// PayPal 流程触发完全相同的领域事件，无需修改
```

### 4.3 错误扩展

```go
// payment/domain/model/errors.go（新增行）

var (
    // 已有错误不变 ...

    // PayPal 专属
    ErrPayPalTokenInvalid   = errors.New("paypal token is invalid or expired")
    ErrPayPalOrderMismatch  = errors.New("paypal order amount mismatch")
)
```

---

## 5. 端口接口（Go 伪代码）

### 5.1 新增：PayPal Gateway 端口

```go
// payment/domain/port/paypal_gateway.go

package port

import (
    "context"
    "payment-demo/internal/payment/domain/model"
)

// PayPalGateway PayPal 支付网关端口（被驱动端口）
// 消费方（payment domain）定义，adapter 层实现
// 与 PaymentGateway 独立，因为入参类型不同（PayPalToken vs CardToken）
type PayPalGateway interface {
    // Authorize 验证 PayPal Order 并完成授权
    // token: 前端 JS SDK 返回的 OrderID + PayerID
    // amount: 用于校验 PayPal Order 金额是否匹配（防篡改）
    Authorize(ctx context.Context, token model.PayPalToken, amount model.Money) (*PayPalAuthResult, error)

    // Capture 从已授权的 PayPal Order 扣款
    Capture(ctx context.Context, providerRef string, amount model.Money) error

    // Refund 对已扣款的 PayPal Order 退款
    Refund(ctx context.Context, providerRef string, amount model.Money) error
}

// PayPalAuthResult PayPal 授权结果 DTO
type PayPalAuthResult struct {
    ProviderRef string // PayPal Capture ID，如 "2GG279541U471931P"
    PayerEmail  string // 付款方 PayPal 邮箱，可选，用于记录
}
```

> **为什么 Capture/Refund 签名与 `PaymentGateway` 一致？**
> Capture 和 Refund 只需要 `providerRef`（ProviderRef 由 Authorize 返回），入参完全相同，语义一致，所以接口方法签名保持对齐，降低 UseCase 的认知负担。

### 5.2 已有端口（不变）

```go
// port.TransactionRepository — 不变，PayPal 交易同样通过此接口持久化
// port.CatalogQuery          — 不变，PayPal 购买同样查商品价格
// port.CardQuery             — 不变，PayPal 流程不调用
// port.PaymentGateway        — 不变，Card 流程继续使用
```

### 5.3 Mock 说明

```go
// payment/adapter/gateway/paypal_gateway.go
// ⚠️ 编译期接口检查放在实现文件中，Mock 测试辅助放在 _test.go

// MockPayPalGateway 模拟 PayPal 外部网关（ACL 边界）
// tok_paypal_decline 前缀 → 返回 ErrPayPalTokenInvalid（类比 Card 的 tok_decline）
type MockPayPalGateway struct{}

var _ port.PayPalGateway = (*MockPayPalGateway)(nil)

func (g *MockPayPalGateway) Authorize(_ context.Context, token model.PayPalToken, amount model.Money) (*port.PayPalAuthResult, error) {
    log.Printf("[MockPayPalGateway] Authorize: orderID=%s, payerID=%s, amount=%s",
        token.OrderID, token.PayerID, amount.String())

    if strings.HasPrefix(token.OrderID, "EC-DECLINE") {
        return nil, model.ErrPayPalTokenInvalid
    }

    return &port.PayPalAuthResult{
        ProviderRef: fmt.Sprintf("CAPTURE-%d", rand.Int63()),
        PayerEmail:  "buyer@example.com",
    }, nil
}

func (g *MockPayPalGateway) Capture(_ context.Context, providerRef string, amount model.Money) error {
    log.Printf("[MockPayPalGateway] Capture: ref=%s, amount=%s", providerRef, amount.String())
    return nil
}

func (g *MockPayPalGateway) Refund(_ context.Context, providerRef string, amount model.Money) error {
    log.Printf("[MockPayPalGateway] Refund: ref=%s, amount=%s", providerRef, amount.String())
    return nil
}
```

```go
// payment/application/paypal_usecase_test.go（_test.go 中的 stub）

// stubPayPalGateway 用于单元测试，替代 MockPayPalGateway
type stubPayPalGateway struct {
    authorizeResult *port.PayPalAuthResult
    authorizeErr    error
    authorizeCalled bool
    authorizedWith  *model.PayPalToken
}

func (g *stubPayPalGateway) Authorize(_ context.Context, token model.PayPalToken, _ model.Money) (*port.PayPalAuthResult, error) {
    g.authorizeCalled = true
    g.authorizedWith = &token
    return g.authorizeResult, g.authorizeErr
}

func (g *stubPayPalGateway) Capture(_ context.Context, _ string, _ model.Money) error { return nil }
func (g *stubPayPalGateway) Refund(_ context.Context, _ string, _ model.Money) error  { return nil }
```

---

## 6. 应用层扩展（UseCase）

### 6.1 新增入参 & 方法

```go
// payment/application/charge_usecase.go（差量修改）

// ChargeUseCase 扩展 paypalGateway 字段
type ChargeUseCase struct {
    gateway       port.PaymentGateway    // Card 网关（已有）
    paypalGateway port.PayPalGateway     // ← 新增：PayPal 网关
    repo          port.TransactionRepository
    catalog       port.CatalogQuery
    cardQuery     port.CardQuery
}

func NewChargeUseCase(
    gateway port.PaymentGateway,
    paypalGateway port.PayPalGateway,   // ← 新增参数
    repo port.TransactionRepository,
    catalog port.CatalogQuery,
    cardQuery port.CardQuery,
) *ChargeUseCase { ... }

// PayPalPurchaseRequest PayPal 购买用例入参
type PayPalPurchaseRequest struct {
    UserID    string
    ProductID string
    Token     model.PayPalToken // 前端 JS SDK 返回的 OrderID + PayerID
}

// PayPalPurchase PayPal 购买用例：查商品 → 验证 PayPal Token → 授权 → 持久化
// Capture / Refund 复用已有方法（ProviderRef 统一路由到对应 Gateway）
func (uc *ChargeUseCase) PayPalPurchase(ctx context.Context, req PayPalPurchaseRequest) (*model.PaymentTransaction, error) {
    // 1. 查商品（复用 CatalogQuery，不变）
    product, err := uc.catalog.FindProduct(ctx, req.ProductID)
    if err != nil {
        return nil, err
    }
    if !product.IsActive {
        return nil, model.ErrProductNotActive
    }

    amount := model.NewMoney(product.Amount, product.Currency)

    // 2. 创建 PayPal 交易（新工厂方法，Method=PAYPAL）
    txn := model.NewPayPalTransaction(req.UserID, req.ProductID, amount, req.Token)

    // 3. 调 PayPal 网关授权（ACL 边界：Mock 隔离外部 API）
    result, err := uc.paypalGateway.Authorize(ctx, req.Token, amount)
    if err != nil {
        txn.MarkFailed(err.Error())
        _ = uc.repo.Save(ctx, txn)
        return txn, model.ErrAuthorizationDeclined
    }

    // 4. 复用已有状态转换（MarkAuthorized 触发 PaymentAuthorized 事件）
    if err := txn.MarkAuthorized(result.ProviderRef, ""); err != nil {
        return nil, err
    }
    if err := uc.repo.Save(ctx, txn); err != nil {
        return nil, err
    }

    uc.publishEvents(txn)
    return txn, nil
}
```

> **Capture / Refund 如何区分 Card vs PayPal？**
> 通过 `txn.Method` 分派：

```go
// Capture 内部分派逻辑（差量，替换原 gateway.Capture 调用）
func (uc *ChargeUseCase) Capture(ctx context.Context, txnID model.TransactionID) (*model.PaymentTransaction, error) {
    txn, err := uc.repo.FindByID(ctx, txnID)
    // ... 校验 ...

    // 按支付方式路由到对应 Gateway
    switch txn.Method {
    case model.PaymentMethodPayPal:
        err = uc.paypalGateway.Capture(ctx, txn.ProviderRef, txn.Amount)
    default: // CARD
        err = uc.gateway.Capture(ctx, txn.ProviderRef, txn.Amount)
    }
    if err != nil {
        return nil, model.ErrCaptureFailure
    }
    // ... MarkCaptured, Save, publishEvents ...
}
// Refund 同理
```

---

## 7. Handler 扩展（Go 伪代码）

```go
// payment/handler/http/payment_handler.go（新增路由）

// PayPalPurchaseRequest HTTP 请求体
type PayPalPurchaseRequest struct {
    ProductID string `json:"product_id"`
    OrderID   string `json:"order_id"` // PayPal JS SDK 返回
    PayerID   string `json:"payer_id"` // PayPal JS SDK 返回
}

func (h *PaymentHandler) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/charge", h.handleCharge)              // 已有（Card）
    mux.HandleFunc("/charge/paypal", h.handlePayPalCharge) // ← 新增
    mux.HandleFunc("/capture/", h.handleCapture)           // 已有，自动分派
    mux.HandleFunc("/refund/", h.handleRefund)             // 已有，自动分派
    mux.HandleFunc("/transaction/", h.handleGetTransaction)
}

func (h *PaymentHandler) handlePayPalCharge(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var req PayPalPurchaseRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonError(w, "invalid request body", http.StatusBadRequest)
        return
    }
    if req.OrderID == "" || req.PayerID == "" {
        jsonError(w, "order_id and payer_id are required", http.StatusBadRequest)
        return
    }

    userID, ok := middleware.UserIDFromContext(r.Context())
    if !ok {
        jsonError(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    txn, err := h.useCase.PayPalPurchase(r.Context(), application.PayPalPurchaseRequest{
        UserID:    userID,
        ProductID: req.ProductID,
        Token:     model.PayPalToken{OrderID: req.OrderID, PayerID: req.PayerID},
    })
    if err != nil {
        jsonError(w, err.Error(), mapErrorStatus(err))
        return
    }
    jsonOK(w, toResponse(txn))
}

// mapErrorStatus 扩展（新增 PayPal 错误映射）
func mapErrorStatus(err error) int {
    switch err {
    // 已有映射不变 ...
    case model.ErrPayPalTokenInvalid, model.ErrPayPalOrderMismatch:
        return http.StatusUnprocessableEntity // 与 ErrAuthorizationDeclined 对齐
    default:
        return http.StatusInternalServerError
    }
}
```

---

## 8. 领域事件

| 事件 | 触发时机 | Payload | 复用/新增 |
|------|----------|---------|-----------|
| `PaymentAuthorized` | PayPal Authorize 成功 | TransactionID, Amount, Currency, OccurredAt | **复用** ✅ |
| `PaymentCaptured` | PayPal Capture 成功 | TransactionID, Amount, Currency, OccurredAt | **复用** ✅ |
| `PaymentRefunded` | PayPal Refund 成功 | TransactionID, Amount, Currency, OccurredAt | **复用** ✅ |

> **结论**：PayPal 流程触发的领域事件与 Card 完全一致（`MarkAuthorized/Captured/Refunded` 复用），无需新增事件类型。下游消费者如需区分支付方式，可从 `TransactionID` 查询 `txn.Method`。

---

## 9. 完整文件变更清单

```
payment/
├── domain/
│   ├── model/
│   │   ├── payment_method.go     ← 新增（PaymentMethod 枚举）
│   │   ├── paypal_token.go       ← 新增（PayPalToken 值对象）
│   │   ├── transaction.go        ← 修改（新增 Method/PayPalToken 字段 + NewPayPalTransaction 工厂）
│   │   └── errors.go             ← 修改（新增 ErrPayPalTokenInvalid / ErrPayPalOrderMismatch）
│   └── port/
│       └── paypal_gateway.go     ← 新增（PayPalGateway 接口 + PayPalAuthResult DTO）
├── application/
│   └── charge_usecase.go         ← 修改（新增 paypalGateway 字段、PayPalPurchase 方法、Capture/Refund 分派）
├── adapter/
│   └── gateway/
│       └── paypal_gateway.go     ← 新增（MockPayPalGateway 实现）
└── handler/
    └── http/
        └── payment_handler.go    ← 修改（新增 /charge/paypal 路由 + handlePayPalCharge）
```

---

## 10. 依赖检查

**无新增外部依赖**。

| 已有依赖 | 版本 | 用途 |
|----------|------|------|
| `github.com/google/uuid` | v1.6.0 | `NewTransactionID()` 复用，不变 |

> PayPal Mock 实现只使用标准库（`math/rand`、`fmt`、`log`、`strings`），与现有 `MockPaymentGateway` 一致，无需引入 PayPal SDK。