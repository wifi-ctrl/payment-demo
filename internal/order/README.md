# Order 限界上下文

## 职责

管理订单生命周期：锁定商品价格 + 优惠券 + 税费 → 计算 FinalAmount，编排支付授权/扣款/退款流程。

## 核心概念

### 聚合根

| 聚合根 | 说明 |
|--------|------|
| `Order` | 订单聚合根，持有 `PriceBreakdown`、关联 Acquiring 交易 ID，封装所有状态流转方法 |

### 值对象

| 值对象 | 说明 |
|--------|------|
| `Money` | 金额（Amount + Currency），引用 `shared/money` |
| `PriceBreakdown` | 定价明细：原价、折扣、税费、最终金额 |
| `OrderID` | 订单唯一标识（UUID） |
| `OrderStatus` | 订单状态枚举 |

### 领域事件

| 事件 | 触发时机 |
|------|----------|
| `order.created` | 订单创建时 |
| `order.paid` | 扣款成功（AUTHORIZED → PAID） |
| `order.refunded` | 退款成功（PAID → REFUNDED） |

### 领域服务

| 服务 | 说明 |
|------|------|
| `CalculateFinalAmount` | 纯计算函数：原价 - 折扣 + 税 |

## 端口依赖

| 端口 | 对应上下文 | 说明 |
|------|-----------|------|
| `OrderRepository` | 自身 | 订单持久化 |
| `CatalogQuery` | Catalog | 查询商品信息（价格、是否上架） |
| `CouponApplier` | Coupon | 应用/回滚优惠券 |
| `TaxRateQuery` | Tax | 查询税率（basis point） |
| `PaymentCommand` | Acquiring | 支付授权（Charge）、扣款（Capture）、退款（Refund） |

## 目录结构

```
order/
├── domain/
│   ├── model/          # Order 聚合根、Money、PriceBreakdown、错误定义
│   ├── port/           # 端口接口 + DTO（ProductView、ChargeRequest 等）
│   ├── event/          # 领域事件（OrderCreated、OrderPaid、OrderRefunded）
│   └── service/        # 定价计算（CalculateFinalAmount）
├── application/        # OrderUseCase：编排定价 → 创建订单 → 支付
├── adapter/
│   ├── persistence/    # 内存仓储实现
│   ├── catalog/        # CatalogQuery 适配器
│   ├── coupon/         # CouponApplier 适配器
│   ├── tax/            # TaxRateQuery 适配器
│   └── payment/        # PaymentCommand 适配器
└── handler/
    └── http/           # HTTP 驱动适配器（REST API）
```

## API 端点

| 方法 | 路径 | 用途 |
|------|------|------|
| `POST` | `/orders` | 创建订单（编排定价 → 创建 Order → 调 Acquiring 授权） |
| `GET` | `/orders?id=xxx` | 查询订单详情 |
| `POST` | `/orders/capture` | 扣款 `{"order_id":"xxx"}`（调 Acquiring Capture → 绑卡） |
| `POST` | `/orders/refund` | 退款 `{"order_id":"xxx"}`（调 Acquiring Refund） |

## 状态机

```
PENDING_PAYMENT ──授权成功──→ AUTHORIZED ──扣款成功──→ PAID ──退款成功──→ REFUNDED
       │
       └──────授权失败──→ FAILED
```

| 当前状态 | 方法 | 目标状态 | 触发事件 |
|---------|------|---------|---------|
| `PENDING_PAYMENT` | `MarkAuthorized()` | `AUTHORIZED` | - |
| `PENDING_PAYMENT` | `MarkFailed()` | `FAILED` | - |
| `AUTHORIZED` | `MarkPaid()` | `PAID` | `order.paid` |
| `PAID` | `MarkRefunded()` | `REFUNDED` | `order.refunded` |
