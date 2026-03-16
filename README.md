# Payment Demo — 六边形架构支付系统

## 项目结构

```
payment-demo/
├── main.go                                         # Composition Root（依赖注入）
│
└── internal/
    ├── domain/                                      # 领域层 ── 零外部依赖
    │   ├── model/                                   # 聚合根、值对象、错误
    │   │   ├── transaction.go                       #   PaymentTransaction 聚合根 + 状态机
    │   │   ├── money.go                             #   Money 值对象
    │   │   ├── card_token.go                        #   CardToken 值对象
    │   │   ├── product.go                           #   Product（Catalog ACL 视图）
    │   │   └── errors.go                            #   领域错误
    │   ├── port/                                    # 被驱动端口（接口定义）
    │   │   ├── gateway.go                           #   PaymentGateway
    │   │   ├── repository.go                        #   TransactionRepository
    │   │   └── catalog.go                           #   CatalogQuery
    │   ├── event/                                   # 领域事件
    │   │   └── event.go                             #   Authorized / Captured / Refunded
    │   └── service/                                 # 领域服务（纯业务规则）
    │       └── validation.go                        #   ValidateCapturable / ValidateRefundable
    │
    ├── application/                                 # 用例编排层
    │   └── usecase/
    │       └── charge_usecase.go                    #   Purchase / Capture / Refund 流程
    │
    ├── adapter/                                     # 被驱动适配器（按职责分包）
    │   ├── gateway/                                 #   支付网关 ACL
    │   │   └── mock_gateway.go                      #     MockPaymentGateway
    │   ├── persistence/                             #   持久化
    │   │   └── memory_repo.go                       #     InMemoryTransactionRepository
    │   └── catalog/                                 #   商品目录查询
    │       └── memory_catalog.go                    #     InMemoryCatalogQuery
    │
    ├── handler/                                     # 驱动适配器（按职责分包）
    │   ├── http/                                    #   HTTP 路由 + DTO
    │   │   └── payment_handler.go
    │   └── middleware/                              #   中间件
    │       └── auth.go                              #     认证
    │
    └── infra/                                       # 基础设施
        ├── config/                                  #   环境配置
        │   └── config.go
        └── database/                                #   数据库连接
            └── postgres.go
```

## 调用关系总览

```
外部请求         中间件              驱动适配器            用例编排              领域层              被驱动适配器
              (Middleware)      (Driving Adapter)    (Application)        (Domain)          (Driven Adapter)

             ┌────────────┐   ┌──────────────┐   ┌────────────────┐   ┌──────────────┐
curl /charge │            │   │              │   │                │   │ Payment      │
────────────▶│ Auth       │──▶│  Payment     │──▶│  ChargeUseCase │──▶│ Transaction  │
             │ Middleware │   │  Handler     │   │                │   │ (聚合根)      │
             │            │   │              │   │  .Purchase()   │   │ .MarkAutho() │
             │ Token→     │   │ DTO→领域模型  │   │  .Capture()    │   │ .MarkCapt()  │
             │ userID→ctx │   │              │   │  .Refund()     │   │ .MarkRefund()│
             └────────────┘   └──────────────┘   │                │   └──────────────┘
                                                  │                │
                                                  │                │   ┌──────────────┐   ┌────────────────┐
                                                  │                │──▶│ CatalogQuery │◀──│ InMemory       │
                                                  │                │   │ (端口)        │   │ CatalogQuery   │
                                                  │                │   └──────────────┘   └────────────────┘
                                                  │                │
                                                  │                │   ┌──────────────┐   ┌────────────────┐
                                                  │                │──▶│ PaymentGate- │◀──│ MockPayment    │
                                                  │                │   │ way (端口)    │   │ Gateway (ACL)  │
                                                  │                │   └──────────────┘   └────────────────┘
                                                  │                │
                                                  │                │   ┌──────────────┐   ┌────────────────┐
                                                  │                │──▶│ Transaction  │◀──│ InMemory       │
                                                  │                │   │ Repo (端口)   │   │ Repository     │
                                                  └────────────────┘   └──────────────┘   └────────────────┘
```

