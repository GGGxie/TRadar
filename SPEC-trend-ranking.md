# Spec: trend-ranking

状态：**首版实现并验证**，2026-10-13 建立，2026-10-15 更新。增长窗口、下降/零基数、
加速度、持续性、日桶隔离、可解释排序、异常提示、查询 API 与雷达交互均已验证。
对应能力图 `trend-ranking`；仅依赖 `project-catalog`。
共同技术栈、API 包装、目标命令和风格遵循 [共享规格](docs/spec-common.md)。

## 目标与非目标

回答“哪些项目正在升温，以及依据是什么”，同时展示关注总量和观测局限。
使用确定性计算，不调用模型；结果不表示需求已验证、收入可信或适合投资。
不把不同平台的 Stars、points、votes 原始值相加。

## 增长口径 v1

计算输入包含固定 `asOf`、项目目录和 `asOf` 前的观测；禁止直接读取系统当前时间的纯计算逻辑。
每个来源记录、指标、basis 分开计算。主信号候选只含 GitHub STARS、HN POINTS、PH VOTES。
同项目多个讨论条目分别计算并保留来源；人工摘录不提供数值增长。

- **最新值**：`t1` 为 `asOf` 之前最新有效累计快照；超过 3 小时标记 STALE，不参与增长排序。
- **24h 窗口**：选取不晚于 `t1−24h` 且距该目标不超过 2 小时的最近累计快照 `t0`。
  因此真实跨度在 24–26 小时；7d 窗口允许目标前 6 小时。无匹配样本标记 INSUFFICIENT_HISTORY。
- **净变化**：`delta = value(t1) − value(t0)`，保留负值；总数回落不是采集错误。
- **日均速度**：`ratePerDay = delta / ((t1−t0)/24h)`。
  输出真实 `from/to/durationHours`，界面写“观测期净增 / 折算每天”，不冒称精确 24h 新增。
- **相对变化**：`growthRatio = delta / value(t0)`；基数为 0 时返回 null，不能生成无穷百分比。
- **加速度**：只有同记录同指标的三个累计样本可用时计算。`t0` 前再找一个与所选 window
  同样跨度/容差的样本 `tMinus1`（24h 窗口为 24–26 小时，7d 为 168–174 小时），得到前后两段日均速度；
  `accelerationPerDaySquared = (rateNew − rateOld) / midpointDistanceDays`。
  没有两段可比窗口为 null。乱序、重复时间戳经稳定去重后不得除零。
- **日级历史**：SOURCE_DAILY 只用于来源日桶图，不参与上述净增/加速度。
  不把未知来源日界转换成北京时间的“昨天”，也不把新增 Stars 当作扣除取消后的净增长。
- **持续性**：以 t1 向前先一次性选定 8 个日边界（目标前最多 2 小时），相邻边界形成
  7 个不重叠窗口，只采用跨度 24–26 小时的有效窗口。至少 4 个完整有效日窗
  才返回 `positiveWindows / validWindows`，同时显示 `validWindows/7`；不足为 null。

数据状态 `READY/INSUFFICIENT_HISTORY/STALE/UNSUPPORTED` 与数值并列，未知值不填零。
采集失败但旧样本仍在新鲜度内可计算，并另显示来源上次失败；失败不能刷新观测时间。

## 可解释排序 v1

首版使用明确标记为启发式的关注优先级，权重可在后续基于用户反馈校准。
公式版本 `trend-v1` 写入每次结果，不提供精确成功概率。

1. 对每条有效主信号计算 `g = 50 × min(1, ln(1 + max(ratePerDay,0)) / ln(1 + scale))`。
   scale：GitHub 1000 Stars/day，HN 100 points/day，PH 100 votes/day。
   项目增长分取最大的 g，保留获选 record ID/指标；不累加多个讨论条目。
2. 跨源数为过去 7 天被实际采集到并可靠关联的独立平台数，仅含 GitHub、HN、PH。
   `c = 15 × (min(platformCount,3) − 1)`，无自动来源取 0；同平台十条讨论仍算 1。
3. 选中主信号持续性可用时 `p = 20 × positiveWindows/validWindows`，否则贡献 0 并明确
   `persistence.status=INSUFFICIENT_HISTORY`。0 分贡献是缺项策略，不表示已经观测到零持续性。
4. 至少一条主信号有有效增长时 `score = round(g+c+p,1)`，否则 `score=null`，放入待积累候选组。
   返回 `isPartial=true` 指示持续性缺项；完整度不隐含真实性。
