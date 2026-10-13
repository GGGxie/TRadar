# SPEC — project-analysis

状态：**详细规格草案，待用户评审**。日期：2026-10-13。尚未实现或运行测试。
范围依据：[能力图](CAPABILITY-MAP.md)；共享技术、运行、安全、API 与测试约定见
[spec-common](docs/spec-common.md)。本文只定义规格，不包含实施计划。

## 目标、依赖与非目标

让个人全栈工程师用约 3 分钟完成初筛，用约 30 分钟完成六维研究，并明确已知事实、
推测与下一步需要验证的问题。时间是人的研究预算，不是模型执行时长。

依赖 `project-catalog` 的项目、不可变证据及观测，依赖 `trend-ranking` 的可解释结果；
不依赖研究笔记或日报。拥有分析版本、分析内声明及生成任务，不拥有事实源数据。
无模型凭据也可使用本地规则模板、手工填写与完整版本历史。

非目标：替用户作投资判断、自动验证收入、读取任意网址、运行陌生仓库、执行实践实验、
向个人笔记写入内容、使用 Agent 工具链自动补齐证据。

## 内容与持久化契约

以下 TypeScript 是 API 类型草案；`ID`、`Instant` 均为字符串，ID 不透明，时间为 UTC RFC3339。

```ts
type AnalysisLevel = "SCREEN" | "DEEP";
type AnalysisOrigin = "MANUAL" | "RULES" | "MODEL";
type SectionKey = "WHAT" | "PROBLEM" | "ATTENTION" | "FIT"
  | "NEED" | "PRODUCT" | "TECH" | "GROWTH" | "BUSINESS" | "COMPETITION";
type ClaimKind = "FACT" | "HYPOTHESIS" | "JUDGMENT" | "UNKNOWN";
interface ClaimInput {
  kind: ClaimKind;
  text: string;
  evidenceIds: string[];
}
interface SectionInput { key: SectionKey; claims: ClaimInput[] }
interface AnalysisContent {
  sections: SectionInput[];
  openQuestions: string[];
  nextChecks: string[];
}
interface AnalysisVersion extends AnalysisContent {
  id: string;
  projectId: string;
  level: AnalysisLevel;
  version: number;
  origin: AnalysisOrigin;
  projectRevision: number;
  basedOnAnalysisId: string | null;
  createdAt: string;
  cutoffAt: string;
  rulesVersion: string | null;
  model: { provider: string; name: string; promptVersion: string } | null;
  evidenceIds: string[];
  // 包含所引用证据的原样元数据、原文摘录及观测窗口；形状沿用 catalog。
  evidenceSnapshots: Evidence[];
  trendSnapshot: TrendResult | null;
}
interface AnalysisView extends AnalysisVersion {
  // 读取时计算的提示，不修改原版本正文。
  contextChanged: boolean;
}
```

`SCREEN` 恰有 WHAT / PROBLEM / ATTENTION / FIT 四节；`DEEP` 恰有 NEED / PRODUCT / TECH /
GROWTH / BUSINESS / COMPETITION 六节，不重复、不混用。没有资料的节保存 UNKNOWN，
而不是补写肯定答案。每节最多 12 条声明，声明正文最多 2,000 字符，问题与下一步各最多 20 条、
每条最多 1,000 字符；输入总大小上限沿用共享 HTTP 限制。

数据库唯一约束 `(project_id, level, version)`，版本号从 1 开始、事务内分配。
AnalysisVersion 不提供 PATCH / DELETE；修订必须新增版本并可记录 `basedOnAnalysisId`。
声明在版本内有稳定序号，引用表唯一约束 `(analysis_id, section_key, claim_index, evidence_id)`；
创建版本、声明、引用快照与幂等结果同事务提交。

- FACT 至少引用一条真实存在且归属同项目的 Evidence；仅有 URL 或不存在的 ID 返回 422。
  收入文字必须保留证据中的声明者、金额口径、时间与“自述 / 估计 / 可验证披露”标签。
  “创始人称月收入 X”是可引用的陈述；不能改写成“已核实月收入 X”。引用存在本身不证明主张真实。
