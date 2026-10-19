# TRadar

个人技术趋势与商业机会雷达：发现正在增长的项目，保存可追溯证据，形成研究判断和行动。

**当前状态：首版实施中。项目目录、不可变证据、三来源采集、趋势雷达、人工与规则分析、个人研究记录、认知库、实践验证和可回溯日报已经实现。**

- [能力图与首版范围](CAPABILITY-MAP.md)
- [首版详细规格总览](docs/spec-overview.md)
- [共享接口、技术栈及验收约定](docs/spec-common.md)
- [信息源可行性](docs/source-feasibility.md)
- [项目工程约定](AGENTS.md)
- [实施任务与当前进度](tasks/todo.md)
- [已执行的验证记录](docs/verification.md)
- [agent-skills 工程过程记录](docs/process/README.md)
- [开发机技能安装与验证](docs/codex-skills-setup.md)
- [固定的 agent-skills 工程规范](engineering/agent-skills/README.md)
- [上游版本与文件校验信息](engineering/agent-skills/UPSTREAM.json)

工程方法来自用户指定的 [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)，
采用项目内固定版本副本并保留 MIT 许可。实现按规格、任务拆分、TDD、增量提交和交付审查推进。

当前可用路径：从空库人工记录项目，搜索并查看详情，编辑时使用版本冲突保护；在详情页保存
私密或公开证据，并通过追加修订保留旧版本。来源设置及采集任务 API 可持久化配置、幂等意图和
检查点。来源设置页可保存配置、手动触发任务并查看运行状态；后台运行、预算、调度、GitHub/HN
真实小流量冒烟及 Product Hunt GraphQL fixture 已验证。Product Hunt 真实采集仍需用户自己的 Token，
缺少凭据时会明确显示不可用，不会回退成演示结果。
项目详情可生成四问初筛或六维深研规则版本，也可人工修订；事实必须引用证据，历史版本会冻结
证据和趋势上下文。私密证据只供本地规则与人工分析使用，当前模型分析保持关闭。
项目详情可保存收藏、标签、判断依据、个人笔记及下一步；认知库支持搜索和筛选，设置页可调整
关注方向与 3/2/1 研究建议。并发编辑会展示服务器最新版本，同时保留本地草稿供手工合并。
实践验证可记录假设、方法、时间预算、结果、结论、项目证据和产物链接；链接只保存和显示，
系统不会访问或执行其中内容。完成状态必须有真实结果与结论，未保存内容和版本冲突均会保留。
每日简报只收录首次覆盖或发生实质变化的项目，按生成时刻冻结来源、证据、趋势和个人判断；
支持同日多版本、历史阅读、安全 Markdown 下载和可编辑时区调度，停机后只补当天一次。
人工链接只做校验和保存，服务端不会自动访问。

本机需要 Go 1.25 和 Node.js 22。首次运行：

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/tradar ./cmd/tradar
./bin/tradar
```

访问 `http://127.0.0.1:8080/radar`。默认数据库为 `data/radar.db`，默认没有演示数据，
也不会联网采集。服务默认只监听回环地址；远程使用建议建立 SSH 隧道。

当前验证入口：

```sh
go test -race ./...
go vet ./...
npm --prefix web run test -- --run
npm --prefix web run build
npm --prefix web run test:e2e
```
