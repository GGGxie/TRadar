# 实施验证记录

更新时间：2026-10-14 16:15（Asia/Shanghai）。这里只记录实际执行过的检查；未列模块仍按
[`tasks/todo.md`](../tasks/todo.md) 视为未完成。

## 已验证范围

| 范围 | 证据 |
|---|---|
| SQLite 初始化、迁移、事务和重启 | 临时真实数据库测试覆盖迁移、回滚、新版拒绝及关闭后重开；HTTP 重启测试验证项目仍可查询 |
| 项目身份 | URL 保守归一化、追踪参数移除、同名不同 URL 隔离、并发去重、稳定分页和中文搜索 |
| 项目 API | JSON 形状/大小、Host/Origin、同源写入、幂等重放、payload 不匹配、ETag 与旧版本冲突 |
| 项目界面 | 空状态、创建、列表、详情、编辑、刷新、搜索以及 360px 手机布局 |
| 证据 | 私密默认、URL 归一化、不可变修订、历史截止读取、公开过滤、跨项目引用拒绝、人工指标伪造拒绝 |
| 证据界面 | 新增、失败后草稿与重试 key 保留、修订保留旧版、原始来源外链及无障碍扫描 |

## 最近一次完整检查

以下命令在当前分支实际成功执行：

```sh
GOCACHE=/tmp/tradar-go-cache go test -race ./...
GOCACHE=/tmp/tradar-go-cache go vet ./...
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
npm --prefix web audit --json
```

结果：Go 全部包通过 race 测试及 vet；Vitest 为 2 个文件、4 个测试通过；Vite 生产构建成功；
Playwright Chromium 为 2 条流程通过。浏览器流程覆盖项目创建、证据新增和修订、项目编辑、刷新、
搜索及移动端布局，页面错误为零，所检查页面 axe 违规为零。npm 完整依赖审计为 0 条已知漏洞。

浏览器审阅截图：

- [`catalog-desktop.png`](screenshots/catalog-desktop.png)
- [`catalog-mobile.png`](screenshots/catalog-mobile.png)
- [`evidence-desktop.png`](screenshots/evidence-desktop.png)

## 当前限制

真实 GitHub/Hacker News/Product Hunt 来源、来源记录与观测 API、采集调度、增长排序、分析、
研究记录、日报、演示模式和可选模型尚未实现。当前运行不会访问人工录入的 URL，也不会静默生成
演示指标或商业结论。公网发布、付费服务与外部通知不在当前授权范围内。
