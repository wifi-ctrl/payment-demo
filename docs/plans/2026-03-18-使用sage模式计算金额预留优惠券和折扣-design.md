# 技术设计文档

- **需求**: 使用sage模式计算金额预留优惠券和折扣以及税务计算
- **项目**: payment-demo
- **日期**: 2026-03-18
- **状态**: 待确认

---

# 领域设计文档

## 1. 现有代码 Review

| # | 问题 | 位置 | 改进方向 |
|---|------|------|---------|
| 1 | `pricing_adapter.go` 引用了 `internal/pricing/domain/model` 和 `internal/pricing/domain/port`，但这两个包完全不存在，项目无法编译 | `internal/payment/adapter/pricing/pricing_adapter.go:10-11` | 本次需求补全 `pricing` 上下文的 domain 层 |
| 2 | `catalog/domain/model/product.go` 自定义了局部 `Money` struct，与 `shared/money.Money` 重复定义 | `internal/catalog/domain/model/product.go:20-23` | 应 import `shared/money` 复用；当前不影响需求，记录技术债 |
| 3 | `payment/domain/model/money.go` 同样重复定义了 `Money`（尽管内部引用了 shared 的 error），没有直接复用 `shared/money.Money` 类型 | `internal/payment/domain/model/money.go:1-50` | 记录技术债，本次不改动以免破坏现有接口 |
| 4 | `PricingView.Status` 是裸 `string`，payment 上下文无法做状态校验 | `internal/payment/domain/port/pricing.go:24` | pricing 设计中明确 `CalculationStatus` 枚举，ACL 翻译时对齐 |
| 5 | `ChargeUseCase.Purchase` 使用商品目录价格作为支付金额，没有定价计算介入点 | `internal/payment/application/charge_usecase.go:75` | Purchase 入参增加可选 `CalculationID`，命中时用 `FinalAmount` 替换 `catalog` 价格 |

---

## 2. 统一语言术语表

| 术语 | 含义 | 类型 |
|------|------|------|
| PriceCalculation | 一次完整的定价计算结果，包含原价、折扣、税费、最终价 | 聚合根 |
| CalculationID | PriceCalculation 的全局唯一标识 | 值对象 |
| Coupon | 优惠券，持有折扣规则，可被用户在定价时应用 | 实体 |
| CouponCode | 优惠券业务编码，用户输入的字符串（如 `SAVE10`） | 值对象 |
| DiscountRule | 折扣规则：PERCENTAGE（百分比）或 FIXED（固定金额） | 值对象 |
| DiscountAmount | 本次计算中实际抵扣的金额（Money 值对象） | 值对象 |
| TaxRate | 税率（以 basis point 存储，如 1000 = 10.00%） | 值对象 |
| TaxAmount | 本次计算中产生的税额（Money 值对象） | 值对象 |
| CalculationStatus | 定价结果的生命周期状态：PENDING / CONFIRMED / CANCELLED | 值对象枚举 |
| PriceCalculationService | 无状态领域服务，执行折扣 + 税务的纯计算逻辑 | 领域服务 |
| CouponQuery | pricing 对 coupon 的内部查询端口 | 端口 |

---

## 3. 上下文归属

**判断依据**

| 上下文候选 | 独立生命周期 | 独立不变量 | 独立变更频率 | 结论 |
|-----------|------------|----------|------------|------|
| pricing（定价计算） | ✅ PENDING→CONFIRMED→CANCELLED | ✅ 折扣不超过原价、最终价不为负 | ✅ 折扣策略独立于支付渠道演进 | **新建上下文** |
| coupon（优惠券管理） | ✅ ACTIVE→USED/EXPIRED | ✅ 使用次数限制、有效期 | ✅ 运营独立配置 | **新建上下文** |
| 税务计算（tax） | ❌ 无独立生命周期（纯计算） | ❌ 无独立存储不变量 | ❌ 规则跟随 pricing 演进 | **领域服务，归入 pricing** |

**上下文映射图**

```
┌──────────────┐     ACL(CouponQuery)    ┌──────────────┐
│   coupon     │◄────────────────────────│   pricing    │
│  (新建)      │                         │  (新建)      │
└──────────────┘                         └──────┬───────┘
                                                │ ACL(PricingQuery)
                                         已有适配器
                                                │
                                         ┌──────▼───────┐
                                         │   payment    │
                                         │  (已有)      │
                                         └──────────────┘
```

**Saga 编排归属**：`pricing/application/` — pricing 是定价计算的系统记录方，orchestrate 整个 Saga（查 Coupon → 计算 → 持久化）。payment 通过 `PricingQuery` ACL 只读消费结果。

---

## 4. 共享类型复用

| 类型 | 决策 | 路径 |
|------|------|------|
| `Money` | **复用** `shared/money.Money` | `payment-demo/internal/shared/money` |
| `DomainEvent` | **复用** `shared/event.DomainEvent` | `payment-demo/internal/shared/event` |
| `CalculationID` | **新增** string 类型别名 | `pricing/domain/model/calculation.go` |
| `CouponID` | **新增** string 类型别名 | `coupon/domain/model/coupon.go` |
| `TaxRate` | **新增** int64（basis point） | `pricing/domain/model/tax.go` |

> `shared/money.Money` 新增 `MultiplyBasisPoint(bp int64) Money` 方法，供税额/百分比折扣计算使用，避免裸 float。

