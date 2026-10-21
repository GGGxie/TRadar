# 实施验证记录

更新时间：2026-10-19（Asia/Shanghai）。这里只记录实际执行过的检查；当前任务状态以
[GitHub Issues](https://github.com/GGGxie/TRadar/issues) 为准，六模块逐项证据见
[`acceptance-matrix.md`](acceptance-matrix.md)。

## 已验证范围

| 范围 | 证据 |
|---|---|
| SQLite 初始化、迁移、事务和重启 | 临时真实数据库测试覆盖迁移、回滚、新版拒绝及关闭后重开；HTTP 重启测试验证项目仍可查询 |
| 项目身份 | URL 保守归一化、追踪参数移除、同名不同 URL 隔离、并发去重、稳定分页和中文搜索 |
| 项目 API | JSON 形状/大小、Host/Origin、同源写入、幂等重放、payload 不匹配、ETag 与旧版本冲突 |
| 项目界面 | 空状态、创建、列表、详情、编辑、刷新、搜索以及 360px 手机布局 |
| 证据 | 私密默认、URL 归一化、不可变修订、历史截止读取、公开过滤、跨项目引用拒绝、人工指标伪造拒绝 |
| 证据界面 | 新增、失败后草稿与重试 key 保留、修订保留旧版、原始来源外链及无障碍扫描 |
| 来源身份 | 稳定 `(source, externalId)` 去重、仓库改名保留项目 ID、新旧 URL 别名、冲突别名事务回滚及关联审计 |
| 观测 | 同 run 重试、不同 run 同值、累计/日桶隔离、桶内最新读取、非负安全整数、90 天窗口及失败事务无部分写入 |
| 来源设置 | 三个远程来源默认配置、按来源字段校验、ETag 冲突、凭据仅以存在状态显示、PH 默认禁用 |
| 采集任务 | 同键并发只建一个任务、完成重放原 202/Location、同源非终态唯一、来源独立完成、真实数据库重开后中断恢复 |
| 事务 checkpoint | catalog 数据、任务计数与 checkpoint 同事务提交；后一页失败保留前页；同 run/page 重放不重复写入 |
| GitHub 适配器 | 无 Token 搜索双桶、重复候选去重、稳定数字 ID、改名、最久未观测跟踪、认证停止、截断及空结果 |
| GitHub Star 历史 | 单自然周七个日桶、周总数校验、`UNSPECIFIED` 时区；历史 422 保留累计 Stars/Forks 并呈现 PARTIAL |
| Hacker News 适配器 | Top/New/Show 去重、讨论身份、共享外链归并、缺指标不填零、dead/deleted/null/非 story 跳过原因及空榜 |
| 请求预算 | GitHub 滚动小时与搜索分钟预算在请求前原子领取；并发 80 次只允许 60 次，关闭重开数据库不重置；平台响应额度只能收紧有效预算，匿名配置始终不超过 60/h |
| 重试与期限 | 网络错误、429、5xx 和明确的 GitHub 限流 403 最多三次并按 Retry-After/reset 退避；普通 403、401、解析失败不重试；单请求取消、五分钟任务取消及 `nextRetryAt` 持久化已覆盖 |
| 来源网络边界 | 生产客户端只接受三个内置 HTTPS 主机；解析结果含私网、回环、链路本地或保留地址即拒绝，连接固定到已校验 IP；同域最多一次重定向且删除 Authorization；解压后响应上限 2 MiB |
| 定时调度 | 持久化到期时间、停机多周期只补一次、同一到期时刻并发去重、来源忙时合并触发、平台退避前不运行及来源独立意图 |
| Product Hunt 条件适配 | 固定官方 GraphQL 端点、Token 请求、窗口分页、complexity 配额、部分 GraphQL 错误保留有效数据；缺凭据不发请求 |
| 来源设置界面 | 三来源配置、凭据/预算/恢复时间、未保存状态、立即采集与最近任务；组件和 Chromium 浏览器验证，axe 违规为零 |
| 真实来源冒烟 | 2026-10-15 匿名 GitHub 2 次请求导入 2 项并写 4 个观测；HN 2 次请求导入 1 项并写 2 个观测；临时 SQLite 关闭重开后项目详情、来源和观测仍可读取 |
| 手动限额反馈 | GitHub 本地搜索预算耗尽时，创建任务前返回 429 `SOURCE_RATE_LIMITED` 与 `Retry-After`，不留下排队任务 |
| 趋势增长计算 | 固定 asOf 的 24h/7d 容差窗口、真实跨度日均速度、下降、零基数、三点加速度、未来样本排除、累计/来源日桶隔离及至少四个完整日窗的持续性均由表驱动测试覆盖 |
| 趋势排序与雷达 | `trend-v1` 主信号评分、同平台去重、跨源加分、缺持续性部分结果、稳定并列排序和三点异常提示均有纯函数测试；SQLite 查询先评分后筛选分页，24h/7d 与来源筛选经 Chromium 验证 |
| 分析版本与规则 | SCREEN 四节、DEEP 六节、事实引用、跨项目拒绝、版本递增、规则模板、创始人自述标签、私密证据本地使用、冻结证据/趋势、来源重关联和项目版本变化均由数据库及 HTTP 测试覆盖 |
| 分析界面 | 证据选择、规则初筛、人工事实修订、旧版本保留、公开/私密快照、事实/推测/判断/未知标签、上下文变化提示和模型关闭状态经组件及 Chromium 验证；axe 违规为零 |
| 可选模型运行时 | 默认关闭；完整显式配置才启用。接受任务时只冻结明确选择的公开证据、项目版本和趋势，在 SQLite 事务内按 provider/model/UTC 日期预留预算；已知用量结算，未知用量及中断保留预留。超时、非法 JSON、越界引用、JUDGMENT、非法用量和重启中断均不发布半成品版本，也不降级为规则成功 |
| OpenAI 提供者边界 | 固定 HTTPS Responses 端点，`store=false`、空 tools、`tool_choice=none`、严格 JSON schema、1 MiB 响应上限及禁重定向；按 REST `output[].content[]` 读取文本并拒绝 refusal，API key 不进入响应或日志。请求形状、用量、REST 响应、refusal 和超限均由本地假服务验证，未消耗真实额度 |
| 模型接口与界面 | HTTP 创建为持久化后的异步 202，稳定 Location/ETag 与幂等重放只调用一次 provider；能力接口不返回秘密。启用态组件验证私密证据不可选择、只提交公开 ID、轮询完成和 MODEL 版本显示；默认关闭态及新增第三个操作按钮的 360px 布局经 Chromium 验证 |
| 服务访问边界 | 默认仅监听回环地址；非回环监听必须配置至少 32 字符访问令牌。缺失/错误 Bearer 被拒绝，精确令牌可访问；无 Origin 且 `Sec-Fetch-Site: cross-site` 的写请求被拒绝。规则经表驱动测试及条件变异验证 |
| 集成安全审查 | HTTP、固定外部端点、SQLite、模型输入输出、环境秘密五类信任边界已审查；请求体/响应体上限、同源、Host、SSRF、重定向、超时、参数化 SQL、安全响应头、通用 500 和秘密忽略规则均有代码或测试证据。未发现 raw HTML、eval 或 shell 执行路径 |
| 依赖完整性与漏洞 | Go 模块校验和通过；Go 1.26.9 生产二进制经官方 govulncheck 数据库扫描为 0；npm 锁文件签名/attestation 验证通过且离线审计为 0。可用更新单独记录，不与漏洞修复混合升级 |
| 可运行交付 | 不含 Git/依赖/构建物/数据的隔离副本按 README 完成锁定依赖安装和生产构建；空库启动后 health 为 200、页面为 HTML、schema 10 且 quick check 为 ok；SIGINT 正常退出 |
| 备份与恢复 | WAL 数据库通过 SQLite online backup 得到一致快照；备份/恢复 quick check 为 ok 且权限 0600。恢复缺显式确认会拒绝，覆盖已有目标前保留带 UTC 时间戳的副本 |
| 检查点 D | 六份规格共 58 个验收 ID，已逐项映射到 `docs/acceptance-matrix.md`；机器检查确认规格与矩阵均为 58 个唯一 ID 且无差集。补充 DB-08 的 STALE 直接断言后，全包 race 再次通过 |
| 个人研究数据与 API | 单项目唯一记录、默认值、Unicode NFC 标签归一化、拉丁大小写去重、字段边界、过滤搜索、幂等创建、ETag/If-Match 冲突、重启持久化和个人正文不受项目变化影响，均由 SQLite 仓库及 HTTP 测试覆盖 |
| 研究偏好 | 默认关注方向与 3/2/1 建议、0–100 边界、版本冲突和持久化均由仓库/API 测试覆盖；建议只提示节奏，不限制保存 |
| 个人研究界面 | 项目详情创建/编辑、收藏/标签/阶段/判断/笔记/下一步、认知库搜索筛选、偏好设置及 360px 布局经组件和 Chromium 验证；412 会展示服务器最新版本并保留本地草稿，重试使用新 revision；axe 违规为零 |
| 实践记录数据与 API | 计划/进行/完成/取消状态、180 分钟默认预算、实际用时与结论 null 清空、完成条件、同项目证据引用、HTTP(S) 产物链接、幂等创建、分页筛选、重启持久化和旧 revision 冲突均由 SQLite 仓库及 HTTP 测试覆盖；方法中的陌生 URL 只作文字保存 |
| 实践验证界面 | 创建/编辑、假设/方法/时间/结果/结论、证据和产物链接、保存失败草稿、稳定重试意图、离开确认及 412 手工合并经组件和 Chromium 验证；桌面与 360px 无横向溢出，axe 违规为零 |
| 日报选择与版本核心 | `brief-selection-v1` 仅选择首次覆盖或有新证据、分析、研究判断、可比增长变化的项目；相同内容的新 ID/采集时间及证据移除不伪装成新变化；接受任务时原子冻结证据、分析、研究判断、趋势与覆盖，来源重关联及 cutoff 后修改不改写旧版本；空数据、同日递增版本、失败任务可查询、IANA 时区/当日限制和设置版本冲突均由 SQLite 集成测试覆盖 |
| 日报调度、接口与导出 | HTTP 幂等生成、任务查询、分页历史、设置 ETag、旧版本读取与安全 Markdown 已覆盖；Markdown 只读取冻结快照，外部文本不能注入标题/HTML/危险 scheme，私密完整笔记不导出 |
| 日报运行时与时区 | 进程内单 worker、启动中断恢复、每日持久化意图、停机当日补偿、夏令时缺失/重复时刻和同日设置变更去重均有固定时钟测试；调度日期判断通过故意反转条件的变异检验 |
| 日报界面 | 历史版本、覆盖状态、变化理由、冻结证据、设置、手动生成、轮询与 Markdown 下载经组件和 Chromium 验证；桌面及 360px 无横向溢出，axe 违规为零 |

## 最近一次完整检查

以下命令在当前分支实际成功执行：

```sh
GOTOOLCHAIN=go1.26.9+auto GOCACHE=/tmp/tradar-go126-cache go test -race ./...
GOTOOLCHAIN=go1.26.9+auto GOCACHE=/tmp/tradar-go126-cache go vet ./...
GOTOOLCHAIN=go1.26.9+auto GOCACHE=/tmp/tradar-go126-cache go build -trimpath -o /tmp/tradar-vuln-scan-go126 ./cmd/tradar
go mod verify
/tmp/tradar-tools/govulncheck -mode binary /tmp/tradar-vuln-scan-go126
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
npm --prefix web audit --offline --json
```

交付入口也作为整体成功执行：

```sh
make setup
make build
make verify
make backup TRADAR_DB_PATH=<临时数据库> BACKUP_FILE=<临时备份>
make restore TRADAR_DB_PATH=<临时恢复库> RESTORE_FROM=<临时备份> CONFIRM_RESTORE=yes
```

结果：Go 1.26.9 全部包通过 race 测试及 vet；生产二进制构建成功，模块校验和全部有效，
govulncheck 二进制模式报告 `No vulnerabilities found.`；Vitest 为 9 个文件、16 个测试通过；Vite 生产构建成功；
Playwright Chromium 为 7 条流程通过。浏览器流程覆盖项目创建、证据新增和修订、项目编辑、刷新、
搜索、规则初筛、人工事实引用、版本上下文变化、个人研究保存与冲突合并、认知库筛选、
实践创建/完成/证据引用/冲突合并/未保存确认、日报多版本/冻结证据/安全下载、模型默认关闭状态、移动端布局、来源配置和手动采集，页面错误为零，
所检查页面 axe 违规为零。npm 完整依赖审计为 0 条已知漏洞；115 个 registry 签名及 39 个
attestation 已验证。真实冒烟通过显式 `TRADAR_LIVE_SMOKE=1` 单独执行，不属于默认测试联网行为。

浏览器审阅截图：

- [`catalog-desktop.png`](screenshots/catalog-desktop.png)
- [`catalog-mobile.png`](screenshots/catalog-mobile.png)
- [`evidence-desktop.png`](screenshots/evidence-desktop.png)
- [`analysis-desktop.png`](screenshots/analysis-desktop.png)
- [`library-mobile.png`](screenshots/library-mobile.png)
- [`practice-desktop.png`](screenshots/practice-desktop.png)
- [`brief-desktop.png`](screenshots/brief-desktop.png)

## 当前限制

Product Hunt 真实接口尚未使用用户账号 Token 联调，GitHub 日历史也未在真实冒烟中覆盖。
首版不提供演示模式；可选模型已实现但默认关闭，尚未执行真实 OpenAI 付费调用。
当前运行不会访问人工录入或实践产物的 URL，
也不会静默生成演示指标或商业结论。
公网发布、付费服务与外部通知不在当前授权范围内。