## 一次完整的购买请求调用链

```
1. curl -X POST /charge -H "Authorization: Bearer user_123"
   -d '{"product_id":"gem_100","token_id":"tok_visa_4242"}'
        │
        ▼
2. internal/handler/middleware/auth.go — Auth()
   ├── 提取 Authorization header
   ├── 验证 Token → 获取 userID
   └── 将 userID 注入 context，传递给下一层
        │
        ▼
3. internal/handler/http/payment_handler.go — handleCharge()
   ├── 解析 JSON → PurchaseRequest (DTO)
   ├── 从 context 取出 userID
   └── DTO → 用例入参，调用 useCase.Purchase()
        │
        ▼
4. internal/application/usecase/charge_usecase.go — Purchase()
   ├── 调用端口: catalog.FindProduct("gem_100")  ────────────┐
   │                                                          ▼
   │   5. internal/adapter/catalog/memory_catalog.go
   │      └── 返回 Product{Price: 999 USD, IsActive: true}
   │◀─────────────────────────────────────────────────────────┘
   │
   ├── 校验: product.IsActive == true ✓
   ├── 用服务端价格创建聚合根（不信任客户端金额）
   │
   ├── 调用端口: gateway.Authorize(token, 999 USD)  ──────────┐
   │                                                           ▼
   │   6. internal/adapter/gateway/mock_gateway.go
   │      ├── ACL 出站翻译: Money → cents, CardToken → provider token
   │      ├── 调用外部系统（这里是模拟）
   │      └── ACL 入站翻译: 外部响应 → GatewayAuthResult
   │◀──────────────────────────────────────────────────────────┘
   │
   ├── 调用聚合根: txn.MarkAuthorized(providerRef, authCode)
   │        │
   │        ▼
   │   7. internal/domain/model/transaction.go — MarkAuthorized()
   │      ├── 校验状态: CREATED → AUTHORIZED ✓（业务规则）
   │      └── 产生领域事件: PaymentAuthorized
   │
   ├── 调用端口: repo.Save(txn)  ──────────────────────────────┐
   │                                                            ▼
   │   8. internal/adapter/persistence/memory_repo.go
   │      └── map[txnID] = txn
   │◀───────────────────────────────────────────────────────────┘
   │
   └── 发布领域事件 (log 输出)
        │
        ▼
9. handler 收到 txn → 转成 TransactionResponse (DTO) → 返回 JSON
```

## 各层职责与依赖规则

```
┌──────────────────────────────────────────────────────────────────┐
│                           main.go                                │
│                  (Composition Root / 依赖注入)                     │
│  config.Load() → 创建 adapter → 注入 usecase → 注入 handler      │
│  → 包裹 middleware → 启动 HTTP Server                             │
└──┬──────────┬──────────┬──────────┬──────────┬───────────────────┘
   │          │          │          │          │
   ▼          ▼          ▼          ▼          ▼
┌──────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌──────┐
│infra │ │handler │ │  app   │ │adapter │ │middle│
│      │ │  /http │ │/usecase│ │/gateway│ │ware  │
│config│ │        │ │        │ │/persis │ │      │
│  db  │ │Payment │ │Charge  │ │/catalog│ │ Auth │
│      │ │Handler │ │UseCase │ │        │ │      │
└──────┘ └───┬────┘ └───┬────┘ └───┬────┘ └──┬───┘
             │          │          │          │
             └────┐     │     ┌────┘     ┌────┘
                  ▼     ▼     ▼          ▼
             ┌──────────────────────────────────┐
             │           domain                 │
             │                                  │
             │  model/     聚合根、值对象、错误     │
             │  port/      端口接口               │
             │  service/   领域服务（纯规则）       │
             │  event/     领域事件               │
             └──────────────────────────────────┘
```

### 依赖方向（从外到内，单向依赖）