---

## 5. 聚合根设计

### 5.1 Coupon（coupon 上下文）

**字段**
```go
type Coupon struct {
    ID         CouponID
    Code       CouponCode        // 用户输入的唯一编码，如 "SAVE10"
    Rule       DiscountRule      // {Type: PERCENTAGE|FIXED, Value int64}
    MaxUses    int               // 0 = 不限
    UsedCount  int
    ValidFrom  time.Time
    ValidUntil time.Time
    Status     CouponStatus      // ACTIVE / EXHAUSTED / EXPIRED
    Events     []event.DomainEvent
}
```

**状态机**
```
      [Create]            [Use / MaxUses reached]      [ValidUntil 过期]
ACTIVE ──────────────────► EXHAUSTED
ACTIVE ──────────────────────────────────────────────► EXPIRED
                     (MarkExhausted)              (MarkExpired)
```

**方法签名**
```go
func NewCoupon(code CouponCode, rule DiscountRule, maxUses int, from, until time.Time) *Coupon
func (c *Coupon) Apply() error                          // ACTIVE+未过期+有剩余次数 → UsedCount++；否则 ErrCouponNotApplicable
func (c *Coupon) IsApplicable(now time.Time) bool
func (c *Coupon) ClearEvents() []event.DomainEvent
```

**错误**
```go
var ErrCouponNotFound      = errors.New("coupon not found")
var ErrCouponNotApplicable = errors.New("coupon is expired, exhausted or inactive")
var ErrCouponCodeConflict  = errors.New("coupon code already exists")
```

---

### 5.2 PriceCalculation（pricing 上下文）

**字段**
```go
type PriceCalculation struct {
    ID             CalculationID
    UserID         string
    ProductID      string
    OriginalAmount money.Money      // shared/money.Money
    CouponID       *CouponID        // nullable
    DiscountAmount money.Money      // 实际抵扣额（FIXED 或 PERCENTAGE 换算后）
    TaxRate        TaxRate          // basis point，如 1000 = 10%
    TaxAmount      money.Money      // OriginalAmount 扣减折扣后 × TaxRate
    FinalAmount    money.Money      // OriginalAmount - DiscountAmount + TaxAmount
    Status         CalculationStatus // PENDING / CONFIRMED / CANCELLED
    CreatedAt      time.Time
    Events         []event.DomainEvent
}
```

**状态机**
```
            Calculate()               Confirm()           Cancel()
[工厂方法] ──► PENDING ──────────────► CONFIRMED
                         └──────────────────────────────► CANCELLED
                              Cancel()（仅 PENDING 可取消）
```

**方法签名**
```go
func NewPriceCalculation(userID, productID string, original money.Money,
    couponID *CouponID, discount money.Money, taxRate TaxRate) *PriceCalculation
func (p *PriceCalculation) Confirm() error    // PENDING → CONFIRMED；否则 ErrInvalidStateTransition
func (p *PriceCalculation) Cancel() error     // PENDING → CANCELLED；否则 ErrInvalidStateTransition
func (p *PriceCalculation) ClearEvents() []event.DomainEvent
```

**错误**
```go
var ErrCalculationNotFound    = errors.New("price calculation not found")
var ErrInvalidStateTransition = errors.New("invalid state transition")
var ErrDiscountExceedsAmount  = errors.New("discount exceeds original amount")
var ErrNegativeFinalAmount    = errors.New("final amount cannot be negative")
```

---

### 5.3 PriceCalculationService（pricing 上下文，无状态领域服务）

```go
// CalcInput 计算入参
type CalcInput struct {
    Original  money.Money
    CouponID  *CouponID      // nil = 不使用优惠券
    TaxRate   TaxRate        // 调用方从配置/商户规则中传入
}

// CalcResult 计算出参（值对象）
type CalcResult struct {
    DiscountAmount money.Money
    TaxAmount      money.Money
    FinalAmount    money.Money
}

// PriceCalculationService 纯计算，无 IO、无副作用
type PriceCalculationService struct{}

func (s PriceCalculationService) Calculate(input CalcInput, rule *DiscountRule) (CalcResult, error)
// 内部顺序：1.应用折扣 → 2.计算税额(折后价×TaxRate) → 3.校验FinalAmount≥0
```

---

## 6. 端口接口

### coupon/domain/port

```go
// CouponRepository coupon 上下文仓储端口
type CouponRepository interface {
    Save(ctx context.Context, c *model.Coupon) error
    FindByID(ctx context.Context, id model.CouponID) (*model.Coupon, error)
    FindByCode(ctx context.Context, code model.CouponCode) (*model.Coupon, error)
}
```

### pricing/domain/port

```go
// PriceCalculationRepository pricing 聚合根仓储端口
// 此接口同时被 payment/adapter/pricing/PricingAdapter 跨上下文引用（仅 adapter 层允许）
type PriceCalculationRepository interface {
    Save(ctx context.Context, pc *model.PriceCalculation) error
    FindByID(ctx context.Context, id model.CalculationID) (*model.PriceCalculation, error)
}

// CouponQuery pricing 对 coupon 上下文的 ACL 查询端口（消费方定义）
// 实现在 pricing/adapter/coupon/ 中
type CouponQuery interface {
    FindApplicableCoupon(ctx context.Context, couponCode string) (*CouponView, error)
}

// CouponView pricing 视角的优惠券视图
type CouponView struct {
    CouponID    string
    DiscountType  string // "PERCENTAGE" / "FIXED"
    DiscountValue int64  // PERCENTAGE: basis point；FIXED: cents
}

// TaxRateQuery pricing 对税率配置的查询端口（可由 infra/config 实现）
type TaxRateQuery interface {
    FindTaxRate(ctx context.Context, productID string, currency string) (int64, error) // basis point
}
```

