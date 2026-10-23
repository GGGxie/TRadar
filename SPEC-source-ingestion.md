# Spec: 信息源采集与任务运行

状态：**首版实现并验证**，2026-10-13 建立，2026-10-15 更新。来源配置、采集任务状态、原子幂等、
事务 checkpoint、重启中断、三来源适配器、持久化预算、受限重试、固定目标传输、调度、后台执行
和来源设置页均已验证；GH/HN 已完成真实小流量冒烟，PH 真实联调仍以用户凭据和平台条件为准。
模块 ID：`source-ingestion`。范围来自 [能力图](CAPABILITY-MAP.md)，仅依赖
`project-catalog`；共享约束采用 [spec-common](docs/spec-common.md)。

## 1. 目标与默认假设

让个人用户无需模型凭据即可发现 GitHub / Hacker News 项目，按小时积累可追溯观测，
随时辨认正常采集、没有结果、缺少凭据和来源故障。采集结果不代表完整覆盖全网，
更不等于商业价值判断。

本规格采用已提出的单用户、单进程、回环访问与 SQLite 假设。首次启动 GitHub/HN
启用，PH 默认禁用，全部来源的定时采集默认关闭；用户在来源设置启用定时后默认每 60 分钟
执行一次，也可手动触发。
允许间隔 15–1,440 分钟；改短间隔不会提高平台配额。默认界面将 GitHub 标为
“GitHub API 候选发现”，不宣称是 GitHub Trending 官方榜单。

首版范围：

- GitHub：官方 REST 搜索候选、持续读取已选仓库累计指标、条件可用的日级 Star 历史。
- HN：官方 Firebase 的 Top/New/Show 列表与 item；保存讨论链接和项目外链。
- Product Hunt：官方 GraphQL Token 适配器，只有显式启用且已配置有效凭据时调用。
- 人工材料：保存链接、摘录及声明类型，复用 catalog 创建项目和证据的接口，不自动访问链接。

不含网页通用爬虫、README 递归下载、评论全文抓取、Algolia 补全、OAuth 登录界面、
来源内容执行、外部推送或付费接口购买。来源依据见 [接入调研](docs/source-feasibility.md)；
该调研属于文档核实，尚未证明真实账户访问资格或运行时响应。

## 2. 采集行为与数据约束

### 来源适配