| 层 | 包路径 | 知道谁 | 职责 |
|---|---|---|---|
| **middleware** | `handler/middleware` | `domain/model`（取 context） | 认证、日志等横切关注点 |
| **handler** | `handler/http` | `application/usecase`, `domain/model` | HTTP 解析 ↔ DTO 转换 |
| **application** | `application/usecase` | `domain/*` | 流程编排：商品验证 → 授权 → 持久化 |
| **domain** | `domain/model,port,event,service` | **无外部依赖** | 业务规则、状态机、端口接口定义 |
| **adapter** | `adapter/gateway,persistence,catalog` | `domain/model`, `domain/port` | 实现端口接口（ACL 翻译） |
| **infra** | `infra/config,database` | 标准库 | DB 连接、环境配置 |
| **main.go** | — | 所有包 | 组装依赖，启动服务 |

### 核心原则

- **domain 不 import 任何业务包** — 它定义接口（端口），不关心谁来实现
- **adapter 实现 domain 的接口** — 依赖方向是 adapter → domain，不是反过来
- **application 只依赖 domain** — 通过端口接口调用 adapter，不直接引用 adapter 包
- **handler 只依赖 application + domain** — 不直接引用 adapter
- **main.go 是唯一知道所有包的地方** — 在这里把 adapter 注入到 usecase

### 扩展示例

| 需求 | 改动位置 | 不影响 |
|---|---|---|
| 接入 Stripe 真实网关 | 新增 `adapter/gateway/stripe_gateway.go` | domain, application, handler |
| 换 PostgreSQL 持久化 | 新增 `adapter/persistence/postgres_repo.go` | domain, application, handler |
| 加 gRPC 入口 | 新增 `handler/grpc/` | domain, application, adapter |
| 加日志中间件 | 新增 `handler/middleware/logging.go` | domain, application, adapter |
| 接入真实商品服务 | 新增 `adapter/catalog/api_catalog.go` | domain, application, handler |

## 状态机

```
  ┌─────────┐     Authorize      ┌────────────┐     Capture      ┌──────────┐
  │ CREATED │───────────────────▶│ AUTHORIZED │──────────────────▶│ CAPTURED │
  └────┬────┘                    └────────────┘                   └─────┬────┘
       │                                                                │
       │ (网关拒绝)                                                      │ Refund
       ▼                                                                ▼
  ┌─────────┐                                                    ┌──────────┐
  │ FAILED  │                                                    │ REFUNDED │
  └─────────┘                                                    └──────────┘
```

## 快速运行

```bash
cd example/payment-demo
go run main.go

# 购买商品（需要 Authorization header）
curl -X POST localhost:8080/charge \
  -H "Authorization: Bearer user_123" \
  -d '{"product_id":"gem_100","token_id":"tok_visa_4242","last4":"4242","brand":"Visa"}'

# 扣款（替换 {id} 为上一步返回的 id）
curl -X POST localhost:8080/capture/{id} \
  -H "Authorization: Bearer user_123"

# 查询
curl localhost:8080/transaction/{id} \
  -H "Authorization: Bearer user_123"

# 退款
curl -X POST localhost:8080/refund/{id} \
  -H "Authorization: Bearer user_123"

# 测试：未认证
curl -X POST localhost:8080/charge -d '{}'
# → {"error":"missing authorization token"}

# 测试：商品不存在
curl -X POST localhost:8080/charge \
  -H "Authorization: Bearer user_123" \
  -d '{"product_id":"xxx"}'
# → {"error":"product not found"}

# 测试：商品已下架
curl -X POST localhost:8080/charge \
  -H "Authorization: Bearer user_123" \
  -d '{"product_id":"gem_old","token_id":"tok_visa"}'
# → {"error":"product is not active"}

# 测试：卡被拒
curl -X POST localhost:8080/charge \
  -H "Authorization: Bearer user_123" \
  -d '{"product_id":"gem_100","token_id":"tok_decline_funds"}'
# → {"error":"authorization declined by gateway"}
```
