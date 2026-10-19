# SPEC — research-workspace

状态：**已实现并验证**。日期：2026-10-13，2026-10-16 更新。研究记录、偏好、认知库和
实践验证均已完成 T14/T15 验收。
范围依据：[能力图](CAPABILITY-MAP.md)，通用契约见 [spec-common](docs/spec-common.md)。

## 目标、依赖与非目标

把发现、初筛、六维研究和实践连接为一个中文个人研究工作台，留下可回溯的个人判断。
依赖 catalog 的项目 / 证据、trends 的排序、analysis 的版本；只有本模块接受用户意图写入
收藏、标签、个人笔记、判断及实践记录。页面可调用各模块 HTTP API，领域依赖不反向。

首版覆盖个人单用户、本机使用、浏览器刷新后持久化；不包含多人协作、付费账号体系、
完整任务管理、附件上传、自动代码沙箱、外部通知或自动执行实验的 Agent。

## 数据模型与约束

```ts
type ResearchStage = "SCREENING" | "DEEP_RESEARCH" | "PRACTICE" | "REVIEWED" | "ARCHIVED";
type Decision = "UNDECIDED" | "OBSERVE" | "LEARN" | "USE" | "VALIDATE" | "PASS";
interface ResearchFields {
  isStarred: boolean;
  tags: string[];
  stage: ResearchStage;
  decision: Decision;
  rationale: string;
  notes: string;
  nextAction: string;
  nextActionAt: string | null;
}
interface ResearchEntry extends ResearchFields {
  id: string;
  projectId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}
type ExperimentStatus = "PLANNED" | "RUNNING" | "COMPLETED" | "CANCELLED";
type ExperimentOutcome = "SUPPORTED" | "REFUTED" | "INCONCLUSIVE";
interface ExperimentFields {
  title: string;
  hypothesis: string;
  method: string;
  status: ExperimentStatus;
  timeBudgetMinutes: number;
  actualMinutes: number | null;
  result: string;
  outcome: ExperimentOutcome | null;
  evidenceIds: string[];
  artifactUrls: string[];
  nextAction: string;
}
interface Experiment extends ExperimentFields {
  id: string;
  projectId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}
interface ResearchPreferences {
  revision: number;
  focusTopics: string[];
  dailySaveTarget: number;
  weeklyDeepTarget: number;
  monthlyPracticeTarget: number;
}
```

`research_entries.project_id` 唯一；项目不存在时不创建孤儿记录。创建默认值为未收藏、空标签、
SCREENING、UNDECIDED、空文字与 null 提醒时间。`focusTopics` 默认 AI Agent / 开发者工具 / SaaS；
研究建议数量默认 3 / 2 / 1，允许 0–100，0 表示不显示该项建议。
首版展示建议，不把数量当硬限额，不把阶段编辑次数当已经完成的研究统计。

标签每项 trim 后 1–32 字符，最多 20 项；Unicode NFC 归一化，同一 Entry 内忽略拉丁大小写去重。
notes 最多 50,000 字符、rationale 最多 10,000 字符；nextAction 最多 2,000 字符。
时间为 UTC RFC3339，`nextActionAt=null` 清空日期。时间到达只在站内呈现，不创建外部提醒。

实验 title 必填，最多 200 字符；hypothesis / method / result 各最多 10,000 字符；
时间预算 1–10,080 分钟，默认 180，actualMinutes 为 null 或 0–1,000,000 的整数。
引用最多 50 条，必须是该项目的有效 Evidence；artifactUrls 最多 20 个、仅 HTTP(S)，
只保存和显示链接，不自动访问、不自动运行。COMPLETED 要求 result 非空且 outcome 非 null；
其余状态允许草稿。允许用户纠正状态，修改必须通过版本条件。

