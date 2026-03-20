# Acquiring 上下文

收单上下文，合并了原 `merchant/` 和 `payment/` 两个限界上下文。

## 职责

1. **商户管理** — 注册、暂停商户；添加/吊销渠道凭据（CARD / PAYPAL）
2. **支付执行** — 按给定金额扣款（不决定金额本身，由 Order 定价后传入）
3. **网关路由** — 根据 MerchantID + PaymentMethod 查凭据 → GatewayFactory 动态构建 Gateway
4. **Capture 后绑卡** — 支付成功后通知 Card 上下文存储已验证卡和 ChannelToken

## 合并原因

在 Demo 规模下，`merchant` 和 `payment` 的交互方式呈现 **Shared Kernel** 特征而非 ACL 隔离：
- `PaymentChannel`（merchant）与 `PaymentMethod`（payment）实质相同，统一为 `PaymentMethod`
- `ChargeUseCase` 每笔交易都查 `Merchant.ActiveCredential`，合并后省去 ACL 翻译层
- 两者的生命周期、变更频率、部署单元完全一致

## 聚合根

| 聚合根 | 状态机 | 核心不变式 |
|---|---|---|
| `Merchant` | ACTIVE → SUSPENDED | 同一渠道最多一个 ACTIVE 凭据 |
| `PaymentTransaction` | CREATED → AUTHORIZED → CAPTURED → REFUNDED / FAILED | 只有合法前序状态才可流转 |

## 目录结构

```
internal/acquiring/
├── domain/
│   ├── model/
│   │   ├── merchant.go              Merchant 聚合根 + ChannelCredential 实体
│   │   ├── transaction.go           PaymentTransaction 聚合根
│   │   ├── payment_method.go        PaymentMethod 枚举（CARD / PAYPAL）— 统一类型
│   │   ├── card_token.go            CardToken 值对象
│   │   ├── paypal_token.go          PayPalToken 值对象
│   │   ├── money.go                 Money type alias
│   │   └── errors.go                合并后的领域错误
│   ├── port/
│   │   ├── merchant_repository.go   MerchantRepository 端口
│   │   ├── transaction_repository.go TransactionRepository 端口
│   │   ├── gateway.go               PaymentGateway + GatewayFactory + ChannelCredentialView
│   │   ├── paypal_gateway.go        PayPalGateway 端口
│   │   └── card.go                  CardQuery / CardCommand ACL 端口（调用 card 上下文）
│   └── event/
│       ├── merchant_event.go        Merchant 领域事件
│       └── payment_event.go         Payment 领域事件
├── application/
│   ├── charge_usecase.go            ChargeUseCase（含 findActiveCredential 内部方法）
│   └── merchant_usecase.go          MerchantUseCase
├── adapter/
│   ├── persistence/
│   │   ├── merchant_memory_repo.go  InMemoryMerchantRepository
│   │   └── transaction_memory_repo.go InMemoryTransactionRepository
│   ├── gateway/
│   │   ├── factory.go               MultiChannelGatewayFactory
│   │   ├── stripe/                  Stripe 网关适配器 + HTTP Client + Mock
│   │   └── paypal/                  PayPal 网关适配器 + HTTP Client + Mock
│   └── card/
│       ├── card_adapter.go          CardQuery ACL（读取 card 上下文）
│       └── card_command_adapter.go  CardCommand ACL（写入 card 上下文）
└── handler/http/
    ├── merchant_handler.go          商户管理 HTTP 端点
    └── payment_handler.go           内部交易查询 + Webhook 端点
```

## 调用关系

```
Order ──(ACL)──▶ Acquiring.ChargeUseCase ──▶ MerchantRepository (内部)
                         │                        ──▶ GatewayFactory → Stripe/PayPal
                         │                        ──▶ TransactionRepository (内部)
                         └──(ACL)──▶ Card (CardQuery + CardCommand)
```

## API 端点

### 商户管理
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/merchants` | 注册商户 |
| GET | `/merchants` | 商户列表 |
| GET | `/merchants?id=xxx` | 商户详情 |
| POST | `/merchants/credentials` | 添加渠道凭据 |
| DELETE | `/merchants/credentials` | 吊销渠道凭据 |
| POST | `/merchants/suspend` | 暂停商户 |

### 支付（内部端点）
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/internal/transaction?id=xxx` | 查询交易（不对外暴露） |
| POST | `/webhooks/recurring-token` | 渠道异步回调 |
