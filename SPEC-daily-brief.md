# SPEC — daily-brief

状态：**T16–T17 已实现并验证**。规格日期：2026-10-13；
实现进度更新：2026-10-19。
范围依据：[能力图](CAPABILITY-MAP.md)；通用时间、幂等、分页、安全及技术约定见
[spec-common](docs/spec-common.md)。

## 目标、依赖与非目标

每天用一份可回溯的站内简报回答“哪些项目值得今天关注、相比上次有什么变化、下一步做什么”。
依赖 catalog、trends、analysis、research 的只读接口，拥有简报版本、生成任务和生成设置。
仅整理已保存资料，不在生成过程中发起采集或模型调用；无模型、无 PH 凭据仍可完成日报。

非目标：向飞书 / 邮件发送通知、保证程序停止期间准时生成、补写任意历史日期的假定状态、
发布公网报告、把未发生变化的项目每日包装成新趋势。

## 数据与不可变版本

```ts
interface BriefSettings {
  revision: number;
  isEnabled: boolean;
  timeZone: string; // IANA，默认 Asia/Shanghai
  localTime: string; // HH:mm，默认 08:30
  maxItems: number; // 默认 5，范围 1–20
}
type BriefReason = "NEW_DISCOVERY" | "FIRST_COVERAGE" | "GROWTH_CHANGE"
  | "NEW_EVIDENCE" | "ANALYSIS_CHANGE" | "RESEARCH_CHANGE";
interface BriefItem {
  projectId: string;
  projectRevision: number;
  title: string;
  url: string;
  reasons: BriefReason[];
  factSummary: string;
  hypothesisSummary: string | null;
  personalDecision: string | null;
  nextAction: string | null;
  evidenceIds: string[];
  evidenceSnapshots: Evidence[];
  trendSnapshot: TrendResult;
  analysisId: string | null;
  researchRevision: number | null;
  changeFingerprint: string;
}
interface DailyBrief {
  id: string;
  date: string; // YYYY-MM-DD，业务日期
  timeZone: string;
  version: number;
  cutoffAt: string;
  generatedAt: string;
  previousBriefId: string | null;
  selectionVersion: string;
  rankingVersion: string;
  isDemo: boolean;
  items: BriefItem[];
  coverage: Array<{
    source: string;
    latestCollectedAt: string | null;
    state: "AVAILABLE" | "STALE" | "NO_DATA";
  }>;
  emptyReason: "NO_DATA" | "NO_NEW_CHANGES" | null;
}
interface CreateBriefInput { date: string; timeZone: string }
interface BriefRun {
  id: string;
  status: "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "INTERRUPTED";
  revision: number;
  date: string;
  timeZone: string;
  cutoffAt: string;
  briefId: string | null;
  errorCode: string | null;
  createdAt: string;
  finishedAt: string | null;
}
```

唯一约束 `(date, time_zone, version)`；version 从 1 开始，成功发布时在事务内分配。
DailyBrief 与 BriefItem 不可变，没有 PATCH / DELETE；显式重新生成使用新意图并产生新版本。
`(brief_id, project_id)` 唯一，单份日报不会重复项目。旧版本始终能阅读和导出。

任务接受时冻结 `cutoffAt` 及对应数据快照，持久化后才响应 202；将项目身份、趋势结果、
证据摘录与口径、所选 analysis ID / 正文、研究 revision / 决策和覆盖信息一起冻结。
生成只能使用在 cutoffAt 前已采集且当时可见的资料，来源声称的发布时间不能绕过此限制。
将来新增资料、修改笔记或重关联来源都不能改变历史简报。
本文只允许生成请求时该 timeZone 的今天，避免在缺少历史个人笔记版本时伪造历史快照；
已保存的历史日报仍可正常查询和导出。

## 精选与变化口径 v1

1. 生成过程使用同一个冻结输入，趋势沿用指定 cutoffAt 的 rankingVersion 和解释。
   从用户 focusTopics 匹配的候选及已收藏项目中选择；未配置关注方向时不做主题限制。
   排除研究 stage=ARCHIVED 或 decision=PASS 的项目。