---

## 7. 领域事件

| 事件名（过去式） | 触发时机 | payload 字段 |
|----------------|---------|------------|
| `coupon.applied` | `Coupon.Apply()` 成功 | `CouponID`, `UserID`, `OccurredAt` |
| `pricing.calculated` | `NewPriceCalculation` 工厂完成 | `CalculationID`, `ProductID`, `OriginalAmount`, `DiscountAmount`, `TaxAmount`, `FinalAmount`, `Currency`, `OccurredAt` |
| `pricing.confirmed` | `PriceCalculation.Confirm()` | `CalculationID`, `FinalAmount`, `Currency`, `OccurredAt` |
| `pricing.cancelled` | `PriceCalculation.Cancel()` | `CalculationID`, `OccurredAt` |

---

## 8. 应用层编排（Saga 模式）

**编排器**：`pricing/application/PricingUseCase`（本地 Saga，单进程内补偿）

### CalculatePrice Saga 步骤

```
CalculatePrice(userID, productID, original, couponCode?, taxProductID)
      │
      ▼
① CouponQuery.FindApplicableCoupon(couponCode)   ← [skip if no couponCode]
      │ 失败 → ErrCouponNotApplicable（终止，无需补偿）
      ▼
② CouponRepository.Apply() → coupon.Apply()      ← [写操作，有补偿步骤]
      │ 失败 → ErrCouponNotApplicable（终止）
      ▼
③ TaxRateQuery.FindTaxRate(productID, currency)
      │ 失败 → 默认税率 0（可配置）
      ▼
④ PriceCalculationService.Calculate(original, rule, taxRate)
      │ 失败(discount>amount) → ④-COMP: coupon.Rollback(UsedCount--)
      ▼
⑤ NewPriceCalculation(...)  → Status=PENDING → 触发 pricing.calculated
      │
      ▼
⑥ PriceCalculationRepository.Save(pc)
      │ 失败 → ⑥-COMP: coupon.Rollback → Save coupon
      ▼
      ✅ 返回 PriceCalculation（PENDING）
```

> **ConfirmCalculation**：payment 支付成功后调用，`PENDING → CONFIRMED`，触发 `pricing.confirmed`。  
> **一致性保证**：同一进程内 InMemory 仓储使用 `sync.RWMutex`；生产环境两个仓储操作在同一 DB 事务中执行（由 infra 层 UoW 保证）。

### payment 集成点

`ChargeUseCase.Purchase` 入参增加可选 `CalculationID string`：
- 非空时：调用 `PricingQuery.FindCalculation()` 校验 `Status==PENDING` → 用 `FinalAmount` 替换 catalog 价格 → 支付成功后调用 `ConfirmCalculation`
- 为空时：沿用现有 catalog 价格逻辑（向后兼容）

---

## 9. API 设计

| 方法 | 路径 | 请求参数 | 成功响应 | 错误码 |
|------|------|---------|---------|-------|
| `POST` | `/pricing/calculate` | `{ product_id, user_id, coupon_code?, tax_product_id? }` | `201` `{ calculation_id, original_amount, discount_amount, tax_amount, final_amount, currency, status }` | `400` coupon无效, `404` 商品不存在 |
| `GET` | `/pricing/calculations/{id}` | — | `200` 同上 | `404` |
| `POST` | `/pricing/calculations/{id}/cancel` | — | `200` `{ status: "CANCELLED" }` | `409` 非PENDING状态 |
| `POST` | `/coupons` | `{ code, discount_type, discount_value, max_uses, valid_from, valid_until }` | `201` `{ coupon_id, code, status }` | `409` code重复 |
| `GET` | `/coupons/{code}` | — | `200` coupon详情 | `404` |

---

## 10. 实现顺序

```
# ── shared 层扩展 ──────────────────────────────────────────────
1. internal/shared/money/money.go          ← 新增 MultiplyBasisPoint() 方法

# ── coupon 上下文（内层先）────────────────────────────────────
2. internal/coupon/domain/model/coupon.go      聚合根 + DiscountRule + CouponStatus
3. internal/coupon/domain/model/errors.go
4. internal/coupon/domain/event/event.go
5. internal/coupon/domain/port/repository.go
6. internal/coupon/application/coupon_usecase.go
7. internal/coupon/adapter/inmem/coupon_repo.go
8. internal/coupon/handler/coupon_handler.go

# ── pricing 上下文（内层先）────────────────────────────────────
9.  internal/pricing/domain/model/calculation.go   PriceCalculation聚合根
10. internal/pricing/domain/model/tax.go            TaxRate值对象
11. internal/pricing/domain/model/errors.go
12. internal/pricing/domain/event/event.go
13. internal/pricing/domain/service/price_calculation_service.go
14. internal/pricing/domain/port/repository.go     PriceCalculationRepository
15. internal/pricing/domain/port/coupon.go          CouponQuery + CouponView
16. internal/pricing/domain/port/tax.go             TaxRateQuery
17. internal/pricing/application/pricing_usecase.go  Saga编排
18. internal/pricing/adapter/coupon/coupon_adapter.go  ACL: pricing→coupon
19. internal/pricing/adapter/inmem/calculation_repo.go
20. internal/pricing/handler/pricing_handler.go

# ── payment 集成层 ─────────────────────────────────────────────
21. internal/payment/adapter/pricing/pricing_adapter.go  ← 已存在，补全 pricing domain 后可编译
22. internal/payment/application/charge_usecase.go       ← 扩展 PurchaseRequest.CalculationID

# ── 组装 ───────────────────────────────────────────────────────
23. main.go   ← 注入 PricingAdapter、CouponAdapter，完成跨上下文依赖注入
```