5. 排序按“有增长评分组、score 降序、firstSeenAt 降序、projectId 升序”；
   待积累组按 firstSeenAt 降序、projectId 升序。筛选只过滤结果，不改变公式或分母。

异常增长提示需要至少 3 个有效累计样本、`rateNew >= 2 × max(rateOld,0)` 且达到最小日均净增
（GitHub 100、HN 30、PH 30），数据必须新鲜；每个项目/指标/UTC 日最多显示一个当前提示。
这是站内研究提示，无自动外部消息，阈值和证据随结果返回。
没有增长历史的热门项目可以按累计量单独查看，但不能带“正在暴涨”标签。

## 结果与 API

`TrendResult` 包含：

```text
projectId, asOf, algorithmVersion, status, score:number|null, isPartial:boolean
primarySignal:{sourceRecordId,source,metric,latestValue,latestObservedAt}|null
growth:{status,from,to,durationHours,delta,ratePerDay,growthRatio,observationIds[]}
acceleration:{status,value:number|null,observationIds[]}
persistence:{status,positiveWindows:int|null,validWindows:int,totalWindows:7}
crossSource:{platforms:Source[],sourceRecordIds[]}
components:{growth:number|null,crossSource:number,persistence:number|null}
explanations:string[], alert:{kind,threshold,observationIds[]}|null
```

`components.persistence=null` 时总分采用上节缺项贡献规则，不能返回其值为 0 来假装已观测。
score/growth 状态区分清楚：项目可能有单平台可用数据而其他来源缺失。

| 方法与路径（前缀 /api/v1） | 输入 | data 输出 |
|---|---|---|
| GET `/radar` | `q?,category?,source?,window=24h|7d,page?,pageSize?,asOf?` | `{project:Project,trend:TrendResult}[]`，统一分页 |
| GET `/projects/{id}/trend` | `window=24h|7d,asOf?` | TrendResult |

缺省 window=24h、asOf=请求开始时刻。asOf 不得在未来；观测读取最多 90 天。
跨源数依据 asOf 截止前有效关联记录；旧日报直接保存 TrendResult，不在阅读时重算。
内部 `Compute(project, records, observations, asOf, window) -> TrendResult` 无副作用，
同输入和算法版本必须产生逐字段相同结果；不写目录表。

## 命令、布局、风格与验证

实现位于 `internal/trends/`，纯函数表驱动测试置于同目录 `*_test.go`；
HTTP 路由归属 `internal/httpapi/`。工程创建后运行 `go test -race ./internal/trends/...`、
`go test ./internal/httpapi/...` 及共享规格的全量检查。gofmt；分离样本选择与公式，避免隐藏时钟。

| ID | 验收示例 |
|---|---|
| TREND-01 | 单快照：score、delta、加速度全为 null，状态历史不足；累计值仍可见 |
| TREND-02 | 25 小时 100→150：净增 50、日均 48、相对变化 0.5，明确真实 25 小时跨度 |
| TREND-03 | 24 小时 100→90：净增 −10 保留、无异常增长；0→10 相对变化 null |
| TREND-04 | 24 小时窗口外超过容差的快照不被选中；最新样本超过 3 小时转 STALE |
| TREND-05 | 3 个连续 24h 点 100→124→172：速度 24→48，加速度 24/day²；重复时刻不除零 |
| TREND-06 | 日级新增桶和累计快照混存时只选累计；同日桶重取不会翻倍；未来观测排除 |
| TREND-07 | 同平台多个讨论不增加平台数；三个平台的来源 ID 完整返回；人工摘录不计平台加分 |
| TREND-08 | 固定输入排序稳定；不同筛选不改变项目评分；相同分数有确定并列顺序 |
| TREND-09 | 持续性少于 4 个有效日窗为 null/isPartial；断采不被补零，负增长日不算正向 |
| TREND-10 | 普通关注量高但无增速样本不触发告警；满足双倍增长、绝对阈值和三样本后才提示 |

## 边界与待决项

- Always：返回公式版本、真实样本窗口、证据 IDs 和缺项；保留下降数据。
- Ask first：将评分用于投资建议、改变来源/统计口径却继续沿用旧算法版本。
- Never：从总量捏造增速、把缺值补零、把启发式分数包装成商业成功率。
- 初始阈值为可审阅工程默认值，后续实测后可版本化调整；不阻塞首版实现。