- HYPOTHESIS、JUDGMENT、UNKNOWN 可无证据，但不得在 UI 或导出中显示为事实。
  模型的建议显示为模型推测；个人最终决策仍由 research 独立维护。
- 引用的 Evidence 不可变；同一证据的修订使用新 ID。版本存引用与生成时快照，
  `projectRevision`、`cutoffAt` 和 trendSnapshot 均冻结，历史不随关联变更而重新绑定。
- 来源记录改关联后，旧版本仍可回溯旧上下文；读取返回 `contextChanged=true`。
  新版本不得引用来源已改关联到另一项目的旧证据。人工修订引用列表也必须重新验证。

## API 与模块边界

所有路径加 `/api/v1`，响应包裹、分页、错误与创建幂等遵循共享约定。

```ts
interface CreateAnalysisInput extends AnalysisContent {
  level: AnalysisLevel;
  projectRevision: number;
  basedOnAnalysisId?: string;
}
interface CreateAnalysisRunInput {
  level: AnalysisLevel;
  mode: "RULES" | "MODEL";
  projectRevision: number;
  evidenceIds: string[]; // 最多 50；只使用明确选中的证据
}
type RunStatus = "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "INTERRUPTED";
interface AnalysisRun {
  id: string;
  projectId: string;
  level: AnalysisLevel;
  mode: "RULES" | "MODEL";
  status: RunStatus;
  revision: number;
  cutoffAt: string;
  analysisId: string | null;
  errorCode: string | null;
  usage: { inputTokens: number | null; outputTokens: number | null } | null;
  createdAt: string;
  finishedAt: string | null;
}
interface AnalysisCapabilities {
  rulesEnabled: true;
  modelEnabled: boolean;
  provider: string | null;
  model: string | null;
  maxOutputTokens: number | null;
  dailyTokenBudget: number | null;
}
```

| 方法与路径 | 输入 | 输出与语义 |
|---|---|---|
| GET `/projects/{id}/analyses` | `level?`, `page`, `pageSize` | `Page<AnalysisView>`，createdAt 降序、id 稳定排序 |
| GET `/analyses/{id}` | 无 | `AnalysisView`；不存在 404 |
| POST `/projects/{id}/analyses` | `CreateAnalysisInput`，Idempotency-Key | 201 `AnalysisVersion`，origin 固定 MANUAL |
| POST `/projects/{id}/analysis-runs` | `CreateAnalysisRunInput`，Idempotency-Key | 202 `AnalysisRun`；先持久化任务与输入快照再运行 |
| GET `/analysis-runs/{id}` | 无 | `AnalysisRun`，携带 ETag，可轮询，失败任务保留 |
| GET `/analysis-capabilities` | 无 | `AnalysisCapabilities`；不返回秘密 |

输入 `projectRevision` 过期返回 412 `PROJECT_CONTEXT_CHANGED`；`basedOnAnalysisId` 必须是同项目、
同层级版本。幂等重放优先于重新验证可变项目版本；同意图不会重复创建版本或调用模型。
模型请求未启用返回 503 `MODEL_DISABLED`，超出预算返回 429 `MODEL_BUDGET_EXCEEDED`；
所有语义错误 422，格式错误 400。未知字段拒绝，防止误把个人笔记当模型参数发送。

提供给上层的只读接口为 `ListVersions(projectId, level?, cutoffAt, page)`、
`GetVersion(id)` 与 `GetLatestVersion(projectId, level, cutoffAt)`；没有版本返回可判定的缺失，
不能伪造空的“已完成分析”。领域接口返回版本内容，HTTP 包裹由 httpapi 负责。

## 行为、失败与模型约束

1. 规则生成只整理现有项目信息、引用和趋势证据，按固定模板列出未知项与待验证问题，
   明示“规则模板”。不根据星数推断收入、付费意愿、留存或市场规模。