---

## 二、验收标准

我先读取现有代码，充分了解项目结构后再生成验收标准。充分了解了项目全貌，现在生成完整的验收标准。

```yaml
acceptance_criteria:

  # ════════════════════════════════════════════════════════════════
  # shared 层
  # ════════════════════════════════════════════════════════════════

  - id: AC-1
    layer: domain
    context: shared/money
    scenario: "MultiplyBasisPoint 正常计算"
    given: "Money{Amount: 10000, Currency: 'USD'}（= $100.00），basis point = 1000（= 10%）"
    when: "调用 MultiplyBasisPoint(1000)"
    then: "返回 Money{Amount: 1000, Currency: 'USD'}（= $10.00），原 Money 不变"
    test_type: unit

  - id: AC-2
    layer: domain
    context: shared/money
    scenario: "MultiplyBasisPoint 税率为零"
    given: "Money{Amount: 5000, Currency: 'USD'}，basis point = 0"
    when: "调用 MultiplyBasisPoint(0)"
    then: "返回 Money{Amount: 0, Currency: 'USD'}"
    test_type: unit

  - id: AC-3
    layer: domain
    context: shared/money
    scenario: "MultiplyBasisPoint 满额税率（10000 bp = 100%）"
    given: "Money{Amount: 200, Currency: 'CNY'}，basis point = 10000"
    when: "调用 MultiplyBasisPoint(10000)"
    then: "返回 Money{Amount: 200, Currency: 'CNY'}"
    test_type: unit

  # ════════════════════════════════════════════════════════════════
  # AC-4 ~ AC-19: domain 层
  # ════════════════════════════════════════════════════════════════

  # ── Coupon 聚合根 ─────────────────────────────────────────────

  - id: AC-4
    layer: domain
    context: coupon
    scenario: "NewCoupon 工厂方法创建合法优惠券"
    given: |
      code='SAVE10'，rule={Type:PERCENTAGE, Value:1000}，maxUses=100，
      validFrom=now，validUntil=now+7days
    when: "调用 NewCoupon(...)"
    then: |
      - ID 非空（UUID 格式）
      - Status = ACTIVE
      - UsedCount = 0
      - Events 为空（工厂不产生事件）
      - Code/Rule/MaxUses/ValidFrom/ValidUntil 与入参一致
    test_type: unit

  - id: AC-5
    layer: domain
    context: coupon
    scenario: "Coupon.Apply() 正常核销"
    given: "ACTIVE 优惠券，MaxUses=5，UsedCount=0，ValidUntil=now+7days"
    when: "调用 Apply()（now 在有效期内）"
    then: |
      - 返回 nil
      - UsedCount = 1
      - Status 仍为 ACTIVE
      - Events 包含 1 个 CouponApplied 事件，字段含 CouponID/OccurredAt
    test_type: unit

  - id: AC-6
    layer: domain
    context: coupon
    scenario: "Coupon.Apply() 达到最大使用次数后状态转为 EXHAUSTED"
    given: "ACTIVE 优惠券，MaxUses=1，UsedCount=0，ValidUntil=now+7days"
    when: "调用 Apply() 一次"
    then: |
      - 返回 nil
      - UsedCount = 1
      - Status = EXHAUSTED
      - Events 包含 CouponApplied 事件
    test_type: unit

  - id: AC-7
    layer: domain
    context: coupon
    scenario: "Coupon.Apply() 在 EXHAUSTED 状态下拒绝"
    given: "Status=EXHAUSTED 的优惠券"
    when: "调用 Apply()"
    then: |
      - 返回 ErrCouponNotApplicable
      - UsedCount 不变
      - Status 仍为 EXHAUSTED
      - Events 无新增事件
    test_type: unit

  - id: AC-8
    layer: domain
    context: coupon
    scenario: "Coupon.Apply() 已过有效期拒绝"
    given: "ACTIVE 优惠券，ValidUntil = now-1day（已过期）"
    when: "调用 Apply()"
    then: |
      - 返回 ErrCouponNotApplicable
      - Status 不变
      - Events 无新增事件
    test_type: unit

  - id: AC-9
    layer: domain
    context: coupon
    scenario: "Coupon.IsApplicable() 边界：ValidFrom 恰好等于 now"
    given: "ACTIVE 优惠券，ValidFrom=T，ValidUntil=T+1day，MaxUses=0（不限），UsedCount=0"
    when: "以 now=T 调用 IsApplicable(T)"
    then: "返回 true"
    test_type: unit

  - id: AC-10
    layer: domain
    context: coupon
    scenario: "Coupon MaxUses=0 时不限使用次数"
    given: "ACTIVE 优惠券，MaxUses=0，UsedCount=99，ValidUntil=now+7days"
    when: "调用 Apply()"
    then: |
      - 返回 nil
      - UsedCount = 100
      - Status 仍为 ACTIVE（不因次数转 EXHAUSTED）
    test_type: unit

  # ── PriceCalculation 聚合根 ───────────────────────────────────

  - id: AC-11
    layer: domain
    context: pricing
    scenario: "NewPriceCalculation 工厂方法 — 无优惠券"
    given: |
      userID='u1', productID='p1',
      original=Money{10000,'USD'},
      couponID=nil, discount=Money{0,'USD'},
      taxRate=1000（10%）
    when: "调用 NewPriceCalculation(...)"
    then: |
      - ID 非空
      - Status = PENDING
      - DiscountAmount = Money{0,'USD'}
      - TaxAmount = Money{1000,'USD'}（10000 × 10%）
      - FinalAmount = Money{11000,'USD'}（10000 - 0 + 1000）
      - Events 包含 1 个 PriceCalculated 事件，字段含 CalculationID/ProductID/OriginalAmount/DiscountAmount/TaxAmount/FinalAmount/Currency/OccurredAt
    test_type: unit

  - id: AC-12
    layer: domain
    context: pricing
    scenario: "NewPriceCalculation 工厂方法 — PERCENTAGE 折扣"
    given: |
      original=Money{10000,'USD'},
      discount=Money{1000,'USD'}（外部已换算为固定金额）,
      taxRate=500（5%），couponID=非nil
    when: "调用 NewPriceCalculation(...)"
    then: |
      - DiscountAmount = Money{1000,'USD'}
      - TaxAmount = Money{450,'USD'}（9000 × 5%）
      - FinalAmount = Money{9450,'USD'}
      - Status = PENDING
      - Events 含 PriceCalculated 事件
    test_type: unit

  - id: AC-13
    layer: domain
    context: pricing
    scenario: "PriceCalculation.Confirm() 正常状态转换"
    given: "Status=PENDING 的 PriceCalculation"
    when: "调用 Confirm()"
    then: |
      - 返回 nil
      - Status = CONFIRMED
      - Events 包含 1 个 PriceConfirmed 事件，含 CalculationID/FinalAmount/Currency/OccurredAt
    test_type: unit

  - id: AC-14
    layer: domain
    context: pricing
    scenario: "PriceCalculation.Confirm() 在非 PENDING 状态下拒绝"
    given: "Status=CANCELLED 的 PriceCalculation"
    when: "调用 Confirm()"
    then: |
      - 返回 ErrInvalidStateTransition
      - Status 仍为 CANCELLED
      - Events 无新增事件
    test_type: unit

  - id: AC-15
    layer: domain
    context: pricing
    scenario: "PriceCalculation.Cancel() 正常状态转换"
    given: "Status=PENDING 的 PriceCalculation"
    when: "调用 Cancel()"
    then: |
      - 返回 nil
      - Status = CANCELLED
      - Events 包含 1 个 PriceCancelled 事件，含 CalculationID/OccurredAt
    test_type: unit

  - id: AC-16
    layer: domain
    context: pricing
    scenario: "PriceCalculation.Cancel() 在 CONFIRMED 状态下拒绝"
    given: "Status=CONFIRMED 的 PriceCalculation"
    when: "调用 Cancel()"
    then: |
      - 返回 ErrInvalidStateTransition
      - Status 仍为 CONFIRMED
      - Events 无新增事件
    test_type: unit

  # ── PriceCalculationService 领域服务 ─────────────────────────

  - id: AC-17
    layer: domain
    context: pricing
    scenario: "PriceCalculationService.Calculate — FIXED 折扣 + 税"
    given: |
      input.Original=Money{10000,'USD'},
      rule={Type:FIXED, Value:200}（200 cents），
      input.TaxRate=1000（10%）
    when: "调用 Calculate(input, rule)"
    then: |
      - DiscountAmount = Money{200,'USD'}
      - TaxAmount = Money{980,'USD'}（9800 × 10%）
      - FinalAmount = Money{10780,'USD'}（9800 + 980）
      - 返回 nil error
    test_type: unit

  - id: AC-18
    layer: domain
    context: pricing
    scenario: "PriceCalculationService.Calculate — 折扣超过原价"
    given: |
      input.Original=Money{100,'USD'},
      rule={Type:FIXED, Value:200}（折扣 > 原价），
      input.TaxRate=0
    when: "调用 Calculate(input, rule)"
    then: |
      - 返回 ErrDiscountExceedsAmount
      - CalcResult 为零值
    test_type: unit

  - id: AC-19
    layer: domain
    context: pricing
    scenario: "PriceCalculationService.Calculate — 无优惠券（rule=nil）"
    given: |
      input.Original=Money{5000,'USD'}, rule=nil,
      input.TaxRate=500（5%）
    when: "调用 Calculate(input, nil)"
    then: |
      - DiscountAmount = Money{0,'USD'}
      - TaxAmount = Money{250,'USD'}（5000 × 5%）
      - FinalAmount = Money{5250,'USD'}
      - 返回 nil error
    test_type: unit

  # ════════════════════════════════════════════════════════════════
  # AC-20 ~ AC-29: application 层
  # ════════════════════════════════════════════════════════════════

  - id: AC-20
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — 无优惠券完整成功路径"
    given: |
      CouponQuery 不调用（couponCode 为空），
      TaxRateQuery.FindTaxRate 返回 1000（10%），
      PriceCalculationRepository.Save 成功
    when: |
      调用 CalculatePrice(userID='u1', productID='p1',
        original=Money{10000,'USD'}, couponCode='', taxProductID='p1')
    then: |
      - 返回 Status=PENDING 的 PriceCalculation，ID 非空
      - FinalAmount = Money{11000,'USD'}
      - PriceCalculationRepository.Save 被调用 1 次
      - PriceCalculated 事件被记录
    test_type: integration

  - id: AC-21
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — 优惠券不存在时终止"
    given: |
      CouponQuery.FindApplicableCoupon('NOTEXIST') 返回 ErrCouponNotFound
    when: |
      调用 CalculatePrice(..., couponCode='NOTEXIST', ...)
    then: |
      - 返回错误（ErrCouponNotApplicable 或透传 ErrCouponNotFound）
      - PriceCalculationRepository.Save 未被调用
      - Coupon.Apply() 未被调用
    test_type: integration

  - id: AC-22
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — 优惠券不可用（EXHAUSTED）时终止"
    given: |
      CouponQuery.FindApplicableCoupon 返回 CouponView（DiscountType=PERCENTAGE），
      CouponRepository.Apply() 内部 coupon.Apply() 返回 ErrCouponNotApplicable
    when: |
      调用 CalculatePrice(..., couponCode='USED', ...)
    then: |
      - 返回 ErrCouponNotApplicable
      - PriceCalculationRepository.Save 未被调用
    test_type: integration

  - id: AC-23
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — 折扣超过原价触发补偿（Coupon UsedCount 回滚）"
    given: |
      CouponQuery 返回 CouponView{DiscountType:FIXED, DiscountValue:999999},
      CouponRepository.Apply() 成功（UsedCount 已+1），
      PriceCalculationService.Calculate 返回 ErrDiscountExceedsAmount
    when: "调用 CalculatePrice(...)"
    then: |
      - 返回 ErrDiscountExceedsAmount
      - Coupon.Rollback（UsedCount--）被执行
      - CouponRepository.Save 以回滚后的 Coupon 再次调用
      - PriceCalculationRepository.Save 未被调用
    test_type: integration

  - id: AC-24
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — PriceCalculationRepository.Save 失败触发补偿"
    given: |
      CouponQuery/Apply 均成功，
      PriceCalculationService.Calculate 成功，
      PriceCalculationRepository.Save 返回 error
    when: "调用 CalculatePrice(..., couponCode='SAVE10', ...)"
    then: |
      - 返回持久化错误
      - Coupon 补偿回滚被执行（UsedCount--，CouponRepository.Save 被调用）
    test_type: integration

  - id: AC-25
    layer: application
    context: pricing
    scenario: "CalculatePrice Saga — TaxRateQuery 失败时使用默认税率 0"
    given: |
      TaxRateQuery.FindTaxRate 返回 error，
      CouponQuery 不调用（无 couponCode），
      PriceCalculationRepository.Save 成功
    when: "调用 CalculatePrice(original=Money{10000,'USD'}, couponCode='')"
    then: |
      - 返回 Status=PENDING 的 PriceCalculation，无 error
      - TaxAmount = Money{0,'USD'}（默认税率 0）
      - FinalAmount = Money{10000,'USD'}
    test_type: integration

  - id: AC-26
    layer: application
    context: pricing
    scenario: "ConfirmCalculation — 正常确认"
    given: |
      PriceCalculationRepository.FindByID 返回 Status=PENDING 的计算结果
    when: "调用 ConfirmCalculation(calculationID)"
    then: |
      - 返回 Status=CONFIRMED 的 PriceCalculation
      - PriceCalculationRepository.Save 被调用 1 次
      - PriceConfirmed 事件被记录
    test_type: integration

  - id: AC-27
    layer: application
    context: pricing
    scenario: "ConfirmCalculation — 计算记录不存在"
    given: "PriceCalculationRepository.FindByID 返回 ErrCalculationNotFound"
    when: "调用 ConfirmCalculation('nonexistent-id')"
    then: |
      - 返回 ErrCalculationNotFound
      - Save 未被调用
    test_type: integration

  - id: AC-28
    layer: application
    context: pricing
    scenario: "CancelCalculation — 正常取消"
    given: |
      PriceCalculationRepository.FindByID 返回 Status=PENDING
    when: "调用 CancelCalculation(calculationID)"
    then: |
      - 返回 Status=CANCELLED
      - PriceCalculationRepository.Save 调用 1 次
      - PriceCancelled 事件被记录
    test_type: integration

  - id: AC-29
    layer: application
    context: payment
    scenario: "Purchase 携带合法 CalculationID — 使用 FinalAmount 替换 catalog 价格"
    given: |
      PricingQuery.FindCalculation 返回 PricingView{Status:'PENDING', FinalAmount:9450, Currency:'USD'},
      Catalog 返回 ProductView{Amount:10000, Currency:'USD', IsActive:true},
      Gateway.Authorize 成功
    when: |
      调用 Purchase(req{CalculationID:'calc-1', MerchantID:'m1', UserID:'u1', ProductID:'p1', Token:...})
    then: |
      - Gateway.Authorize 接收到的 amount = Money{9450,'USD'}（非 10000）
      - 交易持久化后 Amount = Money{9450,'USD'}
      - 返回 Status=AUTHORIZED 的交易
    test_type: integration

  # ════════════════════════════════════════════════════════════════
  # AC-30 ~ AC-39: handler 层
  # ════════════════════════════════════════════════════════════════

  - id: AC-30
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculate 成功"
    given: |
      认证用户 userID='u1'（AuthMiddleware 已注入 ctx），
      body={ product_id:'p1', coupon_code:'', tax_product_id:'p1' },
      PricingUseCase.CalculatePrice 返回合法 PriceCalculation
    when: "POST /pricing/calculate"
    then: |
      - HTTP 201 Created
      - body 含 { calculation_id, original_amount, discount_amount,
                  tax_amount, final_amount, currency, status:'PENDING' }
      - calculation_id 非空字符串
    test_type: http

  - id: AC-31
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculate — 未认证"
    given: "请求无 Authorization header（或 AuthMiddleware 未注入 userID）"
    when: "POST /pricing/calculate"
    then: "HTTP 401 Unauthorized"
    test_type: http

  - id: AC-32
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculate — 优惠券无效返回 400"
    given: |
      认证用户，body 含 coupon_code:'INVALID'，
      PricingUseCase 返回 ErrCouponNotApplicable
    when: "POST /pricing/calculate"
    then: "HTTP 400 Bad Request，body 含错误描述"
    test_type: http

  - id: AC-33
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculate — body 缺少 product_id 返回 422"
    given: "认证用户，body={ coupon_code:'X' }（缺少 product_id）"
    when: "POST /pricing/calculate"
    then: "HTTP 422 Unprocessable Entity"
    test_type: http

  - id: AC-34
    layer: handler
    context: pricing
    scenario: "GET /pricing/calculations?id=xxx 成功"
    given: |
      认证用户，query string id='calc-1'，
      PricingUseCase.GetCalculation 返回合法 PriceCalculation
    when: "GET /pricing/calculations?id=calc-1"
    then: |
      - HTTP 200 OK
      - body 含 calculation_id/original_amount/discount_amount/tax_amount/final_amount/currency/status
    test_type: http

  - id: AC-35
    layer: handler
    context: pricing
    scenario: "GET /pricing/calculations?id=xxx — 不存在返回 404"
    given: |
      认证用户，PricingUseCase 返回 ErrCalculationNotFound
    when: "GET /pricing/calculations?id=nonexistent"
    then: "HTTP 404 Not Found"
    test_type: http

  - id: AC-36
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculations/cancel — 成功取消"
    given: |
      认证用户，body={ calculation_id:'calc-1' }，
      PricingUseCase.CancelCalculation 返回 Status=CANCELLED
    when: "POST /pricing/calculations/cancel"
    then: |
      - HTTP 200 OK
      - body 含 { status: 'CANCELLED' }
    test_type: http

  - id: AC-37
    layer: handler
    context: pricing
    scenario: "POST /pricing/calculations/cancel — 非 PENDING 状态返回 409"
    given: |
      认证用户，PricingUseCase.CancelCalculation 返回 ErrInvalidStateTransition
    when: "POST /pricing/calculations/cancel"
    then: "HTTP 409 Conflict"
    test_type: http

  - id: AC-38
    layer: handler
    context: coupon
    scenario: "POST /coupons 成功创建"
    given: |
      认证用户，body={
        code:'SUMMER20', discount_type:'PERCENTAGE', discount_value:2000,
        max_uses:50, valid_from:'2025-01-01T00:00:00Z', valid_until:'2025-12-31T23:59:59Z'
      }，CouponUseCase.Create 成功
    when: "POST /coupons"
    then: |
      - HTTP 201 Created
      - body 含 { coupon_id, code:'SUMMER20', status:'ACTIVE' }
    test_type: http

  - id: AC-39
    layer: handler
    context: coupon
    scenario: "POST /coupons — code 重复返回 409"
    given: |
      认证用户，CouponUseCase.Create 返回 ErrCouponCodeConflict
    when: "POST /coupons（code 与已存在的优惠券相同）"
    then: "HTTP 409 Conflict"
    test_type: http

  # ════════════════════════════════════════════════════════════════
  # AC-40 ~ AC-49: adapter 层
  # ════════════════════════════════════════════════════════════════

  - id: AC-40
    layer: adapter
    context: coupon
    scenario: "InMemoryCouponRepository — Save & FindByCode"
    given: "空仓储"
    when: |
      1. Save(coupon)
      2. FindByCode(coupon.Code)
    then: |
      - FindByCode 返回与 Save 相同的 Coupon（字段完全一致）
      - FindByCode 不存在的 code 返回 ErrCouponNotFound
    test_type: unit

  - id: AC-41
    layer: adapter
    context: coupon
    scenario: "InMemoryCouponRepository — FindByID 不存在"
    given: "空仓储"
    when: "FindByID('nonexistent')"
    then: "返回 ErrCouponNotFound"
    test_type: unit

  - id: AC-42
    layer: adapter
    context: coupon
    scenario: "InMemoryCouponRepository — 并发 Save 无 race condition"
    given: "空仓储"
    when: "50 个 goroutine 同时 Save 不同 Coupon"
    then: |
      - 无 data race（-race 检测通过）
      - 所有 Coupon 均可通过 FindByID 查到
    test_type: unit

  - id: AC-43
    layer: adapter
    context: coupon
    scenario: "InMemoryCouponRepository — Save 覆盖更新（同 ID 二次 Save）"
    given: "仓储中已存在 coupon（UsedCount=0）"
    when: "修改 UsedCount=1 后再次 Save 同一 coupon"
    then: "FindByID 返回 UsedCount=1 的最新版本"
    test_type: unit

  - id: AC-44
    layer: adapter
    context: pricing
    scenario: "InMemoryPriceCalculationRepository — Save & FindByID"
    given: "空仓储"
    when: |
      1. Save(pc)
      2. FindByID(pc.ID)
    then: |
      - 返回与 Save 相同的 PriceCalculation
      - FindByID 不存在的 ID 返回 ErrCalculationNotFound
    test_type: unit

  - id: AC-45
    layer: adapter
    context: pricing
    scenario: "InMemoryPriceCalculationRepository — 并发 Save 无 race condition"
    given: "空仓储"
    when: "50 个 goroutine 同时 Save 不同 PriceCalculation"
    then: |
      - 无 data race（-race 检测通过）
      - 所有记录均可通过 FindByID 查到
    test_type: unit

  - id: AC-46
    layer: adapter
    context: pricing
    scenario: "CouponAdapter（pricing→coupon ACL）— FindApplicableCoupon 翻译正确"
    given: |
      coupon 仓储中存有 Coupon{Code:'SAVE10', Status:ACTIVE,
        Rule:{Type:PERCENTAGE, Value:1000}, ValidUntil:now+7days}
    when: "CouponAdapter.FindApplicableCoupon(ctx, 'SAVE10')"
    then: |
      - 返回 CouponView{CouponID:非空, DiscountType:'PERCENTAGE', DiscountValue:1000}
      - 无错误
    test_type: unit

  - id: AC-47
    layer: adapter
    context: pricing
    scenario: "CouponAdapter — 查询不存在的 coupon code 返回错误"
    given: "coupon 仓储为空"
    when: "CouponAdapter.FindApplicableCoupon(ctx, 'NOTEXIST')"
    then: "返回非 nil error（ErrCouponNotFound 或封装版本）"
    test_type: unit

  - id: AC-48
    layer: adapter
    context: payment
    scenario: "PricingAdapter（payment→pricing ACL）— FindCalculation 翻译正确"
    given: |
      pricing 仓储中存有 PriceCalculation{
        ID:'calc-1', Status:PENDING,
        OriginalAmount:Money{10000,'USD'}, FinalAmount:Money{9450,'USD'}
      }
    when: "PricingAdapter.FindCalculation(ctx, 'calc-1')"
    then: |
      - 返回 PricingView{
          CalculationID:'calc-1', Status:'PENDING',
          OriginalAmount:10000, FinalAmount:9450, Currency:'USD'
        }
      - 无错误
    test_type: unit

  - id: AC-49
    layer: adapter
    context: payment
    scenario: "PricingAdapter — 查询不存在的 CalculationID 返回 ErrCalculationNotFound"
    given: "pricing 仓储为空"
    when: "PricingAdapter.FindCalculation(ctx, 'nonexistent')"
    then: "返回 port.ErrCalculationNotFound（payment 上下文定义的哨兵错误）"
    test_type: unit
```