ResearchEntry、Experiment、Preferences 带 revision；首次为 1、每次有效修改递增。
个人正文只存在 research 表；分析生成器和采集器没有写入接口。项目来源关联变更不能自动迁移
个人记录，模型版本也不能“同步更新”个人判断。当前个人笔记允许覆盖编辑，完整笔记版本史
不在首版范围；已发布日报保存自己引用的研究快照，不受后续编辑影响。

## API 契约

所有路径加 `/api/v1`。列表默认 updatedAt 降序加 id；分页与错误采用 spec-common。
`Partial<T>` 仅允许明确列出的字段，不接受服务器维护字段；所有 PATCH 要求 If-Match。

| 方法与路径 | typed 输入 | 输出 |
|---|---|---|
| GET `/research` | `page`, `pageSize`, `isStarred?:boolean`, `stage?:ResearchStage`, `decision?:Decision`, `tag?:string`, `q?:string` | `Page<ResearchEntry>`；q 匹配项目名、标签和个人正文，最多 200 字符 |
| GET `/projects/{id}/research` | 无 | `ResearchEntry`；尚无记录 404，UI 显示未保存状态 |
| POST `/projects/{id}/research` | `Partial<ResearchFields>` + Idempotency-Key | 201 `ResearchEntry`；项目已有记录返回 409 `RESEARCH_ALREADY_EXISTS` |
| PATCH `/projects/{id}/research` | `Partial<ResearchFields>` + If-Match | 200 `ResearchEntry` |
| GET `/projects/{id}/experiments` | `page`, `pageSize`, `status?:ExperimentStatus` | `Page<Experiment>` |
| POST `/projects/{id}/experiments` | `ExperimentFields` + Idempotency-Key | 201 `Experiment` |
| GET `/experiments/{id}` | 无 | `Experiment` |
| PATCH `/experiments/{id}` | `Partial<ExperimentFields>` + If-Match | 200 `Experiment` |
| GET `/research-preferences` | 无 | `ResearchPreferences`；存在默认配置 |
| PATCH `/research-preferences` | `Partial<Omit<ResearchPreferences,"revision">>` + If-Match | 200 `ResearchPreferences` |

创建研究记录 / 实验与幂等结果同事务提交；已完成重试返回原结果。
过期 If-Match 返回 412，缺少为 428；无效引用、字段、状态为 422。空 PATCH 拒绝（422）。
PATCH 中 `null` 仅可用于 nextActionAt、actualMinutes、outcome；数组 `[]` 清空，字符串 `""` 清空。
stage 和 decision 独立，不因收藏、分析生成或实验完成而自动改写。

对 daily-brief 提供只读 `GetResearchSnapshot(projectIds)`，在日报截断事务中读取并复制当时 revision、
收藏 / 阶段 / 决策 / 下一步。模型提供者不消费该接口；用户手动复制生成内容到笔记后再保存才算采纳。

## 页面与行为

目标浏览器路由是 `/radar`、`/projects/:id`、`/library`、`/briefs` 与 `/settings`。
`/briefs` 具体内容遵循 daily-brief；来源配置调用 ingestion API，不在本模块另建配置事实源。

| 页面 | 用户能完成的事 | 必须显式区分的状态 |
|---|---|---|
| 雷达 | 按关注方向 / 来源筛选、搜索、查看排序原因、进入项目、收藏 | 未采集、采集中、空结果、历史不足、数据过期、源失败、演示模式 |
| 项目详情 | 来源与证据、增长口径、分析版本、个人研究、实践记录 | 事实 / 自述 / 推测 / 未知、模板 / 人工 / 模型、生成中 / 失败 |
| 认知库 | 收藏筛选、标签搜索、研究阶段与判断、下一步 | 无收藏、无匹配、未保存改动、并发保存冲突 |
| 设置 | 关注方向、研究建议、来源状态、模型启用信息 | 缺凭据与失败不同；凭据值不返回页面 |