| 来源 | 发现与观测 | 结果与限制 |
|---|---|---|
| GitHub | 结构化配置关键词，构造搜索查询；读取仓库 ID、URL、名称、描述、创建/更新时间、Stars/Forks。搜索结果已有当前指标时不重复请求详情 | 首次默认关键词为 `agent`、`developer tools`、`saas`，每关键词分两桶：最近 180 天创建按 Stars 降序取 5 条；最近 7 天推送更新、不限创建时间，按 updated 降序取 5 条。两桶都附加 `stars:>=minStars`（默认 20，0 表示不过滤，[#34](https://github.com/GGGxie/TRadar/issues/34)）。范围和截断必须展示；不是对整个 GitHub 排名 |
| GitHub 跟踪 | 显式 `trackedSourceRecordIds` 中已知仓库，每轮按最久未观测优先读取累计值 | 每轮最多 20 个，资源不足时轮换，保留未读记录的原时间戳，不伪装已刷新；改名按仓库数值 ID 保持身份 |
| GitHub 日级历史 | 每轮最多尝试 5 个仓库、各读取最近一个来源自然周；先给已跟踪仓库（最久未取优先），剩余名额给本轮发现且尚无日级历史的候选，按 Stars 降序（[#33](https://github.com/GGGxie/TRadar/issues/33)）；后续轮换补齐 | 端点不可用/权限不足/字段不兼容时记录历史不可用，仍保留累计观测；不遍历 stargazer 用户列表代替历史 |
| HN | 默认 Top/New/Show 各取前 20 个 ID，去重后最多读取 60 个 item；显式跟踪项优先占用该上限 | 只接受 story 类型；`deleted`、`dead`、`null` 分别记跳过原因；无外链仍保留 HN 讨论记录。分数、评论数、发布时间、采集时间分开。分数低于 `minPoints`（默认 10）的**新**故事计为跳过、不写目录也不记错误；已入库或跟踪中的故事照常刷新；分数缺失不按 0 处理 |
| Product Hunt | 固定 GraphQL 查询近 7 天发布，单页 20、最多 3 页；读取 post ID、主页/产品外链、发布时间、投票与评论指标 | GraphQL `errors` 必须检查；同响应部分数据可独立验证导入，失败字段不能填零。截断和 complexity 配额对用户可见 |

默认采集窗口在任务开始时冻结。参数改变只影响下一次任务，任务保留所用配置版本和窗口。

采集**首次创建**项目时按名称与描述（HN 为标题）归类方向（[#36](https://github.com/GGGxie/TRadar/issues/36)）：
英文整词、中文词组匹配，按 AI Agent（agent/agentic/MCP/LLM/Claude 等）→ 开发者工具（CLI/SDK/API/framework/editor 等）→
SaaS（saas/b2b/subscription/tenant/billing 等）的优先级取第一个命中，都不命中为 OTHER。已有项目和用户修改过的分类
不会被后续采集覆盖；归类只用于筛选与日报关注方向匹配，不代表项目质量或商业属性已被核实。
允许用户调整 GitHub 关键词、发现天数和跟踪项；不允许用户提供任意 API base URL、原始
GraphQL、请求头或可执行表达式。外链仅作为候选关联证据，身份合并仍由 catalog 决定。

### 配额、网络与调度

- GitHub 无 Token 时，**本应用所有 GitHub 请求合计每滚动 60 分钟最多 60 次**，
  搜索每滚动分钟最多 10 次；请求在发出前持久化领取预算，失败与重试也计数。
  界面提供剩余预算和预计恢复时间。外部同 IP 程序可能消耗配额，因此响应头和平台拒绝优先；
  不把本地剩余额度视为平台承诺。有 Token 时默认应用预算仍为 60/h，可配置更高上限，
  有效限额取配置、已知平台限额和最新响应头中最严格值；无法识别配额头时不提高预算。
- 每个来源最多一个非终态任务，来源间可并行，最多同时运行三个来源任务。GitHub/PH
  各最多一个在途请求，HN 最多四个。一个来源退避不占用其他来源的执行额度。
- 定时基于持久化 `nextScheduledAt`；每个来源/到期时刻只有一个调度意图。
  进程停机错过多个周期时只补一次，不追赶执行大量过期任务；运行中的来源到期时合并该次触发。
  关闭定时不删除历史，也不隐式中断已经开始的任务。
- 单请求 15 秒超时，单任务最多 5 分钟；每个可重试请求最多 3 次尝试。
  网络暂时错误、429 和 5xx 可指数退避加随机抖动，首次基础等待 1 秒，最高基础等待 30 秒；
  实际等待不得早于 `Retry-After` / rate-limit reset。401/普通403/解析错误不盲目重试；
  GitHub 明确的限流403按限流处理。延迟超过任务剩余期限则结束任务并记录 `nextRetryAt`，
  下一次调度不早于该时点，不让后台持续重试。
- 仅 HTTPS 固定内置域名 `api.github.com`、`hacker-news.firebaseio.com`、
  `api.producthunt.com`；路径由已验证 ID/参数生成。拒绝跨域/降级重定向、URL 用户信息、
  私网/回环/链路本地目标及 DNS 重绑定；同域重定向也重新检查解析与连接目标，最多一次。
  不转发 Token 到重定向目标。正常生产配置不能用自定义 URL 绕过此边界；测试注入独立传输层。
- 解压后的单响应体最多 2 MiB，限额超出则拒绝该响应并记录 `RESPONSE_TOO_LARGE`；
  JSON/GraphQL 形状、指标非负安全整数和时间字段在边界验证。异常字段不生成假观测。
  外部错误正文不直接回显；日志包含 source/runId/错误分类、耗时、计数，不包含 Token 或正文。

### 身份、事务与恢复

catalog 拥有 `SourceRecord`、`Observation` 及证据；采集仅通过其批量写入接口提交。
`SourceRecord` 以 `(source, externalId)` 唯一：GitHub 仓库 ID、HN item ID、PH post ID
使用字符串表示平台稳定 ID，同名项目不直接归并。

一次成功响应中重复记录先按稳定键去重。观测键为
`(runId, sourceRecordId, metric, basis, periodKey)`；累计值 `basis=CUMULATIVE`、
`periodKey=''`。历史值 `basis=SOURCE_DAILY`，`periodKey` 由平台的周起点和日序号产生，
同时保存 `providerPeriod={weekUnix, dayIndex, timeZone:'UNSPECIFIED'}`。`dayIndex` 为 0–6，
不得猜测平台时区；日级新增值不进入滚动净增长。不同任务可以记录同一来源桶的修订值，
查询时选择截止时点前该桶的最新值，不能叠加重复桶。`observedAt` 是本次取得该值的时间，
不是把日桶强行换算成某个 UTC 日界。

每个批次先验证，再在同一个短 SQLite 事务中：调用 catalog 写入来源记录/观测、
更新导入计数、更新已完成页的 checkpoint。事务提交成功后才推进游标；网络调用在事务外。
适配器持有 catalog 定义的事务上下文，不直接改 catalog 表。失败批次全部回滚，已提交批次保留。

GitHub 搜索/HN 列表会变动，下一任务重新读取发现列表，不能把上次排名位置当作稳定增量游标；
checkpoint 记录任务内已处理 ID/页和固定查询窗口。PH 分页游标仅对记录的查询窗口有效。
重启时所有未终结任务标记 `INTERRUPTED`，保留已提交记录和最后 checkpoint，释放来源锁；
不自动把未知完成状态当成功。新意图从安全查询边界重新读取，目录键和观测键防重复；
新的任务可合法形成新时点观测，旧任务的同一页重放不得重复插入。

缺失记录不会让累计值归零；仓库不可访问、删除/隐藏或空响应不能删除历史。任务存在有效导入且
部分页失败、被预算截断或 `incomplete_results` 为真时为 `PARTIAL`。全部查询成功且没有
可接受项目为 `SUCCEEDED_EMPTY`；来源禁用或缺凭据根本不发请求，两者都不冒充空成功。

## 3. 边界接口

以下为契约类型，所有时间均为 RFC3339 字符串、ID 为不透明字符串；响应外层、分页、错误、
ETag/If-Match 和幂等记录保留策略均采用 [共享 API 契约](docs/spec-common.md#api-通用契约)。

```ts
type RemoteSource = 'GITHUB' | 'HACKER_NEWS' | 'PRODUCT_HUNT';
type RunStatus = 'QUEUED' | 'RUNNING' | 'RETRY_WAIT' | 'SUCCEEDED'
  | 'SUCCEEDED_EMPTY' | 'PARTIAL' | 'FAILED' | 'INTERRUPTED';
type SourceSettings =
  | { source: 'GITHUB'; keywords: string[]; discoveryDays: number;
      candidatesPerKeyword: number; trackedSourceRecordIds: string[];
      requestBudgetPerHour: number; minStars: number }
  | { source: 'HACKER_NEWS'; feeds: ('TOP' | 'NEW' | 'SHOW')[];
      itemsPerFeed: number; trackedSourceRecordIds: string[]; minPoints: number }
  | { source: 'PRODUCT_HUNT'; discoveryDays: number; maxPages: number };
type SourceConfig = SourceSettings & {
  isEnabled: boolean; isScheduleEnabled: boolean; intervalMinutes: number;
};
type SourceConfigPatch<S extends RemoteSource> =
  Partial<Omit<Extract<SourceConfig, { source: S }>, 'source'>>;
interface SourceView {
  source: RemoteSource;
  config: SourceConfig;
  revision: number;
  hasCredentials: boolean;
  availability: 'AVAILABLE' | 'DISABLED' | 'MISSING_CREDENTIALS';
  lastRunId: string | null;
  lastSuccessfulAt: string | null;
  lastObservationAt: string | null;
  nextScheduledAt: string | null;
  nextRetryAt: string | null;
  budget: { remainingRequests: number | null; resetAt: string | null };
}
interface CreateIngestionRunInput { source: RemoteSource }
interface IngestionRun {
  id: string;
  source: RemoteSource;
  trigger: 'MANUAL' | 'SCHEDULED';
  status: RunStatus;
  configRevision: number;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  windowStart: string | null;
  windowEnd: string;
  fetchedItems: number;
  acceptedItems: number;
  skippedItems: number;
  failedItems: number;
  observationsWritten: number;
  requestsMade: number;
  isTruncated: boolean;
  nextRetryAt: string | null;
  errors: { code: string; message: string; itemId: string | null }[];
}
```

`SourceConfigPatch<S>` 按 URL 的来源选择对应 union 分支的 partial schema，不能跨来源
接受字段。`source` 不可修改，未知字段拒绝。
关键词 1–5 项、每项 1–80 字符；GitHub `discoveryDays` 为 1–365、每词候选 2–30（新项目桶向上取半，近期更新桶取其余）；
跟踪 ID 最多 100 个且必须属于所配来源。匿名预算为 1–60，有凭据最多 5,000，
仍受平台实际限制；HN 每榜 1–50、合计 item 硬上限 60；PH 天数 1–30、页数 1–3。
GitHub `minStars` 为 0–100,000，HN `minPoints` 为 0–1,000；此前保存、缺少这两个字段的设置读取时取默认值。
所有配置必填字段在首次默认配置中给定，PATCH 仅改变显式字段，任何字段均不可用 null 清空。

| 方法与路径（前缀 `/api/v1`） | 输入 | 成功输出与语义 |
|---|---|---|
| `GET /sources` | 标准 `page/pageSize` | `SourceView[]` 分页，source 升序；只列三个远程源，未启用的仍返回 |
| `GET /sources/{source}` | `RemoteSource` | `SourceView` + ETag |
| `PATCH /sources/{source}` | `SourceConfigPatch<typeof source>` + If-Match | 200 `SourceView`，原子校验整份变更；凭据不接受经此接口提交 |
| `POST /ingestion-runs` | `CreateIngestionRunInput` + Idempotency-Key | 202 `IngestionRun` + Location；只创建指定来源任务 |
| `GET /ingestion-runs` | `source?`、`status?`、标准分页 | `IngestionRun[]`，createdAt 降序/id 降序 |
| `GET /ingestion-runs/{id}` | run ID | `IngestionRun`，终态及部分错误可查询 |

错误明细每任务最多返回 50 条，其余按错误类型汇总到日志及计数；不得让任务响应无界增长。
普通读取从不因来源不可达返回一个伪造成功结果；它返回持久化状态。网络失败发生于已接受任务，
由任务终态表达，不把异步采集失败改成任务读取的 HTTP 500。
`lastSuccessfulAt` 可因成功空结果更新，但 `lastObservationAt` 只有新指标落库才更新。
新鲜度按后者计算：null 为未观测，距今大于两倍配置间隔为过期；失败/空结果不刷新原指标的年龄。

创建请求在 `(operationScope, key)` 上原子领取并保存 payload 哈希及 202 结果。重复处理中
为 409 `IDEMPOTENCY_IN_PROGRESS`；领取完成后同 key 同 payload 重放原 202 及原 run ID，
通过 GET 获取最新任务状态；同 key 不同 payload 为 422 `IDEMPOTENCY_MISMATCH`。
即使新 key，同一来源已有非终态任务也返回 409 `SOURCE_BUSY` 和可定位现有任务的信息。
来源禁用为 422 `SOURCE_DISABLED`，PH 缺凭据为 422 `SOURCE_CREDENTIALS_REQUIRED`，
本地预算不足且无可执行请求为 429 `SOURCE_RATE_LIMITED`，提供 Retry-After。
任务/来源不存在 404，其他校验和版本冲突使用共享错误，不增加第二种错误响应格式。

GitHub 凭据通过 `TRADAR_GITHUB_TOKEN`、PH 通过 `TRADAR_PRODUCT_HUNT_TOKEN` 注入，
不保存明文到 SourceConfig。`hasCredentials` 仅说明存在，不证明有效；失效凭据在任务中显示
`AUTH_FAILED`，停止该源请求，修正配置后显式重试。来源设置保持可读取。

人工入口直接使用 [catalog 规格](SPEC-project-catalog.md) 的项目/证据创建契约，来源 `MANUAL`。
不会创建远程采集任务；URL 校验遵循 catalog 规则，合法外链保存成功不意味着已访问或验证内容。

## 4. 技术、命令、布局与风格

使用共享规格中的 Go/SQLite 技术方向，不新增第三方采集框架。
完整构建、启动和全套检查目标命令见 [共享命令](docs/spec-common.md#项目布局及目标命令)。
本模块专项验证为 `go test -race ./internal/ingestion/...`，已在当前实现上执行通过；真实来源冒烟
使用显式 opt-in 测试，默认测试不会访问外网。

适配器、预算、调度和任务状态位于 `internal/ingestion/`，`*_test.go` 与被测代码同目录；
去敏 fixture 由对应测试内的本地 HTTP 服务提供。HTTP 路由由 `internal/httpapi/` 映射，
来源配置页面位于 Web 界面。数据库迁移归 `internal/storage/`，采集只能拥有
SourceConfig、IngestionRun、Checkpoint 和预算/调度记录，不能重定义 catalog 模型。

使用 gofmt、context 取消、显式错误返回和注入时钟/传输层；接口风格示例：

```go
type Collector interface {
    Collect(ctx context.Context, input CollectInput) (CollectResult, error)
}
```

`CollectInput` 包含固定窗口、配置快照及已授权预算，不接收任意 URL；`CollectResult`
区分已验证批次、跳过与来源错误。领域层不返回 HTTP response，采集实现不依赖页面或模型。

## 5. 测试策略与验收

沿用 Go testing/httptest、临时真实 SQLite、固定时钟；测试不请求外网。
适配器测试验证请求目标/字段与响应解释，持久化测试验证唯一键和事务，HTTP 测试验证对外语义。
真实来源冒烟必须显式 opt-in，保存日期、凭据有无、状态和样本口径；不把 fixture 通过写成真实接通。

| 编号 | 可验收行为 | 验证方法 |
|---|---|---|
| ING-01 | 空库无需 Token 可导入 GH/HN fixture，每条事实有来源 URL、发布时间（可未知）和观测时间；PH 默认禁用，启用但无 Token 显示缺凭据 | 适配器 + API 集成；独立真实冒烟 |
| ING-02 | 单源断网仅该源失败，其他源完成；已存在指标不归零，任务错误与原观测新鲜度可查询 | 三来源并行故障注入 + SQLite |
| ING-03 | 匿名 GH 第 61 个滚动小时请求在发出前被拒绝，搜索第 11 个分钟请求被延迟；重启和重试不重置预算 | 固定时钟、真实预算表、请求计数 |
| ING-04 | 429/限流403 按 Retry-After 或 reset 延迟；三次尝试/5 分钟期限后停止；普通403/401 不重试 | httptest + 调度时钟 |
| ING-05 | 同 key 并发创建仅一任务，同 key 改 payload 为422；新 key 同源运行中为409；重启后的原任务可查 INTERRUPTED | HTTP 并发 + 进程恢复测试 |
| ING-06 | 页写入故障导致该页和 checkpoint 一起回滚；前页保留；重复导入同一 run/page 不多写观测 | SQLite 故障注入和唯一约束验证 |
| ING-07 | GH 改名保持仓库身份，HN 相同项目不同讨论不丢失；名称相同外链不同不被强行合并 | 目录边界集成 |
| ING-08 | 日历史缺失不阻止累计 Stars 写入；来源桶身份稳定，重复桶不被相加，也不转成滚动24小时增速 | 历史 fixture + catalog 查询 |
| ING-09 | 空榜、禁用、无 Token、上游失败分别呈现；deleted/dead/null item 被跳过且有原因；GraphQL 部分错误为PARTIAL | 适配器 + API 合约 |
| ING-10 | 人工恶意 URL/脚本 URL 拒绝，合法内网链接即使被保存也绝不发请求；来源响应中的恶意外链不触发抓取；重定向/私网 DNS 拒绝 | URL 校验 + 捕获全部出站请求 |
| ING-11 | 压缩后超限响应仍被截断，15秒超时可取消；错误文本/Token 不出现在 API 或日志 | 大体积/慢响应 fixture + 日志断言 |
| ING-12 | 调短间隔、停机数天、手动与定时同时触发均无任务风暴；来源独立，成功空结果不伪造观测 | 固定时钟调度集成 |

## 6. 边界与待审阅取舍

- Always：来源独立失败；预算先领取；游标与数据同事务提交；保存真实采集时间与统计口径；
  对外部内容进行形状、体积和网络边界校验；保留已成功记录与可解释任务状态。
- Ask first：新增平台/抓取任意网页、外部代理、付费服务、超出使用授权的来源接入、对公网发布。
  当前缺少 Token 不阻塞开发；PH 真实接通以用户账号权限与平台条件为准。
- Never：把演示数据回退成实时结果、把无结果/缺凭据/失败混为一类、覆盖个人研究记录、
  绕过限流、记录秘密、执行外部内容的指令、把官方日桶伪装成本地滚动窗口。

可在审阅时调整的默认值：每 60 分钟、GH 三个关键词/新项目180天+近期更新7天/每词共10条、HN 每榜20条、
PH 最近7天/最多3页。它们决定发现范围和配额消耗，均已有明确配置边界。
尚未完成 GitHub 日历史与 PH 账号的真实接口联调；不可用时展示历史不足或来源失败，
不能因此声称“已完成全源接通”。
