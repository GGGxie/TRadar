# TRadar

TRadar 是个人技术趋势与商业机会雷达。它把项目发现、来源观测、增长排序、证据、四问/六维分析、
个人研究、实践验证和每日简报保存在一个可追溯的本地工作区。首版是单用户 Go + SQLite 服务，
React 页面由同一进程提供。

**当前状态：首版已完成本地交付检查；六个模块的 58 项验收证据均已建立映射。**

- [能力图与首版范围](CAPABILITY-MAP.md)
- [详细规格总览](docs/spec-overview.md)
- [实施任务与状态](tasks/todo.md)
- [实际验证证据](docs/verification.md)
- [六模块验收矩阵](docs/acceptance-matrix.md)
- [agent-skills 工程过程记录](docs/process/README.md)

工程方法来自 [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)。项目固定了上游
版本和 MIT License，并按规格、任务拆分、TDD、增量提交、安全审查及交付检查推进。

## 快速开始

需要 Go 1.26.9（或支持自动下载该工具链的 Go）、Node.js 22、npm 10。备份和恢复另需 `sqlite3`。

```sh
make setup
cp .env.example .env
# 按需编辑 .env；默认值可直接用于回环地址上的空库
set -a
. ./.env
set +a
make build
./bin/tradar
```

打开 `http://127.0.0.1:8080/radar`。另一个终端可检查：

```sh
curl -fsS http://127.0.0.1:8080/healthz
```

成功响应为 `{"data":{"status":"ok"}}`。使用 `Ctrl+C` 可让服务完成关闭；默认数据库位于
`data/radar.db`。重新启动同一命令会运行必要迁移并继续使用已有数据。

常用命令：

| 命令 | 作用 |
|---|---|
| `make setup` | 按 `go.sum` 和 `web/package-lock.json` 安装锁定依赖 |
| `make build` | 生成 `web/dist` 和 `bin/tradar` |
| `make run` | 重新构建并以前台进程启动；使用当前 shell 已导出的配置 |
| `make test` | 运行 Go race/vet 和前端组件测试 |
| `make e2e` | 构建后运行 7 条 Chromium 完整流程 |
| `make verify` | 运行发布验证集和 npm 离线漏洞审计 |

## 配置

TRadar 只读取进程环境变量，不会自动读取 `.env`。`.env.example` 可以复制为本地 `.env`，该文件已
排除版本控制；启动前由 shell 或进程管理器导出。不要把真实令牌写入 `.env.example`。

| 变量 | 默认/约束 | 用途 |
|---|---|---|
| `TRADAR_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `TRADAR_DB_PATH` | `data/radar.db` | SQLite 文件路径 |
| `TRADAR_WEB_DIR` | `web/dist` | 已构建前端目录 |
| `TRADAR_ACCESS_TOKEN` | 本地为空；非回环至少 32 字符 | 非回环 API Bearer 凭据 |
| `TRADAR_ALLOWED_HOST` | 空 | 非回环时允许的精确 Host 名称 |
| `TRADAR_DEV_ORIGIN` | 空 | 仅开发时允许的 Vite Origin，例如 `http://127.0.0.1:5173` |
| `TRADAR_GITHUB_TOKEN` | 空 | 可选；提升 GitHub 请求额度 |
| `TRADAR_PRODUCT_HUNT_TOKEN` | 空 | 可选；缺少时 Product Hunt 明确不可用 |

远程使用优先通过 SSH 隧道访问默认回环服务。直接绑定非回环地址时，进程会拒绝少于 32 字符的
`TRADAR_ACCESS_TOKEN`，HTTP 层还要求 `TRADAR_ALLOWED_HOST` 与请求 Host 匹配。首版没有 TLS、账号和
多用户授权，不能直接作为公网服务发布。

模型分析默认关闭。只有显式设置以下全部变量才会启用并产生外部调用：

