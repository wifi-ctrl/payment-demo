# Payment Demo — DDD + 六边形架构支付系统

一个完整的游戏支付商城 Demo，演示如何用 Go 落地 **DDD（领域驱动设计）** 和 **六边形架构（Ports & Adapters）**。

本文档既是项目说明，也是 DDD 实践指南 — 每个 DDD 概念都对应本项目中的真实代码。

> 理论详解见 [`docs/game-payment-store-guide.md`](../../docs/game-payment-store-guide.md)

---

## 目录

- [一、战略设计](#一战略设计)
  - [限界上下文](#限界上下文bounded-context)
  - [上下文映射](#上下文映射context-map)
  - [Shared Kernel](#shared-kernel)
- [二、战术设计](#二战术设计)
  - [聚合根](#聚合根aggregate-root)
  - [值对象](#值对象value-object)
  - [领域事件](#领域事件domain-event)
  - [领域服务](#领域服务domain-service)
  - [端口与适配器](#端口与适配器ports--adapters)
  - [用例编排层](#用例编排层application-service)
- [三、架构规则](#三架构规则)
  - [分层结构](#分层结构)
  - [依赖方向](#依赖方向)
  - [Composition Root](#composition-root)
- [四、关键设计模式](#四关键设计模式)
  - [ACL 防腐层](#acl-防腐层)
  - [Saga 补偿](#saga-补偿)
  - [工厂方法](#工厂方法)
  - [多商户路由](#多商户路由)
- [五、快速上手](#五快速上手)
- [六、API 接口](#六api-接口)
- [七、测试策略](#七测试策略)
- [八、扩展指南](#八扩展指南)

---

# 一、战略设计

战略设计回答一个问题：**系统如何拆分？上下文之间如何协作？**

## 限界上下文（Bounded Context）

限界上下文是一个**语义边界** — 边界内的术语、模型、规则具有唯一且一致的含义。

> 限界上下文不是微服务。它是**逻辑边界**，不是部署边界。本 Demo 的 6 个上下文运行在同一进程中。

```
internal/
├── identity/      用户认证 — Session 管理、AuthMiddleware、UserID 注入 ctx
├── catalog/       商品目录 — 商品 CRUD、上下架
├── card/          卡管理   — 绑卡（Vault tokenization）、挂起/激活/删除、默认卡切换
├── merchant/      商户管理 — 商户注册/暂停、渠道凭据（CARD/PAYPAL）添加/吊销
├── coupon/        优惠券   — 创建、核销（Apply/Rollback 补偿）、用尽/过期状态机
├── payment/       支付核心 — Card/PayPal 购买 → 授权 → 扣款 → 退款
└── shared/        Shared Kernel（见下文）
```

**为什么是 6 个而不是 1 个？** 每个上下文满足独立拆分的三个标准：

| 标准 | 示例 |
|---|---|
| **独立生命周期** | 优惠券策略变更时，卡管理代码不需要改 |
| **独立不变式** | merchant 维护"同渠道最多一个 ACTIVE 凭据"，payment 维护"CREATED → AUTHORIZED → CAPTURED"状态机 |
| **独立变更频率** | catalog 稳定，coupon 频繁迭代 |

**同一个词在不同上下文中含义不同：**

| 术语 | payment 上下文 | card 上下文 |
|---|---|---|
| Token | 一次性支付令牌（`CardToken`），授权后即废 | Vault 持久令牌（`VaultToken`），长期存储 |
| Status | 交易状态：CREATED/AUTHORIZED/CAPTURED/REFUNDED/FAILED | 卡状态：ACTIVE/SUSPENDED/DELETED |

这正是需要划分上下文的信号。

## 上下文映射（Context Map）

上下文之间如何协作？本 Demo 使用三种关系模式：

```
┌──────────┐  ACL   ┌──────────┐  ACL   ┌──────────┐
│ merchant │◀───────│          │───────▶│ catalog  │
└──────────┘        │          │        └──────────┘
                    │ payment  │
┌──────────┐  ACL   │ (下游/   │  ACL   ┌──────────┐
│   card   │◀───────│  消费方)  │───────▶│  coupon  │
└──────────┘        │          │        └──────────┘
                    └────┬─────┘
                         │ ACL
                         ▼
                  ┌──────────────┐
                  │ 外部网关      │
                  │ Stripe/PayPal│
                  └──────────────┘

┌──────────┐  Shared Kernel  ┌──────────┐
│  所有     │◀──────────────▶│ shared/  │
│  上下文   │  money, event,  │          │
│          │  auth           │          │
└──────────┘                 └──────────┘
```

| 关系模式 | 本项目实例 | DDD 含义 |
|---|---|---|
| **防腐层 (ACL)** | payment 通过 `adapter/merchant/` 访问 merchant 上下文 | 下游定义自己的接口，通过 adapter 翻译上游模型，防止上游变更污染自身领域 |
| **Shared Kernel** | `shared/money`、`shared/event`、`shared/auth` | 多个上下文共同拥有的一小部分模型，变更需所有消费方同意 |
| **消费方驱动契约** | payment 定义 `port.CatalogQuery`，而非 catalog 提供接口 | 接口由消费方按自身最小需求定义（接口隔离原则） |

## Shared Kernel

跨上下文共享的基础类型。所有上下文通过 `import "shared/..."` 使用：

| 包 | 内容 | 为什么放 Shared Kernel |
|---|---|---|
| `shared/money` | `Money` 值对象 — Amount（int64 最小货币单位）+ Currency（ISO 4217） | 金额是支付领域最核心的值对象，各上下文必须类型一致 |
| `shared/event` | `DomainEvent` 标记接口 | 领域事件的基础契约，所有聚合根的事件均实现此接口 |
| `shared/auth` | `WithUserID` / `UserIDFromContext` | 用户认证信息的 context 读写，identity 写入，其他 handler 读取 |

**Go 落地技巧 — type alias 消除类型分裂：**

```go
// payment/domain/model/money.go
// 不重新定义 struct，用 type alias 确保与 shared/money.Money 是同一类型
type Money = money.Money
```

这样 `model.Money` 和 `money.Money` 编译期完全等价，聚合根字段（如 `DiscountAmount *Money`）无需类型转换。

---

# 二、战术设计

战术设计回答一个问题：**单个上下文内部如何建模？**

## 聚合根（Aggregate Root）

聚合根是**事务一致性边界** — 所有状态变更必须通过聚合根方法，聚合根负责维护不变式。

### PaymentTransaction — 支付交易

```go
// internal/payment/domain/model/transaction.go

type PaymentTransaction struct {
    ID          TransactionID
    MerchantID  string
    UserID      string
    Amount      Money          // 值对象
    Method      PaymentMethod  // CARD / PAYPAL
    CardToken   CardToken      // 值对象
    Status      TransactionStatus
    ProviderRef string
    Events      []event.DomainEvent  // 未发布的领域事件
}

// 状态流转：CREATED → AUTHORIZED（不变式：只有 CREATED 才能 Authorize）
func (t *PaymentTransaction) MarkAuthorized(providerRef, authCode string) error {
    if t.Status != StatusCreated {          // 不变式守卫
        return ErrInvalidStateTransition
    }
    t.Status = StatusAuthorized
    t.ProviderRef = providerRef
    t.addEvent(event.PaymentAuthorized{...}) // 产生领域事件
    return nil
}
```

**状态机：**

```
  ┌─────────┐     Authorize      ┌────────────┐     Capture      ┌──────────┐
  │ CREATED │───────────────────▶│ AUTHORIZED │──────────────────▶│ CAPTURED │
  └────┬────┘                    └────────────┘                   └─────┬────┘
       │ (网关拒绝)                                                      │ Refund
       ▼                                                                ▼
  ┌─────────┐                                                    ┌──────────┐
  │ FAILED  │                                                    │ REFUNDED │
  └─────────┘                                                    └──────────┘
```

**关键设计点：**

- **状态变更只能通过聚合根方法**（`MarkAuthorized` / `MarkCaptured` / `MarkRefunded`），不允许外部直接修改 `Status` 字段
- **校验是聚合根职责**（如 `ValidateCapturable()`），不是 UseCase 或 Service 的职责
- **每次状态流转产生领域事件**，由 UseCase 调用 `ClearEvents()` 后统一发布

### SavedCard — 卡管理

```
  ┌────────┐ Suspend  ┌───────────┐
  │ ACTIVE │─────────▶│ SUSPENDED │
  └───┬────┘◀─────────└─────┬─────┘
      │      Activate       │
      │                     │
      │ Delete              │ Delete
      ▼                     ▼
  ┌─────────┐
  │ DELETED │  ← 终态，不可逆
  └─────────┘
```

不变式：
- 只有 ACTIVE 状态可设为默认卡（`SetDefault` 方法守卫）
- Delete 是终态，不可逆（`Delete` 方法守卫 `status == DELETED` 返回错误）
- UserID 归属校验由 UseCase 层在调用聚合根方法前完成

### Merchant — 商户管理

```
  ┌────────┐ Suspend  ┌───────────┐
  │ ACTIVE │─────────▶│ SUSPENDED │
  └────────┘          └───────────┘
```

不变式：**同一渠道最多只有一个 ACTIVE 凭据**（`AddCredential` 方法守卫）。

### Coupon — 优惠券

```
  ┌────────┐  Apply(用尽)  ┌───────────┐
  │ ACTIVE │─────────────▶│ EXHAUSTED │
  └───┬────┘              └───────────┘
      │ MarkExpired
      ▼
  ┌─────────┐
  │ EXPIRED │
  └─────────┘
```

`Apply` 和 `Rollback` 构成 **Saga 补偿对**（见下文）。

## 值对象（Value Object）

值对象**无唯一标识**，**按值判等**，**不可变**。

```go
// internal/shared/money/money.go

type Money struct {
    Amount   int64   // 最小货币单位（cents），杜绝 float 精度问题
    Currency string  // ISO 4217，如 "USD"
}

// 带币种校验的运算 — 不同币种运算直接 panic（调用方保证）
func (m Money) Add(other Money) Money { ... }
func (m Money) Subtract(other Money) (Money, error) { ... }
func (m Money) MultiplyBasisPoint(bp int64) (Money, error) { ... }

// 两个 999 USD 没区别 → 值对象
// 两笔同为 999 USD 的交易不能互换 → 实体
```

本项目的值对象：

| 值对象 | 上下文 | 特征 |
|---|---|---|
| `Money` | shared | Amount + Currency，封装货币运算 |
| `CardToken` | payment | TokenID + Last4 + Brand，一次性支付令牌 |
| `PayPalToken` | payment | OrderID + PayerID |
| `VaultToken` | card | Token + Provider，Vault 持久令牌 |
| `CardMask` | card | Last4 + Brand + ExpireMonth/Year，脱敏展示 |
| `CardHolder` | card | Name + BillingCountry |
| `DiscountRule` | coupon | Type(PERCENTAGE/FIXED) + Value |

**识别技巧：** 如果两个实例的所有字段值相同就可以互换 → 值对象；如果即使字段值相同也需要区分是"哪一个" → 实体。

## 领域事件（Domain Event）

聚合根状态流转时产生领域事件，记录"发生了什么"。

```go
// internal/shared/event/event.go — 所有事件的标记接口
type DomainEvent interface {
    EventName() string
}

// internal/payment/domain/event/event.go — 支付领域的具体事件
type PaymentAuthorized struct {
    TransactionID string
    Amount        int64
    Currency      string
    OccurredAt    time.Time
}
func (e PaymentAuthorized) EventName() string { return "PaymentAuthorized" }
```

**事件的生命周期：**

```
聚合根方法内产生 → 暂存在聚合根 Events 字段 → UseCase 调用 ClearEvents() → 发布（目前 log 输出）
```

```go
// 聚合根方法内
func (t *PaymentTransaction) MarkCaptured() error {
    // ...
    t.addEvent(event.PaymentCaptured{...})   // 1. 产生事件
    return nil
}

// UseCase 中
func (uc *ChargeUseCase) Capture(ctx context.Context, txnID TransactionID) {
    txn.MarkCaptured()           // 2. 触发状态流转
    uc.repo.Save(ctx, txn)       // 3. 持久化
    for _, evt := range txn.ClearEvents() {
        log.Printf("[DomainEvent] %s", evt.EventName())  // 4. 发布
    }
}
```

本项目的领域事件：

| 上下文 | 事件 | 触发时机 |
|---|---|---|
| payment | `PaymentAuthorized` / `PaymentCaptured` / `PaymentRefunded` | 交易状态流转 |
| card | `CardBound` / `CardSuspended` / `CardActivated` / `CardDeleted` / `DefaultCardChanged` | 卡状态变更 |
| merchant | `MerchantRegistered` / `MerchantSuspended` / `CredentialAdded` / `CredentialRevoked` | 商户操作 |
| coupon | `CouponApplied` | 优惠券核销 |

## 领域服务（Domain Service）

领域服务处理**不属于单个聚合根**的纯业务逻辑。判断标准：

| 放在哪 | 标准 | 示例 |
|---|---|---|
| **聚合根方法** | 单实体状态检查 / 状态流转 | `txn.MarkAuthorized()`、`txn.ValidateCapturable()`、`card.Suspend()` |
| **领域服务** | 纯计算、需多个值对象协作、不属于任何聚合根 | `CalculateFinalAmount(original, discount, tax)` |
| **UseCase** | 编排（调用多个端口和聚合根方法） | `Purchase()` 流程 |

```go
// internal/payment/domain/service/price_calculator.go
// 纯函数：原价 - 折扣 + 税 = 最终金额
// 不依赖任何外部服务，不修改任何状态 → 领域服务
func CalculateFinalAmount(
    original money.Money,
    discountType string, discountValue int64,
    taxBP int64,
) (finalAmount, discountAmount, taxAmount money.Money, err error) {
    // PERCENTAGE: original × discountValue / 10000
    // FIXED:      直接减去 discountValue
    // tax:        afterDiscount × taxBP / 10000
    // final:      afterDiscount + tax
}
```

**常见误区：把单实体校验放在领域服务里。** 比如"校验交易是否可扣款"只需检查 `txn.Status == AUTHORIZED`，这是聚合根自己的职责，不需要领域服务。

## 端口与适配器（Ports & Adapters）

**端口**是领域层定义的接口（"我需要什么能力"），**适配器**是外层对接口的实现（"我如何提供这个能力"）。

```go
// 端口：在 domain/port/ 中定义 — 领域层说"我需要能保存交易"
type TransactionRepository interface {
    Save(ctx context.Context, txn *model.PaymentTransaction) error
    FindByID(ctx context.Context, id model.TransactionID) (*model.PaymentTransaction, error)
}

// 适配器：在 adapter/persistence/ 中实现 — "我用内存 map 来实现"
type InMemoryTransactionRepository struct {
    data map[model.TransactionID]*model.PaymentTransaction
}
func (r *InMemoryTransactionRepository) Save(...) error { ... }
func (r *InMemoryTransactionRepository) FindByID(...) (*model.PaymentTransaction, error) { ... }
```

本项目的端口分为两类：

| 类型 | 端口 | 适配器 | 说明 |
|---|---|---|---|
| **驱动端口** | HTTP handler 就是驱动适配器本身 | `handler/http/` | 外部请求驱动系统 |
| **被驱动端口** | `port.TransactionRepository` | `adapter/persistence/memory_repo.go` | 系统驱动外部存储 |
| | `port.GatewayFactory` | `adapter/gateway/stripe_gateway_factory.go` | 多渠道网关工厂（Card → Stripe, PayPal → PayPal） |
| | `port.PaymentGateway` | `adapter/gateway/stripe_gateway.go` | Card 支付网关（Stripe PaymentIntents API） |
| | `port.PayPalGateway` | `adapter/gateway/paypal_gateway_adapter.go` | PayPal 支付网关（PayPal Orders API） |
| | `port.TaxRateQuery` | `adapter/tax/static_tax_query.go` | 税率查询（演示用静态配置） |
| | `port.CatalogQuery` | `adapter/catalog/catalog_adapter.go` | 跨上下文 ACL |
| | `port.MerchantQuery` | `adapter/merchant/merchant_adapter.go` | 跨上下文 ACL |
| | `port.CardQuery` | `adapter/card/card_adapter.go` | 跨上下文 ACL |
| | `port.CouponApplier` | `adapter/coupon/coupon_adapter.go` | 跨上下文 ACL |
| | `port.CardVault` | `adapter/vault/stripe_vault.go` | 外部 Vault 服务（Stripe Tokens API） |

**关键原则：端口由消费方定义。** Payment 定义 `port.CatalogQuery`（只含 `FindProduct`），而非 Catalog 提供一个大而全的接口给别人用。

## 用例编排层（Application Service）

UseCase **只做编排，不做业务判断**。它是端口和聚合根方法的"胶水"：

```go
// internal/payment/application/charge_usecase.go
func (uc *ChargeUseCase) Purchase(ctx context.Context, req PurchaseRequest) (*model.PaymentTransaction, error) {
    // 1. 调端口：查商品价格
    product, _ := uc.catalog.FindProduct(ctx, req.ProductID)

    // 2. 调端口：查商户凭据 → 构建网关
    cred, _ := uc.merchantQuery.FindActiveCredential(ctx, req.MerchantID, model.PaymentMethodCard)
    gateway, _ := uc.gatewayFactory.BuildCardGateway(*cred)

    // 3. 调端口：应用优惠券（可选）
    coupon, _ := uc.couponApplier.Apply(ctx, req.CouponCode, req.UserID)

    // 4. 调领域服务：计算最终金额
    finalAmount, _, _, _ := service.CalculateFinalAmount(original, discountType, discountValue, taxBP)

    // 5. 调聚合根工厂：创建交易
    txn := model.NewPaymentTransaction(req.UserID, product.ID, finalAmount, cardToken)

    // 6. 调端口（网关）：授权
    result, err := gateway.Authorize(ctx, cardToken, finalAmount)
    if err != nil {
        uc.couponApplier.Rollback(ctx, req.CouponCode)  // Saga 补偿
        txn.MarkFailed(err.Error())
    }

    // 7. 调聚合根方法：状态流转
    txn.MarkAuthorized(result.ProviderRef, result.AuthCode)

    // 8. 调端口：持久化 + 发布事件
    uc.repo.Save(ctx, txn)
}
```

**UseCase 不做业务判断** — 它不检查 `product.IsActive`（那是聚合根的职责），不计算价格（那是领域服务的职责），只负责把它们串起来。

---

# 三、架构规则

## 分层结构

每个限界上下文内部遵循统一分层：

```
internal/<context>/
├── domain/
│   ├── model/       聚合根、实体、值对象、领域错误
│   ├── port/        被驱动端口接口 + 接口绑定的 DTO
│   ├── event/       领域事件定义
│   └── service/     领域服务（纯业务规则，不做编排）
├── application/     用例编排（串联 domain + port）
├── adapter/         被驱动适配器（实现 port 接口）
│   ├── persistence/ 仓储实现
│   ├── gateway/     外部系统 ACL
│   └── <跨上下文>/   ACL 适配器（如 payment/adapter/merchant）
└── handler/
    ├── http/        HTTP 驱动适配器 + 请求/响应 DTO
    └── middleware/  中间件（认证等）
```

**struct 放哪里？**

| 位置 | 放什么 | 示例 |
|---|---|---|
| `domain/model/` | 聚合根、值对象、领域错误 | `PaymentTransaction`, `Money`, `ErrInvalidStateTransition` |
| `domain/port/` | 端口接口 + 接口绑定的 DTO | `PaymentGateway`, `GatewayAuthResult`, `ProductView` |
| `domain/event/` | 领域事件 | `PaymentAuthorized`, `CardBound` |
| `domain/service/` | 领域服务 | `CalculateFinalAmount` |
| `application/` | 用例入参 DTO | `PurchaseRequest` |
| `handler/http/` | HTTP 请求/响应 DTO | `TransactionResponse` |

## 依赖方向

**从外到内，永不反转。**

```
                    main.go (Composition Root)
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
     ┌────────┐    ┌──────────┐   ┌──────────┐
     │handler │    │application│   │ adapter  │
     │  /http │    │          │   │/gateway  │
     │        │    │ UseCase  │   │/persist. │
     └───┬────┘    └────┬─────┘   └────┬─────┘
         │              │              │
         └──────┐       │       ┌──────┘
                ▼       ▼       ▼
           ┌──────────────────────────┐
           │        domain            │
           │  model/ port/ event/     │
           │  零外部依赖，只依赖标准库    │
           └──────────────────────────┘
```

| 规则 | 含义 | 违反示例 |
|---|---|---|
| domain 不 import 任何业务包 | 它定义接口（端口），不关心谁来实现 | ~~domain import adapter~~ |
| adapter 实现 domain 的接口 | 依赖方向 adapter → domain | ~~domain import persistence~~ |
| application 只依赖 domain | 通过端口接口调用 adapter | ~~usecase import adapter~~ |
| handler 只依赖 application + domain | 不直接引用 adapter | ~~handler import persistence~~ |
| 跨上下文 import 只在 adapter 和 main.go | domain 和 application 永远只依赖自己上下文的包 | ~~payment handler import identity middleware~~ |

## Composition Root

Composition Root 由 `main.go` + `internal/bootstrap/` 包共同构成。`main.go` 只保留启动入口，组装逻辑拆分到 bootstrap 包中。

**bootstrap 包结构：**

```
internal/bootstrap/
├── app.go               App struct + New() 总编排
├── wire_identity.go     identity 上下文组装
├── wire_catalog.go      catalog 上下文组装
├── wire_card.go         card 上下文组装
├── wire_merchant.go     merchant 上下文组装
├── wire_coupon.go       coupon 上下文组装
└── wire_payment.go      payment 上下文组装（依赖其他 4 个模块）
```

**每个上下文一个 Module struct + wire 函数：**

```go
// wire_catalog.go — 每个 wire 函数只做依赖组装（wiring），不做业务逻辑
type CatalogModule struct {
    Handler     *catalogHTTP.CatalogHandler
    ProductRepo *catalogPersistence.InMemoryProductRepository  // 共享给 payment ACL
}

func wireCatalog() *CatalogModule {
    repo := catalogPersistence.NewInMemoryProductRepository()
    uc := catalogApp.NewCatalogUseCase(repo)
    return &CatalogModule{Handler: catalogHTTP.NewCatalogHandler(uc), ProductRepo: repo}
}
```

**App.New() 总编排 — 按依赖顺序组装所有上下文：**

```go
// app.go
func New(cfg *config.Config) *App {
    // 0. 共享基础设施：各渠道 HTTP 客户端（Demo 使用内嵌 mock server）
    stripeClient := stripe.NewMockClient(cfg.StripeAPIKey)
    paypalClient := paypal.NewMockClient()

    // 1. 无跨上下文依赖的先组装
    identity := wireIdentity()
    catalog  := wireCatalog()
    card     := wireCard(stripeClient)  // card 与 payment 共享 stripe.Client
    merchant := wireMerchant()
    coupon   := wireCoupon()

    // 2. payment 依赖 infra 客户端 + 其他上下文的 Repository（通过 ACL adapter 隔离）
    payment := wirePayment(
        stripeClient, paypalClient,
        catalog.ProductRepo, card.CardRepo,
        merchant.MerchantRepo, coupon.CouponRepo,
    )

    // 3. 路由注册 + 中间件链
    mux := http.NewServeMux()
    catalog.Handler.RegisterRoutes(mux)
    // ...
    return &App{handler: identity.Middleware.Handle(mux)}
}
```

**main.go 极简化：**

```go
func main() {
    cfg := config.Load()
    app := bootstrap.New(cfg)
    log.Fatal(http.ListenAndServe(":"+cfg.Port, app.Handler()))
}
```

**设计要点：**

- `wire` 前缀明确表达"只做依赖组装" — 看到 `wire` 就知道函数内只有 new + 注入，无业务逻辑
- Module struct 暴露需要跨上下文共享的依赖（如 `ProductRepo`），共享只发生在 bootstrap 包内
- payment 的 `wirePayment` 接收其他模块的 Repository 指针作为参数，通过 ACL adapter 隔离
- bootstrap 包是系统中唯一 import 所有上下文的地方 — 它仍然是 Composition Root

---

# 四、关键设计模式

## ACL 防腐层

跨上下文通信通过 **adapter 层的 ACL（Anti-Corruption Layer）** 实现：

```
payment 上下文                              merchant 上下文
┌────────────────────┐                     ┌──────────────────┐
│ application/       │                     │                  │
│  ChargeUseCase     │                     │  domain/model/   │
│                    │   ┌────────────┐    │   Merchant       │
│  port.MerchantQuery│──▶│ adapter/   │───▶│   聚合根          │
│  (payment 自己定义) │   │ merchant/  │    │                  │
│                    │   │ ACL 防腐翻译│    │                  │
└────────────────────┘   └────────────┘    └──────────────────┘
```

ACL 做两件事：
1. **翻译模型** — `merchant.ChannelCredential` → `payment.ChannelCredentialView`
2. **翻译错误** — merchant 侧的 `ErrCredentialNotFound` → payment 侧的 `ErrMerchantCredentialNotFound`

```go
// internal/payment/adapter/merchant/merchant_adapter.go
func (a *MerchantAdapter) FindActiveCredential(ctx context.Context, merchantID string, channel model.PaymentMethod) (*port.ChannelCredentialView, error) {
    m, err := a.repo.FindByID(ctx, merchantModel.MerchantID(merchantID))
    if err != nil {
        return nil, port.ErrMerchantCredentialNotFound  // 防腐翻译错误
    }
    cred, _ := m.ActiveCredential(merchantModel.PaymentChannel(channel))
    return &port.ChannelCredentialView{                 // 防腐翻译模型
        CredentialID: string(cred.ID),
        Secrets:      cred.Secrets,
    }, nil
}
```

## Saga 补偿

优惠券核销采用 **Apply/Rollback 补偿对**。当支付授权失败时，自动回滚已核销的优惠券：

```go
// UseCase 编排 Saga：
coupon, err = uc.couponApplier.Apply(ctx, couponCode, userID)  // 步骤 A：核销
// ...
result, err := gateway.Authorize(ctx, token, amount)
if err != nil {
    uc.couponApplier.Rollback(ctx, couponCode)  // 步骤 A 的补偿
    txn.MarkFailed(err.Error())
    return txn, model.ErrAuthorizationDeclined
}
```

```
Apply (核销优惠券)  ──▶  CalculatePrice  ──▶  Authorize (网关授权)
        │                                           │
        │ ◀──── Rollback (补偿) ◀──── 失败 ◀────────┘
```

**Saga 补偿是 UseCase 的编排职责**，不是领域服务或聚合根的职责。聚合根只提供 `Apply()` 和 `Rollback()` 原语。

## 工厂方法

聚合根通过工厂方法创建，确保初始状态正确：

```go
// 工厂方法：初始状态 CREATED，Method 固定 CARD
txn := model.NewPaymentTransaction(userID, productID, amount, cardToken)

// 工厂方法：初始状态 ACTIVE，IsDefault 固定 false
card := model.NewSavedCard(userID, vaultToken, mask, holder)

// 工厂方法：初始状态 ACTIVE，触发 MerchantRegistered 事件
merchant := model.NewMerchant(name)
```

工厂方法保证"聚合根诞生即有效" — 不需要调用方手动设置状态字段。

## 多商户路由

Payment 上下文支持多商户隔离。核心思路：**每笔交易携带 MerchantID → 查凭据 → 动态构建该商户专属的 Gateway 实例**。

```
Purchase(merchantID="m-1", ...) ──▶ MerchantQuery.FindActiveCredential("m-1", CARD)
                                            │
                                            ▼ ChannelCredentialView{Secrets: {"api_key":"sk_live_xxx"}}
                                            │
                                            ▼ GatewayFactory.BuildCardGateway(cred)
                                            │
                                            ▼ 返回 m-1 专属的 Gateway 实例
                                            │
                                            ▼ gateway.Authorize(token, amount)
```

Capture/Refund 时，从已保存的 `txn.MerchantID` 重新构建 Gateway（`buildCaptureRefunder`），实现商户级隔离。

---

# 五、快速上手

```bash
cd example/payment-demo
go run main.go
```

### 端到端示例（Card 购买 → 扣款 → 退款）

```bash
# 1. 注册商户
curl -X POST localhost:8080/merchants \
  -H "Authorization: Bearer token_alice" \
  -d '{"name":"GameStore"}'

# 2. 添加 CARD 渠道凭据
curl -X POST localhost:8080/merchants/credentials \
  -H "Authorization: Bearer token_alice" \
  -d '{"merchant_id":"<merchant_id>","channel":"CARD","secrets":{"api_key":"sk_live_xxx"}}'

# 3. Card 购买（授权）
curl -X POST localhost:8080/charge \
  -H "Authorization: Bearer token_alice" \
  -d '{"merchant_id":"<merchant_id>","product_id":"gem_100","token_id":"tok_visa_4242","last4":"4242","brand":"Visa"}'
# → {"id":"txn-xxx", "status":"AUTHORIZED", "method":"CARD", "amount":1100}

# 4. 扣款
curl -X POST localhost:8080/capture \
  -H "Authorization: Bearer token_alice" \
  -d '{"transaction_id":"<txn_id>"}'
# → {"status":"CAPTURED"}

# 5. 退款
curl -X POST localhost:8080/refund \
  -H "Authorization: Bearer token_alice" \
  -d '{"transaction_id":"<txn_id>"}'
# → {"status":"REFUNDED"}
```

### 带优惠券的 PayPal 购买

```bash
# 创建优惠券
curl -X POST localhost:8080/coupons \
  -H "Authorization: Bearer token_alice" \
  -d '{"code":"SAVE10","discount_type":"PERCENTAGE","discount_value":1000,"max_uses":100,"valid_from":"2024-01-01T00:00:00Z","valid_until":"2030-12-31T23:59:59Z"}'

# PayPal 购买（带优惠券）
curl -X POST localhost:8080/charge/paypal \
  -H "Authorization: Bearer token_alice" \
  -d '{"merchant_id":"<merchant_id>","product_id":"gem_100","order_id":"5O190127TN364715T","payer_id":"FSMVU44LF3YUS","coupon_code":"SAVE10"}'
# → {"status":"AUTHORIZED", "method":"PAYPAL", "discount_amount":100, "tax_amount":90}
```

---

# 六、API 接口

### Catalog
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/products` | 商品列表 |
| GET | `/products?id=xxx` | 商品详情 |

### Card
| 方法 | 路径 | Body |
|---|---|---|
| POST | `/cards` | `{"one_time_token":"tok_xxx"}` |
| GET | `/cards` | 我的卡列表（不含已删除） |
| GET | `/cards?id=xxx` | 卡详情 |
| DELETE | `/cards` | `{"card_id":"xxx"}` |
| POST | `/cards/suspend` | `{"card_id":"xxx"}` |
| POST | `/cards/activate` | `{"card_id":"xxx"}` |
| PUT | `/cards/default` | `{"card_id":"xxx"}` |

### Merchant
| 方法 | 路径 | Body |
|---|---|---|
| POST | `/merchants` | `{"name":"xxx"}` |
| GET | `/merchants` | 商户列表 |
| POST | `/merchants/credentials` | `{"merchant_id","channel","secrets"}` |
| POST | `/merchants/suspend` | `{"merchant_id":"xxx"}` |

### Coupon
| 方法 | 路径 | Body |
|---|---|---|
| POST | `/coupons` | `{"code","discount_type","discount_value","max_uses","valid_from","valid_until"}` |
| GET | `/coupons?code=SAVE10` | 按编码查询 |

### Payment
| 方法 | 路径 | Body |
|---|---|---|
| POST | `/charge` | `{"merchant_id","product_id","token_id"或"saved_card_id","coupon_code?"}` |
| POST | `/charge/paypal` | `{"merchant_id","product_id","order_id","payer_id","coupon_code?"}` |
| POST | `/capture` | `{"transaction_id":"xxx"}` |
| POST | `/refund` | `{"transaction_id":"xxx"}` |
| GET | `/transaction?id=xxx` | 查询交易 |

---

# 七、测试策略

```bash
go test ./... -count=1
```

测试分层与 DDD 分层对应：

| 层 | 测什么 | 怎么测 | 示例 |
|---|---|---|---|
| **domain/model** | 聚合根状态机不变式、值对象运算 | 纯单元测试，无 mock | `TestSavedCard_Suspend_AlreadySuspended_ReturnsError` |
| **application** | UseCase 编排流程、端口调用顺序 | stub 实现所有端口接口 | `TestChargeUseCase_Purchase_Success` |
| **adapter** | 仓储契约、ACL 翻译正确性 | 内存实现 + 接口断言 | `TestMerchantAdapter_FindActiveCredential` |
| **handler/http** | HTTP 状态码、请求解析、错误映射 | `httptest` + stub UseCase | `TestChargeHandler_NoAuth_Returns401` |

**测试中的端口替换**（依赖倒置的实际价值）：

```go
// handler 测试不需要真实的 Gateway / Repository / Catalog
// 只需 stub 实现端口接口即可
type handlerStubCatalog struct {
    product *port.ProductView
    err     error
}
func (c *handlerStubCatalog) FindProduct(_ context.Context, _ string) (*port.ProductView, error) {
    return c.product, c.err
}
```

编译期验证 stub 实现了接口：
```go
var _ port.CardRepository = (*testRepo)(nil)
```

---

# 八、扩展指南

六边形架构的核心价值：**换掉任何适配器，domain 和 application 代码零改动。**

| 需求 | 改动位置 | 不影响 |
|---|---|---|
| 接入真实 Stripe/PayPal | `infra/stripe`、`infra/paypal` 切换 `NewClient` 替代 `NewMockClient`，adapter 零改动 | domain, application, handler |
| 换 PostgreSQL 持久化 | 新增 `adapter/persistence/pg_repo.go`，实现同一 port 接口 | domain, application, handler |
| 加 gRPC 入口 | 新增 `handler/grpc/` | domain, application, adapter |
| 新增支付渠道（Apple Pay） | merchant 加枚举 + payment 加 Gateway 端口 + adapter | 其他上下文 |
| 接入真实商品服务 | 新增 `payment/adapter/catalog/api_catalog.go` | domain, application, handler |
| 新增限界上下文 | 新建 `internal/<context>/` 完整分层 + bootstrap 新增 `wire_<context>.go` | 已有上下文 |