2. MODEL 请求先冻结选定公开证据、项目版本和趋势快照。`public=false` 的证据拒绝发送（422），
   不悄悄删掉选择内容；默认不发送个人笔记、私人摘录、环境变量或凭据。
3. 模型提供者仅做受限文本生成，无浏览、工具或代码执行。上下文把来源文字视作引用数据；
   生成结果必须经过同一内容 schema、同项目证据引用、大小与枚举校验。
   schema 或引用校验失败时任务标 FAILED，保留错误分类，不发布分析版本。
   无法通过自动校验证实自然语言完全符合证据，UI 仍标“模型生成，待核对”，不称已验证。
4. 每次请求有配置的超时、输入大小和输出 Token 上限，预算先预留再调用、完成后结算。
   用量未知保留 null 并保留预算预留，不能当 0 放行后续无限调用。超时或进程重启不自动重调模型；
   任务进入 FAILED / INTERRUPTED，用户查看后可用新意图重试。
5. 模型失败不静默切换成规则成功。用户可以另行选择规则模板；两者 origin 始终可区分。
   生成完成只新增分析版本，永远不覆盖已有人工版本或 research 的个人记录。
6. 初筛停止条件是能说明对象与需求、至少一条关注证据及一个下一步；深研停止条件是六维都有
   已知或 UNKNOWN，并能列出最关键的验证问题。停止条件作为 UI 指引，不禁止保存不完整资料。

## 目标目录、命令与代码风格

实现目标为 `internal/analysis/`、相邻 `*_test.go`、`internal/httpapi/` 及
`web/src/features/analysis/`。采用 spec-common 中 Go/TypeScript 风格、类型示例、版本策略及全量命令；
本模块目标验证命令为 `go test -race ./internal/analysis/... ./internal/httpapi/...`。
这些路径与命令尚未实现，当前不执行，也不宣称通过。

## 验收场景

使用固定时钟、临时 SQLite、可控模型假提供者和 API 集成测试；模型真调用仅显式 opt-in。

| ID | 输入 / 场景 | 必须结果 |
|---|---|---|
| AN-01 | 无模型配置，零到一条证据 | 能保存人工分析、生成带 UNKNOWN 的规则版本；MODEL 明示未启用 |
| AN-02 | FACT 无引用、引用不存在或另一项目 | 422，无任何半成品版本；其他声明类型可明确无证据 |
| AN-03 | 创始人自述收入 | 详情与导出保留自述标签、期间、来源；不显示“已核实” |
| AN-04 | 相同创建 key 并发、不同 body 重用 key | 只生成一个版本 / 一次模型意图；处理中 409、不同 body 422 |
| AN-05 | 模型输出越界 ID、恶意 HTML、提示注入或非法 schema | 引用非法不发布；文本转义、无工具执行；失败可见 |
| AN-06 | 新模型版本生成时已存在人工记录 | 原分析可回溯，个人笔记 / 决策字节内容不变 |
| AN-07 | private 证据、预算耗尽、超时、重启 | 私密内容不发送；预算 / 错误明确；无自动重调或静默模板替代 |
| AN-08 | 来源重关联、旧 projectRevision 写入 | 旧版本快照保持；新写入 412 或非法引用 422；历史有上下文变化提示 |
| AN-09 | SCREEN / DEEP、分页、事实与推测混合 | 节数及枚举严格校验，稳定分页，标签与原始引用可见 |

## 边界与待配置项

- Always：可追溯引用、保留未知项、冻结分析版本、展示 origin 与上下文变化。
- Ask first：启用有费用的模型服务或扩大上传范围；自主联网研究或执行代码需要重新评审范围。
- Never：把模型输出当授权、把热度当营收、覆盖个人笔记、静默降级冒充模型成功。
- Open questions（不阻塞规格）：模型服务商、模型名、输出限制与每日预算待首次启用配置；
  默认 MODEL 关闭，RULES 与 MANUAL 可用。没有必须先回答才能完成本规格的问题。
