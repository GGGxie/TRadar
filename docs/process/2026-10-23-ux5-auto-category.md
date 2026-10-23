# UX-5 采集项目自动归类方向：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #36](https://github.com/GGGxie/TRadar/issues/36)
- 状态：已完成，PR 待审阅（基于 #35 分支）
- 对应提交：本记录所在提交

## 1. 背景与边界

用户视角验收：采集得到的 113 个项目全部为「其他方向」，按 AI Agent 筛选 0 条，方向筛选对采集数据无效。

本次在采集首次创建项目时，按名称与描述（HN 为标题）的确定性规则归类；无法判断仍为 OTHER。
不覆盖已有项目或用户修改过的分类，不回填历史数据，不引入模型。

## 2. 采用的技能

| 技能 | 读取文件 | 应用 |
|---|---|---|
| `test-driven-development` | `engineering/agent-skills/skills/test-driven-development/SKILL.md` | 用真实采集标题/描述做表驱动用例，含词边界反例（agency、capital） |
| `incremental-implementation` | `engineering/agent-skills/skills/incremental-implementation/SKILL.md` | 先纯函数，再接入三个采集器 |

## 3. 规格到实现切片

| 验收 | 实现 | 证据 |
|---|---|---|
| 规则：整词/中文词组、AI Agent → 开发者工具 → SaaS、否则 OTHER | `internal/ingestion/category.go` | `TestClassifyCategoryUsesWholeWordsWithAgentFirst`（18 例） |
| 采集创建时归类；用户修改后再采集不覆盖 | GitHub / HN / PH 的 `CreateProject` 调用 | `TestGitHubClassifiesNewProjectsButKeepsUserCategory` |
| 规格 | SPEC-source-ingestion 归类说明 | 本提交 |

## 4. TDD 记录

- RED：`classifyCategory` 未定义；集成测试 `category="OTHER"`。
- GREEN 后用真实采集复核，发现「Eleven free Claude skills…」仍为 OTHER；先补「claude skills」「chatgpt」用例（RED），再加入 claude/chatgpt/openai/anthropic（GREEN）。

## 5. 问题、根因与修正

- 首次真实复核全部为 OTHER：服务进程比二进制写完早 3 秒启动（14:46:47 vs 14:46:50），运行的是旧程序。换新库重启后结果正常。
- 复核中 PostHog、beancount-io 被归为 AI Agent：描述里确实写了「agents」「MCP」，属于规则按优先级的预期结果，未调整。

## 6. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 通过 |
| 新空库真实采集（GitHub 30 + HN 28） | AI Agent 19、开发者工具 12、SaaS 5、其他 22；其他方向多为非软件类 HN 新闻 |

## 7. 产物与剩余限制

- 规则是关键词启发式，一个项目只归一类；同时涉及 agent 与 SaaS 时取 AI Agent。
- 已有数据库中的历史项目保持原分类，可在详情页「编辑信息」手动调整。