2. `previousBriefId` 是同 timeZone、cutoffAt 更早的最近成功版本（包括同日早些版本）。
   首次生成比较起点为 cutoffAt 前 24 小时；之后以 previousBrief.cutoffAt 为新发现起点。
   项目首次被系统发现落在该区间标 NEW_DISCOVERY；未在之前 7 个本地日的成功简报中出现、
   但仍符合当前候选筛选的项目可标 FIRST_COVERAGE。
3. 最近 7 日内已推荐项目只有发生下列变化才能再次推荐：
   新增内容不同的商业 / 需求证据（NEW_EVIDENCE）；最新已保存分析正文语义结构的规范化内容哈希
   不同（ANALYSIS_CHANGE）；个人决策或研究阶段改变（RESEARCH_CHANGE）；
   可比较的相同指标增长速度由非正变正或较上次达到至少 50% 增幅（GROWTH_CHANGE）。
   上次速度为 0 时不计算百分比，只应用非正变正规则；窗口口径 / 样本状态不同则不可比较。
   速度条件不等于商业机会保证，结果仍展示绝对值、原窗口与样本状态。
4. 变化指纹含归一化证据内容、分析内容、研究阶段 / 决策及所用增长口径，
   不含生成时间、采集时间、快照 ID 或排行榜名次。只有时间刷新、相同内容重复采集、
   微小累计值增加或版本号增加都不构成新变化。证据内容是否不同按 catalog 不可变正文哈希判断，
   商业 / 需求类别是证据元数据或用户标注，不通过模型临时推断。
5. 对符合变化条件的候选按趋势模块既定排序稳定取前 maxItems；理由保留全部命中的项。
   规则文字只摘要有证据的事实与已存分析推测，明确区分个人决策。没有分析可用 UNKNOWN 说明，
   不为凑足条数生成收入、竞争优势或增长数字。无数据生成 NO_DATA 空简报；有候选无变化生成
   NO_NEW_CHANGES 空简报，均为成功结果。

每项保留 source URL、采集时间、观测窗口、证据类型；自述收入始终标自述。
coverage 仅总结已采集资料的覆盖与新鲜度，不从未采集推断“服务正常”。
无数据源写 NO_DATA、最近采集超过 24 小时写 STALE；详细任务失败原因由来源设置页面显示，
日报不为读取采集任务而新增对 ingestion 的领域依赖。

## API 与调度

路径加 `/api/v1`；分页与错误采用共享契约。读响应包裹 `{data:...}`，Markdown 除外。

| 方法与路径 | typed 输入 | 输出 |
|---|---|---|
| GET `/briefs` | `page`, `pageSize`, `date?:string`, `timeZone?:string` | `Page<DailyBrief>`，date 降序、version 降序、id 稳定排序；包含所有版本 |
| POST `/briefs` | `CreateBriefInput` + Idempotency-Key | 202 `BriefRun`；冻结当前快照，今天之外 422 |
| GET `/brief-runs/{id}` | 无 | `BriefRun`，含 ETag；失败可见 |
| GET `/briefs/{id}` | 无 | `DailyBrief`；不存在 404 |
| GET `/briefs/{id}/markdown` | 无 | 200 `text/markdown; charset=utf-8`，安全固定文件名附件 |
| GET `/brief-settings` | 无 | `BriefSettings`；默认启用，08:30 Asia/Shanghai |
| PATCH `/brief-settings` | `Partial<Omit<BriefSettings,"revision">>` + If-Match | `BriefSettings`；时区 / 时间 / 范围无效 422 |

同 key 重试重放原任务、原 cutoffAt 与原结果，不重新取快照；处理中并发请求 409。
相同 key 不同参数 422；同日期时区不同 key 表示明确重生成，旧版本不覆盖。
每个 `(date,timeZone)` 同时最多一个活动生成任务，另一个意图返回 409 `BRIEF_ALREADY_RUNNING`；
只有成功或失败确认后才可重试，不以“任务慢”作为启动重复任务的理由。

