# 首版验收证据矩阵

状态：2026-10-19 检查点 D 已核对。这里把六份模块规格的 58 个验收编号映射到实际测试和运行证据；
完整命令结果见 [`verification.md`](verification.md)。“通过”只表示列出的自动或运行检查已执行，
不把未调用的付费服务或未授权的公网部署算作已验证。

## project-catalog

| ID | 主要证据 | 结果 |
|---|---|---|
| CAT-01 | `TestStableSourceIdentitySurvivesRepositoryRename`、`TestProjectIdentityAndConflictingEdits`、GH/HN adapter fixture | 通过 |
| CAT-02 | `TestNormalizeURLPreservesIdentity`、`TestNormalizeURLRejectsUnsafeInputs`、人工录入浏览器流程 | 通过 |
| CAT-03 | `TestObservationRetriesAndDailyBucketsRemainDistinct`、`TestBatchDataAndCheckpointCommitOrRollbackTogether` | 通过 |
| CAT-04 | GH/HN 空值 fixture、`TestCatalogSurvivesApplicationRestart`、真实临时库重开冒烟 | 通过 |
| CAT-05 | `TestEvidenceVersionsStayImmutableAndRespectHistory`、`TestEvidenceRejectsCrossProjectAndSyntheticMetricClaims` | 通过 |
| CAT-06 | `TestReassociationAuditsWithoutMovingEvidenceOrObservations`、`TestSourceReassociationMarksHistoryAndRejectsNewReference` | 通过 |
| CAT-07 | `TestProjectIdentityAndConflictingEdits`、`TestConcurrentRetryCreatesOneProject`、checkpoint 回滚测试 | 通过 |
| CAT-08 | `TestProjectValidation`、项目 HTTP 分页/404/中文查询、目录组件与 Chromium 流程 | 通过 |

## source-ingestion

| ID | 主要证据 | 结果 |
|---|---|---|
| ING-01 | `TestGitHubExecutorDiscoversRenamesAndKeepsCumulativeWhenHistoryFails`、HN fixture、默认来源配置、2026-10-15 GH/HN 真实小流量冒烟 | 通过；PH 真调用需用户 Token |
| ING-02 | `TestRunnerKeepsSourceFailuresIndependent`、错误/新鲜度 API 及旧观测保留断言 | 通过 |
| ING-03 | `TestGitHubBudgetIsClaimedBeforeRequestAndSurvivesRestart`、分钟预算及 80 并发领取测试 | 通过 |
| ING-04 | `TestRequestRetriesRateLimitsAndTemporaryFailures`、deadline/Retry-After 固定时钟测试 | 通过 |
| ING-05 | `TestRunCreationBusyRulesAndRestartRecovery`、`TestConcurrentIngestionRunReplayCreatesOneTask` | 通过 |
| ING-06 | `TestBatchDataAndCheckpointCommitOrRollbackTogether`，覆盖前页保留及同页重放 | 通过 |
| ING-07 | GH 改名 fixture、`TestHackerNewsExecutorKeepsDiscussionIdentityAndSkipReasons`、目录身份测试 | 通过 |
| ING-08 | GH 历史 422 fixture、累计与日桶隔离测试、趋势 basis 过滤 | 通过 |
| ING-09 | `TestEmptyRemoteResultsAreSuccessfulWithoutInventedProjects`、dead/deleted/null fixture、PH 部分/完全 GraphQL 错误及设置页 | 通过 |
| ING-10 | URL 拒绝测试、`TestSourceTransportRejectsTargetsOutsideFixedHTTPSHost`、私网 DNS 与重定向测试 | 通过 |
| ING-11 | 解压后超限/15 秒取消测试、认证错误脱敏、GraphQL/上游正文不落任务错误测试 | 通过 |
| ING-12 | scheduler 停机补一次、忙源合并、并发 tick、来源独立及空结果不造数据测试 | 通过 |

## trend-ranking

| ID | 主要证据 | 结果 |
|---|---|---|
| TREND-01 | `TestCalculateGrowthWindows` 单样本表项、`TestCalculateReturnsUnsupportedWithoutInventingValues` | 通过 |
| TREND-02 | 25 小时跨度表项，验证 delta、真实跨度日均和 relative change | 通过 |
| TREND-03 | 负增长与零基数表项 | 通过 |
| TREND-04 | 窗口容差表项、`TestCalculateMarksOldLatestStaleWhileKeepingMeasuredValues` | 通过 |
| TREND-05 | `TestCalculateAccelerationUsesComparableCumulativeSamples` | 通过 |
| TREND-06 | `TestCalculateIgnoresDailyOtherMetricAndFutureObservations` | 通过 |
| TREND-07 | `TestComputeProjectCountsPlatformsWithoutAddingDuplicateDiscussions` | 通过 |
| TREND-08 | `TestSortProjectTrendsIsStableForScoredAndAccumulatingGroups`、repository 先评分后筛选分页测试 | 通过 |
| TREND-09 | `TestCalculatePersistenceRequiresFourCompleteNonOverlappingWindows` | 通过 |
| TREND-10 | `TestComputeProjectAlertsOnlyOnFreshThreeSampleAcceleration` | 通过 |

