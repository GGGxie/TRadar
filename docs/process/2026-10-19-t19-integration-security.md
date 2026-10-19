# T19 集成与安全：agent-skills 过程记录

- 记录日期：2026-10-19（Asia/Shanghai）
- 任务来源：`tasks/todo.md` 的 T19
- 状态：已完成
- 实现与验收提交：本记录所在的 T19 提交

## 1. 背景与边界

T19 不增加业务能力，而是验证 T00–T18 能作为一个整体安全运行。检查范围包括全量测试、race/vet、
生产构建、真实 Chromium 闭环、HTTP 输入与授权、SQLite 迁移和重启、外部网络边界、模型信任边界、
秘密处理、依赖完整性及已知漏洞。

本轮没有启用真实模型、Product Hunt Token、公网部署或外部通知。漏洞工具只分析本仓库依赖，
不上传源代码，也不进入产品运行依赖。

## 2. 采用的技能

| 技能 | 采用原因 | 实际读取文件 |
|---|---|---|
| code-review-and-quality | 用正确性、可读性、架构、安全、性能五轴审查完整变更，并验证测试会捕获回归 | `engineering/agent-skills/skills/code-review-and-quality/SKILL.md` |
| security-and-hardening | 系统含 HTTP、外部采集、令牌、SQLite 和 LLM 信任边界，需要 threat model 与安全清单 | `engineering/agent-skills/skills/security-and-hardening/SKILL.md` |
| debugging-and-error-recovery | 扫描或全量检查失败时保留证据，定位根因，补防回归后再恢复 | `engineering/agent-skills/skills/debugging-and-error-recovery/SKILL.md` |

同时读取 `engineering/agent-skills/references/security-checklist.md`。认证账号、密码重置、多租户授权、
PII 保留和文件上传不属于这个本地单用户首版，标为不适用；没有把它们伪装成已实现。

## 3. 规格到审查切片

1. 锁定基线：确认工作树、Go/Node 版本、锁文件及唯一安装边界。
2. 运行全量 race/vet、组件测试、生产构建、Chromium 和两套依赖审计。
3. 审查 HTTP 请求体、Host、Origin、Bearer、非回环监听、安全头和内部错误响应。
4. 审查三个固定采集端点及模型端点的 SSRF、重定向、凭据、大小和超时限制。
5. 审查 SQLite 权限、迁移事务、外键、并发、重启中断与默认空数据。
6. 搜索 raw HTML、eval、shell 执行、动态 SQL、秘密模式和多个前端锁文件。
7. 对发现项先保留失败证据，再做最小修复、聚焦验证、变异验证和全量回归。

## 4. RED / GREEN / 变异记录

### 标准库漏洞 RED

最初由宿主 Go 1.25.6 构建生产二进制，使用官方 `govulncheck` 数据库做 binary mode 扫描。
扫描报告 26 个从二进制符号看可达的标准库漏洞，涉及 `net/http`、TLS、URL、MIME 等路径，退出码为 3。
Go 官方发布记录显示 Go 1.26.9 于 2026-10-08 发布并包含 go command、`crypto/tls`、
`html/template`、`net/http`、`net/textproto` 和 `os` 的安全修复，因此旧工具链不能继续作为交付基线。

### 工具链升级 GREEN

把 `go.mod` 的最低版本固定为 1.26.9，通过 Go 自动工具链下载官方工具链，重新运行 race、vet、
生产构建和模块校验。对新二进制再次扫描，结果为 `No vulnerabilities found.`。

源码模式扫描在当前小内存开发机被系统以退出码 137 终止。没有把资源失败写成通过；改用官方支持的
binary mode 作为交付门禁。该模式不能提供源码调用栈，而且可能按符号可达性高估风险，所以保留这一
限制。扫描器采用 `golang.org/x/vuln/cmd/govulncheck@v1.7.0`：当前宿主最初只能安装这一兼容
Go 1.25 的官方版本；当时最新 v1.9.0 要求 Go 1.26。扫描数据库在最终运行时重新联网刷新。

### 非回环访问保护 RED / GREEN

先加入 `TestValidateServerAccessRequiresLongTokenOffLoopback`，测试引用尚不存在的函数，编译以
`undefined: validateServerAccess` 失败。随后提取启动规则：地址必须是 `host:port`；IPv4/IPv6
回环和 localhost 可保持本地免令牌；其他监听地址要求至少 32 字符令牌。表驱动测试覆盖有效和无效边界。

