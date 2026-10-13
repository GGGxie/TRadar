# Spec: project-catalog

状态：**详细规格草案，待审阅**，2026-10-13。
对应 [能力图](CAPABILITY-MAP.md) 的 `project-catalog`，无业务模块依赖。
[共享规格](docs/spec-common.md) 的技术栈、错误、分页、幂等、安全和完成标准适用于本模块。

## 目标与边界

给一个项目保存唯一身份、来自多个平台的独立线索及可回溯的证据/指标。
候选去重不损失原文；同名不自动合并。持久化可脱离网络和模型运行。
本模块不采集网页、不算热度、不生成分析，也不编辑个人研究笔记。

## 数据契约

`ID` 为不透明字符串，`Timestamp` 为 UTC RFC3339，所有字符串长度限制按 Unicode 字符计。
创建、修改时间由服务端产生；第三方发布时间可为 null；外部数值必须非负且为安全整数。

| 实体 | 字段与含义 | 约束 |
|---|---|---|
| Project | `id, name, canonicalUrl, summary, category, firstSeenAt, lastSeenAt, createdAt, updatedAt, revision` | category=`AI_AGENT/DEVTOOLS/SAAS/OTHER`；规范 URL 唯一；名称 1–200、摘要至多 4000 字 |
| SourceRecord | `id, projectId, source, externalId, recordType, url, targetUrl, title, excerpt, publishedAt, fetchedAt, revision` | `(source,externalId)` 唯一；source=`GITHUB/HACKER_NEWS/PRODUCT_HUNT/MANUAL`；type=`PROJECT/DISCUSSION/LAUNCH/STORY`；摘录至多 16000 字 |
| Observation | `id, sourceRecordId, runId, metric, basis, periodKey, value, observedAt, fetchedAt, providerPeriod` | `(runId,sourceRecordId,metric,basis,periodKey)` 唯一；只追加，不覆写旧累计观测 |
| Evidence | `id, projectId, sourceRecordId?, url, title, excerpt, kind, publisher?, publishedAt?, collectedAt, isPublic, observationIds[], supersedesId?` | 不可变；引用必须属于创建时同一项目；人工摘录默认 `isPublic=false`；最多 16000 字 |
| AssociationEvent | `id, sourceRecordId, oldProjectId, newProjectId, reason, changedAt` | 每次人工改关联新增审计事件；不级联搬移分析和笔记 |
| ProjectAlias | `projectId, normalizedUrl` | 别名全局唯一；仓库更名不创建新项目 |

指标枚举 `STARS/FORKS/POINTS/COMMENTS/VOTES` 与来源共同决定含义。
`basis=CUMULATIVE` 时 `periodKey=""`，`providerPeriod=null`；
`basis=SOURCE_DAILY` 时 `periodKey` 由来源周标识与 dayIndex 派生，
`providerPeriod={weekUnix:integer,dayIndex:0..6,timeZone:"UNSPECIFIED"}`。
日桶的获取时刻不是日桶边界。同桶多次获取保留各次观测，查询日历史按桶取截止时间前最新值，
不能把它们相加；日桶与累计快照不参与同一增量运算。

证据种类 `METRIC_OBSERVATION/OFFICIAL_DESCRIPTION/OFFICIAL_PRICING/FOUNDER_CLAIM/THIRD_PARTY_ESTIMATE/PUBLIC_EXCERPT`。
种类说明声明来源，不自动赋予真实性。证据若引用数值，包含相应 Observation IDs；
MRR/ARR 的摘录仍保留币种、期间、原始措辞和声明主体，不将自述提升为审计事实。
证据修改通过新增 `supersedesId` 版本表达；历史引用保留原内容。

## 身份与关联行为

1. 优先按 `(source,externalId)` 命中：GitHub 用稳定 repository ID，HN 用 item ID，PH 用 post ID。
2. 没有已知对象时，对目标 URL 做保守归一化：主机名小写、去默认端口和 fragment，
   只删除 `utm_*`、`gclid`、`fbclid` 等明确追踪参数；保留其他参数、非空路径和大小写。
   拒绝用户名/密码、非 HTTP(S) scheme 与无效主机。通用 URL 不擅自把 HTTP/HTTPS、www/非 www、
   `/a` 与 `/a/` 当作同一资源。GitHub 仓库 URL 可按其平台规则专门归一化。
3. 精确规范 URL/已知别名相同可关联。同域不同产品、仅名称相同或文章中提及相似名字不合并。
4. HN/PH 直接链接同一仓库或官网才归入已知项目；只链接新闻报道时保留独立候选。
5. 人工可把 SourceRecord 关联到另一个已存在的项目；要求 reason 和 revision。
   观测随来源记录归属用于后续查询，但不改观测内容。已有 Evidence 固定 projectId 不动，
   旧分析/日报以冻结引用保留；来源已迁走的证据从该旧项目的新分析输入中排除。
   旧项目保留，个人笔记不删除。旧分析标记上下文变化，供用户决定是否重新分析。