---

## 覆盖率说明

下面是各验收标准对设计文档关键点的映射，便于人工核对：

| 设计关键点 | 覆盖的 AC |
|-----------|----------|
| `shared/money.MultiplyBasisPoint` 新方法 | AC-1, AC-2, AC-3 |
| `Coupon` 聚合根：工厂 / Apply / IsApplicable | AC-4, AC-5, AC-6, AC-7, AC-8, AC-9, AC-10 |
| `Coupon` 状态机 ACTIVE→EXHAUSTED（MaxUses 触发） | AC-6 |
| `PriceCalculation` 聚合根：工厂 / Confirm / Cancel | AC-11, AC-12, AC-13, AC-14, AC-15, AC-16 |
| `PriceCalculationService`：FIXED / PERCENTAGE / nil rule / 折扣超限 | AC-17, AC-18, AC-19 |
| Saga 正常路径（无券 / 有券）| AC-20, AC-21 |
| Saga 补偿路径（折扣超限 / Repo 失败） | AC-23, AC-24 |
| Saga TaxRateQuery 失败降级为税率 0 | AC-25 |
| ConfirmCalculation / CancelCalculation 用例 | AC-26, AC-27, AC-28 |
| payment.Purchase 携带 CalculationID 集成点 | AC-29 |
| pricing handler：201 / 400 / 401 / 404 / 409 / 422 | AC-30–AC-37 |
| coupon handler：201 / 409 | AC-38, AC-39 |
| InMemory 仓储 CRUD + 并发安全（coupon / pricing） | AC-40–AC-45 |
| ACL Adapter 翻译正确性（CouponAdapter / PricingAdapter） | AC-46, AC-47, AC-48, AC-49 |