HTTP 聚焦测试补充三种 Bearer 情形，并证明没有 Origin、但浏览器声明
`Sec-Fetch-Site: cross-site` 的写请求仍返回 403。

按 code-review-and-quality 的要求做变异实验：临时把令牌长度条件改成永不成立，聚焦测试立即在
“公网缺少令牌”和“公网短令牌”两例失败；恢复 `< 32` 后同一测试及全量 race 均通过。这证明测试能
阻止真实安全回归。

## 5. Threat model 与审查结果

| 信任边界 / 资产 | 主要风险 | 已验证控制 |
|---|---|---|
| 浏览器与 HTTP 服务 | 跨站写入、伪造 Host、超大或歧义 JSON、未授权远程访问 | 回环默认、非回环强令牌、常量时间比较、Host/Origin/Fetch Metadata、JSON 类型、1 MiB、拒绝未知及尾随字段 |
| GitHub/HN/Product Hunt | SSRF、DNS 重绑定、重定向泄露凭据、无限响应/等待 | 仅三个固定 HTTPS 主机、拒绝私网/保留地址、连接固定到已校验 IP、同域一次重定向并删除授权、2 MiB、请求/任务超时 |
| OpenAI 可选提供者 | 泄露密钥/私密证据、工具调用、恶意或超大输出 | 默认关闭、固定 HTTPS 端点、禁重定向、只选公开证据、无 tools、严格 schema、本地引用校验、1 MiB、预算和超时 |
| SQLite 与本地文件 | 部分迁移、并发损坏、旧任务误重放、权限过宽 | 目录 0700/新库 0600、迁移事务、外键、WAL/busy timeout、单连接、版本前向拒绝、启动中断标记 |
| 环境与日志 | Token/API key 入库、回传、提交或日志 | `.env*`、key/pem 忽略；能力接口只返回存在状态；通用 500；扫描当前树及 Git 历史未发现令牌模式 |

代码搜索没有发现 `dangerouslySetInnerHTML`、直接 `innerHTML`、eval 或 shell 执行。SQL 中只有受控的
排序/筛选片段，用户数据继续使用绑定参数。安全响应头包含 CSP、frame deny、nosniff、
Permissions-Policy 与 no-referrer。静态文件路径清理并拒绝点段；首版没有上传路径。

前端只有 `package-lock.json` 一个安装真相，`packageManager` 固定 npm 10.9.8，`.npmrc` 禁止安装
脚本；唯一声明安装脚本的包是可选的 macOS `fsevents`。`npm audit signatures` 验证 115 个 registry
签名和 39 个 attestation。`npm audit --offline` 为 0 条已知漏洞，`go mod verify` 全部通过。

`npm outdated` 与 `go list -m -u all` 显示若干可用更新，但没有形成漏洞发现。本轮保留已验证的精确
锁定版本，把 React、TypeScript、jsdom、SQLite 等兼容性升级留给独立任务，避免把安全修复和批量升级
混在一个不可归因的变更里。

## 6. 验证证据

最终实际执行并通过：

```sh
GOTOOLCHAIN=go1.26.9+auto go test -race ./...
GOTOOLCHAIN=go1.26.9+auto go vet ./...
GOTOOLCHAIN=go1.26.9+auto go build -trimpath -o /tmp/tradar-vuln-scan-go126 ./cmd/tradar
GOTOOLCHAIN=go1.26.9+auto go mod tidy -diff
GOTOOLCHAIN=go1.26.9+auto go mod verify
/tmp/tradar-tools/govulncheck -mode binary /tmp/tradar-vuln-scan-go126
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
npm --prefix web audit --offline --json
npm --prefix web audit signatures
```

结果：Go 全包 race/vet 通过；模块图无需 tidy 且校验和有效；新二进制漏洞为 0；Vitest 9 个文件、
16 个测试通过；Vite 生产构建通过；Chromium 7/7 通过；npm 已知漏洞为 0，签名和 attestations 有效。

## 7. 产物与剩余限制

本任务产物包括 Go 1.26.9 安全基线、可单测的非回环访问规则、Bearer/跨站回归测试、正式验证记录和
本过程记录。真实 Product Hunt 与 OpenAI 调用继续需要用户自己的凭据；公网发布、TLS 终止、账号系统、
外部告警和 CI 托管不在首版授权范围。binary mode 漏洞扫描应在交付或依赖变更时用最新官方数据库重跑。
