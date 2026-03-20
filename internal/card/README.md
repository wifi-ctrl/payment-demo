# Card 限界上下文

管理用户银行卡的令牌化存储、加密保护与生命周期，提供 PCI 合规的 Card Vault 能力。

## 核心概念

| 类型 | 名称 | 说明 |
|------|------|------|
| 聚合根 | `SavedCard` | 已保存卡，包含加密 PAN、脱敏信息、渠道令牌、状态 |
| 子实体 | `ChannelToken` | 渠道复购令牌（一卡一渠道一令牌） |
| 值对象 | `EncryptedPAN` | AES-256-GCM 密文 + DEK 版本号 |
| 值对象 | `PANHash` | HMAC-SHA-256 哈希，用于查重 |
| 值对象 | `CardMask` | 脱敏展示信息（last4 / brand / 有效期） |
| 值对象 | `CardHolder` | 持卡人姓名 + 账单国家 |
| 值对象 | `RawCardData` | 原始卡数据，仅内存短暂存在，`String()` 返回 `[REDACTED]` |

### 领域事件

| 事件 | 触发时机 |
|------|---------|
| `CardBound` | 绑卡成功 |
| `CardSuspended` | 卡挂起 |
| `CardActivated` | 卡恢复激活 |
| `CardDeleted` | 卡删除（吊销所有渠道令牌） |
| `DefaultCardChanged` | 默认卡变更 |
| `ChannelTokenStored` | 存储渠道复购令牌 |
| `ChannelTokenRevoked` | 吊销渠道令牌 |
| `PANDecrypted` | PAN 被解密（PCI Req 10 审计） |

## 端口依赖

本上下文不依赖其他限界上下文的端口，所有端口均为自身定义的被驱动端口：

| 端口 | 职责 | Demo 适配器 |
|------|------|------------|
| `CardRepository` | 卡持久化 | `persistence/InMemoryCardRepository` |
| `CardVault` | 临时令牌缓存（`ct_*` 的存取消费） | `vault/LocalVault` |
| `Encrypter` | AES-256-GCM 加解密 + HMAC | `crypto/AESEncrypter` |
| `KeyManager` | DEK 生命周期管理（获取/轮换/退役） | `keymanager/InMemKeyManager` |

## 目录结构

```
card/
├── domain/
│   ├── model/       # SavedCard 聚合根、值对象、错误定义
│   ├── port/        # 被驱动端口接口 + DTO（TokenizeResult, CachedCardData）
│   ├── event/       # 领域事件
│   └── service/     # EncryptionService（加解密编排）、Luhn 校验、品牌识别
├── application/     # CardUseCase（令牌化/绑卡/卡管理）、KeyRotationUseCase（密钥轮换）
├── adapter/
│   ├── crypto/      # AES-256-GCM 实现
│   ├── persistence/ # 内存仓储
│   ├── vault/       # 本地内存 Vault
│   └── keymanager/  # 内存密钥管理器
└── handler/http/    # HTTP 驱动适配器 + 限流中间件
```

## API 端点

| 方法 | 路径 | 用途 |
|------|------|------|
| `POST` | `/cards/tokenize` | 令牌化：收集卡信息 → 加密 → 返回临时 `ct_*` token（per-user 限流 10 次/分钟） |
| `GET` | `/cards` | 查询当前用户所有已保存卡 |
| `GET` | `/cards?id=<cardID>` | 按 ID 查询单张卡详情 |
| `DELETE` | `/cards?id=<cardID>` | 删除卡（终态，吊销所有渠道令牌） |
| `POST` | `/cards/suspend` | 挂起卡 |
| `POST` | `/cards/activate` | 恢复激活卡 |
| `POST` | `/cards/default` | 设为默认卡 |

> 绑卡（`BindCardFromToken`）不暴露 HTTP 端点 — 仅在支付 Capture 成功后由 Payment 内部调用。

## 状态机

```
            Suspend()           Delete()
  ACTIVE ──────────► SUSPENDED ──────────► DELETED
    ▲                    │
    │    Activate()      │
    └────────────────────┘
    │                                        ▲
    │              Delete()                  │
    └────────────────────────────────────────┘
```

- `ACTIVE` → `SUSPENDED`：挂起，卡不可用于支付
- `SUSPENDED` → `ACTIVE`：恢复激活
- `ACTIVE` / `SUSPENDED` → `DELETED`：终态不可逆，同时吊销所有渠道令牌
- `ChannelToken` 有独立状态：`active` / `revoked`

## 安全设计要点（PCI 相关）

| PCI 要求 | 实现方式 |
|----------|---------|
| Req 3 — 存储保护 | PAN 使用 AES-256-GCM 加密存储（`EncryptedPAN`），明文仅在内存短暂存在 |
| Req 3 — 密钥轮换 | `KeyRotationUseCase.RotateAndMigrate` 支持在线 DEK 轮换 + 全量重加密迁移 |
| Req 3 — 密钥销毁 | 退役 DEK 先清零再删除（`RetireDEK` 中 `entry.dek[i] = 0`） |
| Req 7/8 — 审计追溯 | 密钥轮换强制要求 `operatorID`，拒绝匿名操作 |
| Req 8 — 防暴力枚举 | `/cards/tokenize` 接口 per-user 限流（10 次/分钟） |
| Req 10 — 审计日志 | 每次 PAN 解密产生 `PANDecrypted` 事件，含原因和时间戳 |
| 令牌化 | 原始卡数据收集后立即加密，返回临时 `ct_*` 令牌（15 分钟过期，一次性消费） |
| 查重 | 使用 `PANHash`（HMAC-SHA-256）查重，避免明文比对 |
| 防泄露 | `RawCardData.String()` 返回 `[REDACTED]`，防止日志意外打印 PAN/CVV |
| 绑卡隔离 | 绑卡不暴露 HTTP 端点，仅在支付成功后由 charge 上下文内部调用 |
