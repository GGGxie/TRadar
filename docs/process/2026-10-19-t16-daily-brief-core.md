# T16 日报选择与版本：agent-skills 过程记录

- 记录日期：2026-10-19（Asia/Shanghai，补记 2026-10-16 的实施）
- 任务来源：`tasks/todo.md` 的 T16，`SPEC-daily-brief.md`
- 状态：已完成
- 对应提交：`c3d7a19 feat: freeze versioned daily briefs`

## 1. 背景与边界

T16 负责把“今日值得关注的变化”冻结成可回溯的日报版本。这个切片只实现选择、版本、
冻结输入、覆盖率和设置持久化；HTTP、定时执行、Markdown 和页面留给 T17，模型生成留给 T18。

关键数据边界是：日报只能依据截止时间前已经保存的数据；项目后来变化、来源重新关联或证据
修改，都不能重写旧日报。事实、推测和个人判断分别冻结，私密研究笔记不能进入日报输入。

## 2. 采用的技能

| 技能 | 采用原因 | 实际读取文件 |
|---|---|---|
| incremental-implementation | 日报跨数据库、选择器、仓库和设置，需按可验证切片推进 | `engineering/agent-skills/skills/incremental-implementation/SKILL.md` |
| test-driven-development | 选择条件、日期边界和不可变性都属于易回归行为 | `engineering/agent-skills/skills/test-driven-development/SKILL.md` |
| api-and-interface-design | 先稳定任务、版本、冻结输入和设置的数据契约 | `engineering/agent-skills/skills/api-and-interface-design/SKILL.md` |
| security-and-hardening | 冻结输入包含外部文本与私密研究数据，需要建立数据边界 | `engineering/agent-skills/skills/security-and-hardening/SKILL.md` |
| code-review-and-quality | 提交前检查正确性、并发、可维护性和规格覆盖 | `engineering/agent-skills/skills/code-review-and-quality/SKILL.md` |
| git-workflow-and-versioning | 在功能分支上形成可回滚的任务提交 | `engineering/agent-skills/skills/git-workflow-and-versioning/SKILL.md` |

## 3. 规格到实现切片

1. 建立迁移和领域模型，保存日报任务、日报版本、条目、证据快照、设置与调度幂等键。
2. 实现 `brief-selection-v1`，仅选择首次覆盖或存在新增证据、分析、研究判断、可比增长变化的项目。
3. 在接受任务的同一事务中冻结候选输入和选择理由，之后由处理器读取冻结值生成版本。
4. 增加同一时区自然日的版本递增、失败任务查询、设置版本冲突和 IANA 时区校验。
5. 用完整 race 测试与静态检查验证数据库事务、并发和跨模块兼容性。

## 4. TDD 记录

### RED

先写仓库与选择行为测试。初次运行因 `ProcessRun`、`GetRun`、`GetBrief` 等入口尚不存在而编译失败，
证明测试确实先于实现。随后用聚焦测试逐项暴露空摘要匹配、新旧证据差异和冻结输入校验问题。

### GREEN

实现迁移、模型、选择器、快照、仓库和设置后，聚焦测试通过。任务接受与输入冻结位于同一事务，
处理失败会保存可查询的失败状态；同一日期再次生成会创建新版本，不覆盖旧版。

### REFACTOR

冻结输入统一为有语义校验的 JSON 结构，选择理由集中计算，避免调用方自行拼装；查询始终从冻结表读取，
减少“后来数据污染旧版本”的入口。

## 5. 问题、根因与修正

### 空摘要被错误识别为变化

- 现象：空摘要可能通过反向包含判断，被当成有意义的项目变化。
- 根因：字符串包含关系对空字符串成立，但业务语义要求空摘要不参与变化判断。
- 修正：增加空值前置条件与回归测试。

### 证据消失被误报为新增证据

- 现象：来源重新关联导致旧证据不再出现时，集合不同被解释为 `NEW_EVIDENCE`。
- 根因：比较了集合是否相等，没有比较“当前集合是否新增元素”。
- 修正：改为集合增量判断；证据移除不再触发新变化，并新增来源重关联测试。

### 冻结输入只验证了 JSON 语法

- 现象：语法合法但缺少必需字段的 `{}` 可能进入处理阶段。
- 根因：数据库约束只能证明字符串是 JSON，不能证明它符合日报输入契约。
- 修正：保留迁移层 `json_valid`，并在应用层执行 `validateFrozenInput` 语义校验；损坏输入测试改用 `{}` 验证该边界。

## 6. 验证证据

实际完成并通过：

```sh
GOTOOLCHAIN=local GOCACHE=/tmp/tradar-go-cache go test -race ./internal/brief/...
GOTOOLCHAIN=local GOCACHE=/tmp/tradar-go-cache go test -race ./...
GOTOOLCHAIN=local GOCACHE=/tmp/tradar-go-cache go vet ./...
```

测试覆盖 DB-01..06、DB-08、DB-10，以及来源重关联、cutoff 后修改、失败可查询和语义损坏输入。

## 7. 产物与剩余限制

主要产物是 `internal/brief/`、对应 SQLite 迁移及 T16 测试。T16 完成时，日报尚不能经 HTTP 触发、
定时生成、导出或在浏览器阅读；这些限制由 T17 处理。可选模型仍由 T18 单独实现。