```sh
TRADAR_MODEL_PROVIDER=openai
TRADAR_MODEL_NAME=<明确选择的模型名>
TRADAR_OPENAI_API_KEY=<密钥>
TRADAR_MODEL_MAX_OUTPUT_TOKENS=800
TRADAR_MODEL_DAILY_TOKEN_BUDGET=100000
TRADAR_MODEL_TIMEOUT=45s
```

输出上限允许 128–8192，每日预算上限为 100,000,000，超时最长 5 分钟。模型只接收本次明确选择的
公开证据，不接收私密证据或个人研究正文；输出仍标记为待人工核对。

## 数据真实性与联网行为

新数据库为空，没有演示数据，也没有自动生成的项目、增长或营收。首版没有演示模式；测试使用临时
数据库，不会填充 `data/radar.db`。界面展示的数据来自以下路径：

- 人工项目、证据、判断、笔记和实践记录由本机用户保存；
- GitHub 与 Hacker News 初始可用，但自动调度默认关闭，只有手动采集或明确开启调度后才联网；
- Product Hunt 缺少 Token 时保持不可用，不会回退到样例数据；
- 模型 provider 留空时不会发出模型请求，也不会消耗额度；
- 采样不足时增长为缺失/部分结果，不把未知值填成 0，也不凭热度推断收入或留存。

来源记录保留 URL、采集时间和观测窗口。规则、模型推测与用户判断使用不同标签，历史分析和日报按
生成时刻冻结，便于回溯当时证据。

## 备份、恢复与回滚

`make backup` 使用 SQLite 在线 backup API，能从 WAL 数据库取得一致快照；不需要只复制活跃的主文件。
默认写到被 Git 忽略的 `backups/`，完成后运行 `PRAGMA quick_check` 并把权限设为 0600。

```sh
make backup
# 或指定文件
make backup BACKUP_FILE=/安全目录/tradar-20261019.db
```

恢复会替换当前数据库，因此先用 `Ctrl+C` 停止 TRadar。目标旁存在 `-wal` 或 `-shm` 时命令会拒绝继续；
覆盖前还会把原数据库保存为带 UTC 时间戳的 `.before-restore-*` 文件。

```sh
make restore RESTORE_FROM=backups/tradar-20261019T080000Z.db CONFIRM_RESTORE=yes
make run
```

恢复命令先检查备份完整性，再用 0600 权限原子替换目标。启动旧程序前要确认它支持备份中的 schema；
程序会拒绝打开比自身更新的数据库。代码回滚前应保留升级前备份，切回对应提交并恢复该备份，
不要尝试手工降级 schema。

## 验证与工程资料

发布前运行：

```sh
make verify
```

最近一次完整结果为 Go 全包 race/vet 通过、Vitest 9 文件 16 测试通过、Vite 构建成功、Chromium
7/7 通过、npm 已知漏洞为 0；Go 1.26.9 生产二进制的 govulncheck 结果为 0。命令、范围和限制见
[验证记录](docs/verification.md)。HTTP 健康检查为 `/healthz`，服务日志以 JSON 写到标准错误。

主要代码入口：

```text
cmd/tradar/          进程配置和生命周期
internal/            目录、采集、趋势、分析、研究、日报、HTTP 和 SQLite
web/src/             React 界面
web/e2e/             Chromium 完整流程
docs/                规格、验证和 agent-skills 过程记录
```

## 当前限制

- Product Hunt 尚未使用用户账号 Token 做真实联调；GitHub 日历史未在真实冒烟中覆盖。
- 可选模型已用假提供者验证，但没有执行真实 OpenAI 付费调用。
- 首版是本地单用户、单进程应用，没有 TLS、公网部署、账号、多租户、外部告警或托管 CI。
- 人工链接和实践产物链接只保存与显示，服务不会访问其中内容。
- SQLite 适合当前低并发范围；多实例或多用户写入需要重新评估存储方案。
