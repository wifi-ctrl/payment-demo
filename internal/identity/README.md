# Identity 限界上下文

负责用户身份认证：通过 Access Token 验证用户会话，拦截过期和封禁用户。

## 核心概念

| 类型 | 名称 | 说明 |
|------|------|------|
| 聚合根 | `User` | 用户，包含 ID、外部平台账号、游戏 ID、状态（ACTIVE / BANNED） |
| 聚合根 | `Session` | 会话，包含 AccessToken 和过期时间 |
| 值对象 | `UserID` / `SessionID` | 强类型标识 |
| 值对象 | `UserStatus` | 用户状态枚举 |

## 端口依赖

| 端口 | 方法 | 用途 |
|------|------|------|
| `UserRepository` | `FindByID(ctx, UserID)` | 按 ID 查询用户 |
| `SessionRepository` | `FindByAccessToken(ctx, token)` | 按 Access Token 查询会话 |

当前实现：`adapter/persistence/` 下的内存仓储（`InMemoryUserRepository`、`InMemorySessionRepository`），内置了测试用种子数据。

## 目录结构

```
identity/
├── domain/
│   ├── model/       # User、Session 聚合根，领域错误
│   └── port/        # UserRepository、SessionRepository 接口
├── application/     # AuthUseCase — 认证用例编排
├── adapter/
│   └── persistence/ # 内存仓储实现
└── handler/
    └── middleware/   # AuthMiddleware（HTTP 驱动适配器）
```

## 认证流程

```
HTTP Request
  │  Authorization: Bearer <token>
  ▼
AuthMiddleware.Handle()
  │  提取 token
  ▼
AuthUseCase.Authenticate(token)
  │
  ├─ SessionRepository.FindByAccessToken(token)
  │    ├─ 未找到 → ErrInvalidToken (401)
  │    └─ 找到 → 检查 Session.IsExpired()
  │              └─ 已过期 → ErrSessionExpired (401)
  │
  ├─ UserRepository.FindByID(session.UserID)
  │    └─ 未找到 → ErrUserNotFound (401)
  │
  └─ User.IsBanned()
       ├─ 是 → ErrUserBanned (401)
       └─ 否 → 认证通过，将 UserID 写入 context
                 ▼
           next.ServeHTTP(w, r)
```

中间件通过 `shared/auth.WithUserID()` 向 context 注入 `UserID`（string 类型），下游上下文通过 `auth.UserIDFromContext()` 读取，不依赖 identity 的领域模型。
