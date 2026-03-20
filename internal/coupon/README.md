# Coupon 限界上下文

管理优惠券的创建、核销、回滚与过期。Order 上下文通过 `CouponApplier` ACL 端口调用本上下文完成折扣计算。

## 核心概念

| 类型 | 名称 | 说明 |
|------|------|------|
| 聚合根 | `Coupon` | 优惠券，承载状态流转与核销逻辑 |
| 值对象 | `CouponID` | UUID 主键 |
| 值对象 | `CouponCode` | 业务编码，如 `SAVE10`，唯一 |
| 值对象 | `DiscountRule` | 折扣规则（Type + Value） |
| 领域事件 | `CouponApplied` | 优惠券被成功核销时发布 |

## 状态机

```
ACTIVE ──Apply()达上限──▶ EXHAUSTED
  │                          │
  │ MarkExpired()            │ Rollback()
  ▼                          ▼
EXPIRED                   ACTIVE
```

| 当前状态 | 触发动作 | 目标状态 | 条件 |
|----------|----------|----------|------|
| ACTIVE | `Apply()` | EXHAUSTED | `UsedCount >= MaxUses`（MaxUses > 0） |
| ACTIVE | `Apply()` | ACTIVE | 未达上限，或 MaxUses=0（不限） |
| ACTIVE | `MarkExpired()` | EXPIRED | 定时任务或检查时调用 |
| EXHAUSTED | `Rollback()` | ACTIVE | 支付失败补偿，UsedCount-- |

## 折扣规则

| DiscountType | Value 含义 | 示例 |
|--------------|-----------|------|
| `PERCENTAGE` | 基点 (basis point)，10000 = 100% | `1000` = 10.00% |
| `FIXED` | 最小货币单位（如 cents） | `500` = $5.00 |

## 端口依赖

| 端口 | 接口 | 方法 |
|------|------|------|
| CouponRepository | `port.CouponRepository` | `Save`, `FindByID`, `FindByCode` |

当前提供 `adapter/inmem` 内存实现。

## 目录结构

```
coupon/
├── domain/
│   ├── model/       # Coupon 聚合根、值对象、领域错误
│   ├── event/       # CouponApplied 事件
│   └── port/        # CouponRepository 接口
├── application/     # CouponUseCase（创建、按编码查询）
├── adapter/
│   └── inmem/       # 内存仓储实现
└── handler/
    └── http/        # HTTP 驱动适配器（POST/GET /coupons）
```

## API 端点

| 方法 | 路径 | 用途 |
|------|------|------|
| `POST` | `/coupons` | 创建优惠券 |
| `GET` | `/coupons?code=<code>` | 按编码查询优惠券 |
