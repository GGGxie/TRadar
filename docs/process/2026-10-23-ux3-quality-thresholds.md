# UX-3 采集质量门槛：agent-skills 过程记录

- 记录日期：2026-10-23（Asia/Shanghai）
- 任务来源：[GitHub Issue #34](https://github.com/GGGxie/TRadar/issues/34)
- 状态：已完成，已合并到 main（PR #40，2026-10-26）
- 对应提交：本记录所在提交

## 1. 背景与边界

用户视角验收：GitHub 44 个候选有 26 个不足 10 星（近期推送桶按更新排序且无门槛）；HN 69 条中 36 条不足 5 分。
本次为 GitHub 增加 `minStars`（默认 20）、HN 增加 `minPoints`（默认 10），设置页可编辑。

边界：门槛只拦截**新**候选；已入库或跟踪中的记录继续刷新，避免观测断档；HN 分数缺失不当作 0；
已有数据不删除；不改变预算、跟踪和历史名额规则。

## 2. 采用的技能

| 技能 | 读取文件 | 应用 |
|---|---|---|
| `test-driven-development` | `engineering/agent-skills/skills/test-driven-development/SKILL.md` | 先写默认值/校验、GitHub 查询、HN 跳过与刷新三个测试；用 stash 取得 RED 证据 |
| `api-and-interface-design` | `engineering/agent-skills/skills/api-and-interface-design/SKILL.md` | 只加可选字段；旧配置缺字段时读取取默认值，无需数据迁移；跨来源字段继续拒绝 |
| `frontend-ui-engineering` | `engineering/agent-skills/skills/frontend-ui-engineering/SKILL.md` | 说明文字放在 label 外并用 `aria-describedby` 关联，避免污染可访问名称 |

## 3. 规格到实现切片

| 验收 | 实现 | 证据 |
|---|---|---|
| 默认值与范围校验（GH 0–100,000、HN 0–1,000、跨来源拒绝） | `config.go` | `TestSourceConfigsDefaultQualityThresholds` |
| GitHub 两桶查询附加 `stars:>=N` | `github.go` | `TestGitHubDiscoveryQueriesRequireMinimumStars` |
| HN 新低分条目跳过且不记错误；已知条目继续刷新；分数缺失保留 | `hackernews.go` | `TestHackerNewsSkipsNewLowPointStoriesButRefreshesKnownOnes` |
| 设置页编辑两项门槛 | `SettingsPage.tsx` | `SettingsPage.test.tsx` 新增一例 |
| 规格 | SPEC-source-ingestion 来源表、配置类型、范围说明 | 本提交 |

## 4. TDD 记录

- RED（后端）：stash 实现后编译失败，`SourceConfigPatch` 无 `MinPoints`/`MinStars`。
- GREEN 后旧测试 `TestHackerNewsExecutorKeepsDiscussionIdentityAndSkipReasons` 失败：其 107/108 号夹具只有 3/4 分，被新门槛过滤。
  该测试关注身份与跳过原因，把两个分数改为 13/14 以保留原意。
- RED（界面）：stash `SettingsPage.tsx` 后新测试失败；恢复后 12 文件 32 测试通过。

## 5. 问题、根因与修正

- 首次真实验证结果「门槛无效」。根因不在代码：验证脚本并行执行，服务进程比 `go build` 写完二进制早 2 秒启动，运行的是旧程序
  （进程启动 11:20:27，二进制修改 11:20:29）。改为构建完成后再启动，并换用新的空库重测。

## 6. 验证证据

| 命令 / 检查 | 结果 |
|---|---|
| `go test -race ./...`、`go vet ./...`、`gofmt -l` | 通过 |
| `npx vitest --run`、`npm run build` | 12 文件 32 测试通过；构建通过 |
| `make e2e` | Chromium 7/7 通过 |
| 新空库真实采集 | GitHub 30 个候选最低 20 星（此前同配置 14/30 低于 20）；HN 读取 60、接受 29、跳过 31，最低 13 分，无错误条目 |
| 浏览器检查 | 设置页显示「最低 Stars（新候选）」「最低分数（新条目）」及说明，无横向溢出、无控制台错误 |

## 7. 产物与剩余限制

- 最近采集任务列表不显示跳过数，用户看不到「31 条因门槛跳过」；可在后续小问题中补充。
- 门槛为全局值，不能按关键词分别设置。