定时器在同一应用进程内运行，不另装系统后台服务。每天到 localTime 生成一次，
内部幂等 key 为 `brief:schedule:{timeZone}:{date}`，同日期修改 localTime 不触发第二份定时意图。
程序重启后若已过今天设定时间且今天尚无该定时意图，可补今天；不批量补过去日期。
夏令时重复时刻只运行一次，缺失时刻在当天第一个有效时刻运行，业务日期仍按配置时区。
模型未启用不影响生成；进程中断任务标 INTERRUPTED，不自动伪装成成功或生成两份。
设置切换时区会产生不同业务日期身份，已有版本维持原时区，并在列表中显示。

Markdown 必须来自保存快照，含日期 / 时区 / version / cutoffAt / 算法版本 / 覆盖说明 / 引用 / 空结果原因。
转义标题、链接与来源文本，禁用嵌入原始 HTML，禁止将恶意来源文字拼成 Markdown 结构。
不导出 Token、完整个人 notes 或错误堆栈；已选的个人决策与下一步有明确标签。
同一 brief ID 多次导出的语义和内容相同，不能重新查询最新数据改写正文。

## 目标目录、命令与测试

实现目录为 `internal/brief/`、相邻 `*_test.go`、`web/src/BriefPage.tsx` 与
`web/e2e/zz-brief.spec.ts`（使用 `zz-` 让依赖空库的目录流程先执行）。
Go/TypeScript 风格示例、技术栈及全量目标命令沿用 spec-common；本模块验证目标为
`go test -race ./internal/brief/... ./internal/httpapi/...` 与
`npm --prefix web run test:e2e -- zz-brief.spec.ts`。截至 2026-10-19，brief / HTTP 聚焦测试、
全量 Go race/vet、前端组件/构建及完整 Chromium 流程均已执行通过。

使用固定时钟、临时 SQLite 和确定性输入做选择 / 版本 / 调度集成测试，浏览器验证站内阅读和下载。

| ID | 场景 | 必须结果 |
|---|---|---|
| DB-01 | 无模型、无 PH、没有资料 | 成功生成明确 NO_DATA 空简报，站内可读可导出 |
| DB-02 | 相同 key 并发 / 超时重试、定时器重启 | 最多一个意图，固定 cutoffAt；已有成功结果重放 |
| DB-03 | 同日显式再生成 | version 递增，旧版内容不变；没有变化允许空简报 |
| DB-04 | 来源重关联、笔记修改、cutoffAt 后到达旧发布时间证据 | 已保存正文 / 导出不变；迟到资料不倒灌旧快照 |
| DB-05 | 昨日已推荐项目仅重新采集相同内容 | 不重复推荐；时间 / ID 改变不算变化 |
| DB-06 | 新证据、分析正文变化、决策变化、速度可比与不可比 | 精确返回命中理由；不比较不同口径或除以零 |
| DB-07 | Asia/Shanghai 午夜、夏令时重复 / 缺失、停机恢复 | 业务日期准确，每时区日期定时只运行一次，仅补今天 |
| DB-08 | 部分来源无数据 / 过期，只有自述收入 | coverage 如实标注；收入仍带自述与来源，不补写零值 |
| DB-09 | Markdown 含恶意标题 / URL、缺少私密字段 | 无脚本 / 结构注入；不含 Token 与完整私人 notes |
| DB-10 | 分页多版本列表、旧 id 下载、生成失败 | 排序稳定，旧文件可回溯，失败可重查且不发布半成品 |

## 边界与待配置项

- Always：保存生成截断快照、业务时区与版本；展示覆盖不足；无变化允许为空。
- Ask first：外部推送、公开分享、历史回填策略、改变保留 / 删除规则。
- Never：重新计算后冒充旧日报、把源失败填为零、为凑数捏造推荐、自动购买或调用模型服务。
- Open questions（不阻塞）：生成时间、最多条数、时区均使用上述可编辑默认值；外部通知不属首版。
  当前没有阻塞本规格的问题。
