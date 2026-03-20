# Catalog 限界上下文

管理游戏内可售商品的定义、价格和上下架状态。

## 核心概念

| 类型 | 名称 | 说明 |
|------|------|------|
| 聚合根 | `Product` | 商品，包含 ID、名称、价格、状态 |
| 值对象 | `Money` | 金额（Amount + Currency），复用 Shared Kernel |
| 值对象 | `ProductID` | 商品唯一标识（`string` 类型） |
| 值对象 | `ProductStatus` | 商品状态枚举 |

## 商品状态

| 状态 | 常量 | 含义 |
|------|------|------|
| `ACTIVE` | `ProductStatusActive` | 上架，可购买 |
| `OFFLINE` | `ProductStatusOffline` | 下架，不可购买 |

通过 `Product.IsActive()` 判断商品是否可售。

## 端口依赖

| 端口 | 接口 | 方法 |
|------|------|------|
| 商品仓储 | `ProductRepository` | `FindByID(ctx, id)` / `FindAll(ctx)` |

当前适配器实现：`InMemoryProductRepository`（内存存储，含预置种子数据）。

## 目录结构

```
catalog/
├── domain/
│   ├── model/       # Product 聚合根、Money 值对象、领域错误
│   └── port/        # ProductRepository 接口
├── application/     # CatalogUseCase（查询编排）
├── adapter/
│   └── persistence/ # 内存仓储实现
└── handler/
    └── http/        # GET /products 驱动适配器
```

## API 端点

| 方法 | 路径 | 用途 |
|------|------|------|
| `GET` | `/products` | 查询所有商品列表 |
| `GET` | `/products?id=<productID>` | 按 ID 查询单个商品 |