三级研究指引：3 分钟回答是什么 / 需求 / 关注证据 / 是否继续；30 分钟覆盖六维并留下验证问题；
3 小时实践记录假设、方法、实际结果与行动。用户可跳过或返回阶段，不强制跑计时器或等待时长。
点击“新建实践”只打开记录表单；不会克隆、安装或运行项目。

保存采用显式提交和可见状态；网络失败保留当前页编辑内容，重试复用原意图 key。
导航离开未保存内容时提示；412 显示服务器已变化并保留本地文字，用户加载最新或人工合并后重试，
禁止自动覆盖服务器内容。刷新 / 关闭浏览器前未保存草稿不保证持久化，UI 明确说明。

基础界面要求：中文、360px 到桌面无横向溢出；表单有 label，键盘可操作、焦点可见，
模态框管理焦点并支持 Esc，加载和错误使用可读文本。颜色不是状态的唯一表达，
目标 WCAG AA 文本对比度；外部链接明确来源且采用安全的打开方式。
外部 Markdown / 模型正文禁用原始 HTML 和脚本协议，引用链接只允许 HTTP(S)。

## 目标目录、命令、风格与测试

实际目录为 `internal/research/`、相邻 `*_test.go`、当前扁平前端结构下的 `web/src/ResearchPanel.tsx`、
`web/src/LibraryPage.tsx`、`web/src/ResearchPreferencesPanel.tsx` 及 `web/e2e/research.spec.ts`。
技术栈、Go/TypeScript 示例与全量命令沿用 spec-common。
本模块目标命令为 `go test -race ./internal/research/... ./internal/httpapi/...`、
`npm --prefix web run test -- --run`、`npm --prefix web run test:e2e -- research.spec.ts`。
仓库、API、组件及真实浏览器流程均已执行；实际证据见验证记录。

| ID | 场景 | 必须结果 |
|---|---|---|
| RW-01 | 空库项目首次收藏、服务重启后读取 | Entry 单项目唯一，收藏 / 标签 / 笔记持久化，不依赖模型 |
| RW-02 | 两个页面同时编辑，后提交旧 revision | 412；服务器与本地文字都保留，不悄悄丢失修改 |
| RW-03 | 创建实验重试 / 并发请求 | 同 key 只保存一个实验；不同 payload 返回 422 |
| RW-04 | 三级研究跳转、保存 UNKNOWN 分析、超出建议收藏数 | 用户仍可保存，建议不构成强制门槛 |
| RW-05 | 完成实验缺结果；含陌生仓库 URL 的方法 | 缺结果 422；URL 仅链接、无网络抓取和代码执行 |
| RW-06 | 模型重生成、采集更新、来源改关联 | 个人 notes / decision 不变；项目历史保持原归属 |
| RW-07 | XSS 文本、键盘遍历、360px 页面 | 无脚本执行；主要操作可键盘完成，焦点可见且无水平溢出 |
| RW-08 | 空结果、离线保存、模型 / PH 未配置、首版无演示模式 | 状态文案不同，失败保留编辑内容；不自动回退样例数据，未来若引入演示模式必须全局可见 |
| RW-09 | 标签归一化、列表过滤 / 分页、非法字段 / null | 稳定结果与清空语义；不接受服务器字段或误清空 |

测试使用临时 SQLite、API 集成、Testing Library 与真实浏览器流程；控件及反馈需人工视觉复核。
浏览器环境不可用时记录未验证，不把类型检查当作 UI 验证。

## 边界与待配置项

- Always：个人写入与生成内容分离；所有编辑验证 revision；事实带来源、未知有明确文案。
- Ask first：扩大为多人账号、自动执行实践、持久化浏览器草稿或向模型发送个人笔记。
- Never：自动采纳模型结论、用阶段流转伪造研究完成、失败时清空用户文字、执行来源指令。
- Open questions（不阻塞）：关注方向和建议频率可在设置中调整；初始值已给出。
  暂无必须先回答才能完成本规格的问题。