## project-analysis

| ID | 主要证据 | 结果 |
|---|---|---|
| AN-01 | `TestRulesRunKeepsPrivateEvidenceLocalAndModelDisabled`、HTTP capabilities、规则/人工组件流程 | 通过 |
| AN-02 | `TestFactReferencesAreValidatedWithoutPartialVersion`、HTTP 422 测试 | 通过 |
| AN-03 | `TestManualAnalysisFreezesEvidenceAndVersions`、规则自述标签、日报 Markdown 与 Chromium 导出 | 通过 |
| AN-04 | 分析 HTTP replay/mismatch、共享 mutation 并发测试、模型 provider 调用计数 | 通过 |
| AN-05 | 模型未知引用、尾随/非法 JSON、refusal/超限响应测试；React 文本转义及无 tools 请求断言 | 通过 |
| AN-06 | 不可变分析版本测试、研究记录不受项目/模型变化影响测试 | 通过 |
| AN-07 | 私密证据/预算、超时/重启中断、未知用量及禁止自动重调测试 | 通过 |
| AN-08 | 来源重关联历史、旧 projectRevision 412、上下文变化组件/浏览器流程 | 通过 |
| AN-09 | SCREEN/DEEP schema 校验、稳定分页、混合声明类型与原始引用界面 | 通过 |

## research-workspace

| ID | 主要证据 | 结果 |
|---|---|---|
| RW-01 | `TestCreateEntryDefaultsAndSurvivesRestart`、研究 HTTP 与 Chromium 刷新流程 | 通过 |
| RW-02 | repository stale revision、HTTP 412、组件及 Chromium 本地草稿合并提示 | 通过 |
| RW-03 | 实践 HTTP replay/mismatch、共享 mutation 并发约束 | 通过 |
| RW-04 | 默认 3/2/1 偏好与 0–100 可编辑测试；保存不受建议数强制限制，UNKNOWN 分析可保存 | 通过 |
| RW-05 | `TestExperimentPersistsReferencesAndCompletesOnlyWithAResult`、陌生 URL 仅作为文字/链接测试 | 通过 |
| RW-06 | `TestListEntriesFiltersSearchesAndProjectChangesDoNotRewriteNotes`、来源重关联不移动个人记录 | 通过 |
| RW-07 | React 默认转义、axe、桌面/360px Chromium 无溢出、全局 `:focus-visible` 样式和标准表单控件 | 通过 |
| RW-08 | 空状态、离线/保存失败保留草稿、MODEL_DISABLED、PH 缺凭据测试；README 与默认空库证明无演示回退 | 通过；首版无演示模式 |
| RW-09 | Unicode/大小写标签归一化、列表搜索过滤、分页、未知字段及 null 清空语义测试 | 通过 |

## daily-brief

| ID | 主要证据 | 结果 |
|---|---|---|
| DB-01 | `TestEmptyBriefVersionsAndFailedRunsRemainQueryable`、站内空简报和导出流程 | 通过 |
| DB-02 | brief HTTP replay、active intent、scheduler 并发 tick 与中断恢复测试 | 通过 |
| DB-03 | 同日 version 1/2、previousBriefId 及旧版本不变测试 | 通过 |
| DB-04 | `TestAcceptedBriefInputStaysFrozenAcrossLaterEvidenceAndResearchChanges`、来源重关联冻结测试 | 通过 |
| DB-05 | `TestFingerprintIgnoresIdsTimesAndRepeatedEquivalentContent` | 通过 |
| DB-06 | meaningful change 选择表、不可比趋势与稳定排序测试 | 通过 |
| DB-07 | DST 缺失/重复时刻、同日设置变化、并发 tick、Asia/Shanghai 当日校验及恢复测试 | 通过 |
| DB-08 | 冻结测试同时覆盖 AVAILABLE/STALE/NO_DATA，FOUNDER_CLAIM 始终带“创始人自述” | 通过 |
| DB-09 | `TestMarkdownUsesOnlyFrozenDataAndEscapesUntrustedStructure`、恶意标题/URL 浏览器下载 | 通过 |
| DB-10 | 分页多版本、按旧 ID 读取/下载、损坏输入失败可查且无 briefId 测试 | 通过 |

## 跨模块门禁

- `make verify`：Go race/vet、Vitest、Vite build、Chromium 7/7 和 npm 离线审计全部通过。
- Go 1.26.9 生产二进制 govulncheck 为 0；Go 模块校验和及 npm registry 签名/attestation 有效。
- 隔离交付副本完成依赖安装、构建、空库启动、健康检查、SQLite 在线备份和确认恢复。
- 真实付费 OpenAI、Product Hunt Token、公网 TLS/账号/多租户不在首版验证范围，未声明为通过。