若未来需要完整项目合并/拆分、跨项目搬移研究记录，需要单独规格；首版只提供来源关联纠正。

## API 与内部接口

所有路径在 `/api/v1` 下，响应包装及分页遵循共享规格。

| 方法与路径 | 输入 | data 输出 |
|---|---|---|
| GET `/projects` | `q?:string, category?:enum, page?:int, pageSize?:int` | Project[]；默认 firstSeenAt 倒序再 id |
| POST `/projects` | `{name,canonicalUrl,summary?:string,category?:enum}` + Idempotency-Key | Project；URL 已存在返回已有项目 200，否则 201 |
| GET `/projects/{id}` | ID | Project |
| PATCH `/projects/{id}` | `{name?,summary?,category?}` + If-Match | Project；不允许直接更改 ID/来源归属 |
| GET `/projects/{id}/sources` | 分页 | SourceRecord[] |
| PATCH `/source-records/{id}` | `{projectId,reason}` + If-Match | SourceRecord；关联和审计事件同事务提交 |
| GET `/projects/{id}/evidence` | `asOf?:Timestamp, page?, pageSize?` | Evidence[]；排除该时点尚未产生的版本 |
| POST `/projects/{id}/evidence` | `{url,title,excerpt,kind,publisher?,publishedAt?,isPublic?:bool,sourceRecordId?,supersedesId?}` + Idempotency-Key | Evidence；不自动抓取 URL |
| GET `/projects/{id}/observations` | `sourceRecordId?:ID, metric?:enum,basis?:enum,from?:Timestamp,to?:Timestamp,page?,pageSize?` | Observation[]，时间升序再 id；窗口最长 90 天 |

人工作品的 `canonicalUrl` 作为 MANUAL 来源对象规范 URL；链接录入不隐式认定为已验证外部数据。
POST evidence 不接受客户端伪造自动指标观测，`METRIC_OBSERVATION` 只由内部采集/证据派生接口创建。
不同项目复用 supersedesId/sourceRecordId 返回 422；用户新增 evidence 不可填任意 collectedAt。

模块向 ingestion 暴露 `UpsertSource(ctx, input) -> SourceRecord`、
`AppendObservations(ctx, runID, recordID, inputs) -> []Observation` 的事务组合；
向其他模块暴露按项目、截止时间读取 Project/证据/观测的查询接口。
内部读取应能过滤 `isPublic` 及当前归属；历史分析用自己的冻结引用，不重新绑定。
来源记录及其观测的单次写入必须同事务完成，冲突不能产生半条记录。

## 命令、布局与风格

源文件归属 `internal/catalog/`、迁移归属 `internal/storage/`，HTTP 属于 `internal/httpapi/`。
采用共享规格的 Go 结构体风格、gofmt、显式 error/context；持久化使用参数化 SQL。
工程创建后目标验证命令：`go test -race ./internal/catalog/... ./internal/storage/...`、
`go test ./internal/httpapi/...`、`go vet ./...`；全量构建/开发命令见共享规格。

## 测试与验收

| ID | 必须验证的行为 |
|---|---|
| CAT-01 | 同一仓库改名与重复导入命中原稳定 ID；两个同名项目不同官网保持独立 |
| CAT-02 | URL 追踪参数可去除，业务参数保留；恶意 scheme/credentials 被拒绝；人工录入不产生网络请求 |
| CAT-03 | 同 run 重试无重复快照；不同 run 相同数值保留新的时间点；重复日桶读取不重复求和 |
| CAT-04 | 指标失败/缺失不写零；来源发布时间为空仍能保存；重启后所有记录与首次发现时间保留 |
| CAT-05 | 跨项目 evidence 引用和 supersedes 被拒绝；修订保留旧版；原始自述类别不被自动提升 |
| CAT-06 | 来源重关联写审计、保留全部原始观测和个人记录；旧分析引用不被悄悄改写 |
| CAT-07 | 两个并发创建只有一个身份；旧 revision 编辑返回 412；失败事务无部分提交 |
| CAT-08 | 列表分页稳定，未知项目返回 404，中文搜索可用，时间/长度/枚举校验返回统一错误 |

用临时真实 SQLite 检查约束与重启，用固定时钟测试历史查询，用 httptest 验证边界。

## 边界与待决项

- Always：原始证据可追溯、保守关联、参数化查询、事务与版本约束。
- Ask first：删除历史数据、批量合并既有研究项目或迁移外部真实数据。
- Never：用名称猜测后静默合并、自动联网抓取任意录入 URL、写入虚构指标。
- 无阻塞产品问题；数据库驱动确切版本随基础工程锁定并记录。
