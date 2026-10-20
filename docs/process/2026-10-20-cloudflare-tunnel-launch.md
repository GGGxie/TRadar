# Cloudflare Tunnel 临时运行：agent-skills 过程记录

- 记录日期：2026-10-20（Asia/Shanghai）
- 任务来源：[GitHub Issue #28](https://github.com/GGGxie/TRadar/issues/28)
- 状态：已完成；临时服务运行中
- 对应提交：本记录所在提交

## 1. 背景与边界

本次目标是启动当前 `feature/tradar-core` 分支的 TRadar，并提供一个可从外部浏览器访问的
Cloudflare Tunnel HTTPS 地址。它是临时预览，不改变产品代码，也不把首版描述为已经具备正式公网部署能力。

启动前确认：仓库没有 `.env`，`data/radar.db` 不存在，因此本次从空库启动；没有向进程传入 GitHub、
Product Hunt 或模型凭据。应用只监听 `127.0.0.1`，没有直接开放主机端口。

Quick Tunnel 没有账号登录保护。任何获得随机地址的人都能访问和写入这个空实例，因此本次地址只用于临时预览，
不应录入私密证据、个人研究或其他敏感内容。停止 `cloudflared` 后随机地址失效。

## 2. 采用的技能

### `shipping-and-launch`

- 读取文件：`engineering/agent-skills/skills/shipping-and-launch/SKILL.md`
- 采用原因：任务包含构建、启动、外部入口、健康检查、故障恢复和运行限制。
- 实际应用：先验证构建和环境，再建立最小范围 Tunnel；分别检查本地健康、外部健康、页面和写入链路；
  保留停止 Tunnel 与应用两个进程的回滚路径。

### `security-and-hardening`

- 读取文件：`engineering/agent-skills/skills/security-and-hardening/SKILL.md`
- 采用原因：公网入口把浏览器和 Cloudflare 边缘变成新的信任边界。
- 实际应用：不把应用绑定到非回环地址；不加载外部服务凭据；避免复用现有多域名 Tunnel；
  使用精确的外部 Origin，并验证 CSP、`X-Frame-Options`、`nosniff` 等响应头仍存在。

### `git-workflow-and-versioning`

- 规范文件：`engineering/agent-skills/skills/git-workflow-and-versioning/SKILL.md`
- 采用原因：过程记录和索引需要形成可审查、可回滚的独立提交。
- 实际应用：运行产物和 SQLite 数据保持在 Git 忽略范围，只提交本过程记录及索引；提交前检查差异和工作区。

## 3. 从目标到运行切片

1. 搜索开放 Issues，确认没有重复任务，然后建立 #28。
2. 盘点 Cloudflare、Go、Node、构建产物、环境文件和数据库状态。
3. 执行 `make build`，前端类型检查与 Vite 构建通过，Go 生产二进制构建成功。
4. 为避免启动已有配置中的多个域名入口，使用独立空配置创建仅指向 TRadar 的 Quick Tunnel。
5. 应用监听 `127.0.0.1:18080`；Tunnel 把源站 Host 固定为 `localhost`，应用只允许本次随机 HTTPS
   地址作为精确 Origin，使浏览器的同源写请求能通过反向代理。
6. 验证本地 `/healthz`、公网 `/healthz`、公网 `/radar` 和不会写入数据的无效 POST。
7. 将运行地址、限制和验证证据回填 #28，再以 `completed` 关闭任务。

## 4. RED / GREEN 运行证据

本次不改变业务逻辑，单元测试的 RED/GREEN 循环不适用，改用可观察的运行断言。

### RED：默认端口冲突

- 第一次按 README 使用 `127.0.0.1:8080` 启动时返回 `bind: address already in use`。
- 随即停止刚建立的 Tunnel，避免它把流量转发到错误服务。
- 通过监听端口检查确认 8080 由现有 nginx 占用；没有停止或修改该服务。

### GREEN：隔离端口与外部链路

- 改用空闲的 `127.0.0.1:18080` 后，TRadar 成功监听。
- 本地 `GET /healthz`：HTTP 200，响应为 `{"data":{"status":"ok"}}`。
- 公网 `GET /healthz`：HTTP/2 200，经过 Cloudflare 后响应内容一致。
- 公网 `GET /radar`：HTTP/2 200，返回当前构建的中文 TRadar 页面和带摘要的静态资源。
- 公网无效 `POST /api/v1/projects`：HTTP/2 422，返回应用的 `VALIDATION_ERROR`；没有创建数据。
  这证明请求已通过 Tunnel 和 Origin 检查到达业务校验层。

## 5. 关键决策与修正

### 不复用现有命名 Tunnel

主机已有 Cloudflare 配置，包含 `crazya.gggxie.com`、`skills.gggxie.com` 和 `reddit.gggxie.com`
等多个入口。启动它会同时影响不属于本任务的服务，因此本次用独立 Quick Tunnel，只转发 18080。

### 保持回环监听

Cloudflare 连接主动从本机发起，TRadar 无需监听 `0.0.0.0`。这保留了主机防火墙边界，并避免直接暴露
未加 TLS 的应用端口。

### 明确临时公网风险

TRadar 的 Bearer Token 适用于 API 客户端，当前前端不会自动附带该 Token；直接启用会让页面本身无法加载。
本次数据库为空，用户又明确要求 Tunnel 预览，所以保留无登录的临时地址，并把“链接持有者可读写”作为已知限制。
正式长期使用需要命名 Tunnel 加 Cloudflare Access，或先为产品补充浏览器登录流程。

## 6. 运行与验证证据

| 项目 | 结果 |
|---|---|
| GitHub 任务 | [#28](https://github.com/GGGxie/TRadar/issues/28) |
| 构建 | `make build` 通过 |
| 应用监听 | `127.0.0.1:18080` |
| 公网入口 | `https://field-interim-administrators-watching.trycloudflare.com/radar` |
| 本地健康 | HTTP 200 |
| 公网健康 | HTTP/2 200 |
| 公网页面 | HTTP/2 200 |
| 写入链路探测 | HTTP/2 422，预期字段校验，无数据写入 |
| 外部凭据与模型 | 未启用 |

本次没有重复运行完整 Go、Vitest 或 Chromium 套件：产品代码没有变化，构建已经执行类型检查；运行风险由本地、
代理和业务边界四项冒烟检查覆盖。最近一次完整发布验证仍记录在 `docs/verification.md`。

## 7. 生命周期与剩余限制

- Quick Tunnel 没有正常运行时间保证；主机、TRadar 或 `cloudflared` 任一进程停止后地址都会失效。
- 当前入口无登录保护，只适合空库临时预览。
- 停止时先向 `cloudflared` 发送中断信号使公网地址失效，再向 TRadar 发送中断信号完成数据库关闭。
- 如需稳定域名和长期运行，应另建 GitHub Issue，配置单一命名 Tunnel、Cloudflare Access、进程托管、
  监控与备份，不沿用本次随机地址。
