# 检查点 D：agent-skills 过程记录

- 记录日期：2026-10-19（Asia/Shanghai）
- 任务来源：`tasks/todo.md` 的检查点 D
- 状态：已完成
- 实现与验收提交：本记录所在的检查点 D 提交

## 1. 背景与完成声明

检查点 D 不是再跑一次测试就打勾，而是把六份模块规格的每个验收编号与实际证据对应起来，查找被
任务汇总或全量绿色结果掩盖的缺口。审计对象共 58 项：CAT 8、ING 12、TREND 10、AN 9、RW 9、
DB 10。证据矩阵写入 `docs/acceptance-matrix.md`。

## 2. 采用的技能

| 技能 | 采用原因 | 实际依据 |
|---|---|---|
| code-review-and-quality | 不用“全量测试绿色”替代逐项契约核对；沿正确性、安全、架构和可维护性检查证据是否真的支持声明 | T19 已读取并应用的 `engineering/agent-skills/skills/code-review-and-quality/SKILL.md` |
| documentation-and-adrs | 把验收编号、证据、限制写成未来人员和 agent 可复核的长期文档 | T20 已读取的 `engineering/agent-skills/skills/documentation-and-adrs/SKILL.md` |
| shipping-and-launch | 最终声明同时核对构建、健康检查、配置、备份/恢复、安全与回滚，不把本地首版误述为公网生产系统 | T20 已读取的 `engineering/agent-skills/skills/shipping-and-launch/SKILL.md` |

## 3. 核对方法

1. 从六份 `SPEC-*.md` 直接提取 ID，不从 `tasks/todo.md` 反推，防止遗漏被任务摘要省略的条件。
2. 对每个 ID 找到最接近失败模式的领域测试、HTTP 集成测试、组件测试或 Chromium 流程。
3. 并发/幂等、同源、安全文本等共享机制允许“领域 replay/mismatch + 共享机制并发/安全测试”组成证据链，
   但在矩阵中明确列出，不把间接证据写成领域专用测试。
4. 外部真实调用、付费服务和公网设施单独标限制；fixture 通过不等于真实 Token 联调。
5. 找不到证据的条件先修正测试或契约，再重新运行相关门禁。

## 4. 审计发现与处理

### 发现 1：RW-08 的演示措辞与最终首版范围不一致

规格仍写“演示模式全局可见”，但交付决策是首版不提供演示模式，默认空库且不允许样例数据静默回退。
这不是实现失败，而是 T20 后没有把规格同步到最终范围。修订 RW-08 为“首版无演示模式”；保留未来约束：
若引入演示模式，必须全局可见。README、共享规格和验证记录使用相同表述。

### 发现 2：DB-08 对 STALE 覆盖只有实现，没有直接测试断言

现有冻结测试同时证明 FOUNDER_CLAIM 标签、AVAILABLE 和 NO_DATA，却没有构造超过 24 小时的来源。
在同一测试加入 25 小时前的 Hacker News source，明确断言 coverage 顺序为 AVAILABLE、STALE、NO_DATA。
这没有修改生产逻辑，只补足验收证据；聚焦测试通过后再运行全包 race。

## 5. 验证证据

验收 ID 机器核对结果：规格为 58 个唯一 ID，矩阵为 58 个唯一 ID，`comm` 无差集。DB-08 聚焦测试
通过后执行 Go 1.26.9 全包 race，全部包通过；本次未修改前端或生产 Go 逻辑，因此沿用紧邻 T20 且
产物未改变的 `make verify` 结果，不重复运行 Vitest、Vite 和 Chromium。

矩阵的上层门禁仍包括：T19 的二进制漏洞扫描、模块/签名完整性和信任边界审查；T20 的隔离安装、
构建、空库启动、health、SQLite quick check、备份/恢复及整体 `make verify`。

最终文档检查 `git diff --check` 通过，秘密模式搜索无命中。任务总表只在上述差异清零后改为已完成。

## 6. 完成判断

六模块 58/58 条目都有可定位证据，发现的契约漂移和直接测试缺口均已处理。首版可声明完成的是本地
单用户交付范围；矩阵明确保留真实 Product Hunt、付费模型、公网 TLS/账号/多租户未验证的边界。
