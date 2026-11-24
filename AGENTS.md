# AGENTS.md — AI 协作规范（Agent Harness 指引）

> 本文件给在本仓库工作的 AI 编码代理（Claude Code / ZCode / Codex 等）看。
> 目标：不读聊天记录也能正确构建、测试、发版，不踩已知的坑。
> 人读的文档见 [README.md](README.md) 与 [docs/](docs/)。

## 0. 项目一句话

七杀 Qisha = AI 自主渗透测试平台（Go 单体后端 + Next.js 静态导出前端 + PostgreSQL 唯一存储）。
planner/worker 双角色多 Agent 自主推进，双图架构（资产图×探索图），三层拦截规则，全程证据留痕，
外加军团调度条令与任务级收尾上限。**仅供授权测试**（README 免责条款）。

**与上游的关系**：七杀 fork 自 [chu0119/Pojun](https://github.com/chu0119/Pojun)（破军）的 v0.4.86，
自 v1.0.0 起独立演进，**不跟随上游发版**。模块路径 `github.com/Maplisky/qisha`，发布源
`Maplisky/qisha`（`selfupdate.Repo`）。Pojun 又接手自已删除的原作者仓库（基线 v0.3.15），
git 历史完整保留。

**改了什么、没改什么（动品牌相关的东西前先看这段）**：

- 改了：产品名（界面、启动横幅、日志前缀、文档）、Go 模块路径、发布源、镜像名（`ghcr.io/maplisky/qisha`）。
- **没改，也不要改**——它们是兼容标识符，改了已部署实例会登不上或读不到旧设置：
  可执行文件名 `pojun`、安装目录 `/opt/pojun`、systemd 服务名、环境变量前缀 `POJUN_*`、
  登录用户名 `POJUN`、cookie 与本地存储键（`pojun_token`、`pojun_*`）、Clash 策略组名 `POJUN-AUTO`、
  给模型看的标记串（`POJUN_WORKER_CHAT`、平台管控前缀）、Release 资产命名 `pojun-<ver>-<os>-<arch>.zip`。
- `docs/` 里的文档是上游 Pojun 时期的设计文档与阶段快照，里面的「破军」「Pojun」指的就是本项目的前身。
- 页签图标不走 `web/src/app/icon.png` / `favicon.ico` 这类静态文件约定——那会写进每一页的 `<head>`，包括未登录的
  登录页（上游的 `favicon.ico` / `icon.png` 就是这样被带到每个页面的）。匿名页不引用任何产品图像；登录后由
  `web/src/app/(main)/_components/tab-icon.tsx` 挂上 `/logo.png`。换图标改 `web/public/logo.png`
  （`scripts/gen-logo.py` 生成），不要把图标文件加回 `app/` 目录。

## 1. 常用命令

```bash
# 后端构建（两条都要能过）
go build ./...

# 前端：类型检查 + 静态导出（注意：必须在 web/ 目录下跑；NEXT_EXPORT=1 是 Git Bash 语法，
# 不要用 `npm run build:static`——Windows cmd 下该 script 里的环境变量语法会挂）
cd web && npx tsc --noEmit
cd web && NEXT_EXPORT=1 npx next build

# 全量测试：连真 PG（Docker），-p 1 不能去（跨包死锁），先给新鲜库（共享库跑多轮会撞
# 固定名 fixture 的唯一键，产生假失败）
docker run -d --name pojun-audit-pg -e POSTGRES_PASSWORD=pojuntest -e POSTGRES_USER=pojun \
  -e POSTGRES_DB=pojun -p 15432:5432 postgres:16-alpine
docker exec pojun-audit-pg psql -U pojun -d postgres -c "DROP DATABASE IF EXISTS pojun_ci; CREATE DATABASE pojun_ci OWNER pojun"
POJUN_PG_DSN="postgres://pojun:pojuntest@127.0.0.1:15432/pojun_ci?sslmode=disable" go test -p 1 ./... -count=1

# 发版构建（本机交叉编译验证用；正式发版靠 CI）
POJUN_SKIP_FRONTEND=1 POJUN_SKIP_NPM_CI=1 POJUN_COMPRESS=0 POJUN_PACKAGE=0 \
  POJUN_OUTPUT=/abs/path/pojun ./build.sh --target linux/amd64
```

## 2. 测试基线（判定 FAIL 是否与你有关）

- **已知负载性 flaky**（单跑必过，全量偶发，勿当回归）：
  `TestChatMentionWorkerReceivesServerDetails`、`TestNotifyDigestSegmentsAndDefersRemainder`（共享库脏数据）、
  `TestChatUnavailableReasonDistinguishesStates`（Windows TempDir 清理竞态）、
  `TestCoreTaskLifecyclePG`（目标拆解是异步的，只等 3 秒；前序测试遗留的任务循环抢 CPU 时会超时）、
  `TestTaskTemplateHTTPCRUD`（TempDir 清理竞态，同上）。
- **依赖本机装了哪些宿主工具**（与改动无关）：`hosttools.TestSummaryStaysCompact` 与
  `agent.TestWorkerToolListStaysCompact` 断言工具清单的体积预算，预算是按「只装了一部分工具」定的；
  把宿主工具全部装齐（90 件）的机器上这两个用例必然失败（已在未改动的基线代码上对照确认）。
  CI runner 不装这些工具，按设计不会触发——v1.0.0 的 ci 已在 GitHub runner 上全量通过，这一点已验证。
- **判定法**：单跑，过 = 环境问题；仍失败再用未改动的基线代码在同样条件下跑一遍对照。
  全量测试前先给新鲜库（见上），并用独立的测试库——**不要指向正在使用的实例**。
- **Windows 特有**：`evidence` 目录 fsync 已做 GOOS 分支；不要引入依赖目录 fsync 的新代码。
- **前端 lint**：仓库的 CI 门禁只有 tsc + build，上游文件并没有按当前 Biome 版本格式化。
  所以不要对整个文件跑 `biome check --write`（会把大量无关的上游代码行一起改掉）；
  只保证自己新增的文件干净、自己新增的行没有新告警。

## 3. 发版流程（tag → CI → Release）

1. `CHANGELOG.md` 补节（**编辑前确认锚点存在**——历史上 str.replace 锚点失配静默吞过整节）。
   标题必须是 `## [X.Y.Z] - 日期`：发版流程会从更新日志里提取 `## [X.Y.Z]` 到下一个二级标题之间的内容
   当发布说明，**找不到这一节就直接让发版失败**。上游 Pojun 的历史记在分隔线之后，不要混进去。
2. commit → push main → `git tag vX.Y.Z` → push tag → GitHub Actions 自动出各平台 zip + SHA256SUMS + Docker + Release。
   版本号必须是纯净三段式（见第 4 节）。镜像发布到 `ghcr.io/maplisky/qisha`（全小写）。
   macOS 安装冒烟只在推 main 且改动涉及 install.sh 时触发，并依赖已发布的 Release：这种推送要等标签的
   Release 出来后再推 main，或事后 `gh workflow run macos-install-smoke.yml`，否则它会先红一次。
3. CI 门禁与本地等价：`go build ./...`、`go test -p 1`（真 PG）、前端 tsc + `NEXT_EXPORT=1 next build`。
4. **提示词改动会随 `SeedPromptVersioned` 自动下发**已部署实例（用户手改过的不受影响）——改 worker/planner 默认模板不用写迁移。
   规划者的「多目标调度军规」就在 `agent/planner.go` 的内置模板里；改它等于给所有未手改提示词的实例换调度规则，慎重。
5. **拦截规则新增走 seed 批次**（v1…v5 先例）：除 `seedBuiltinInterceptRulesBatch` 外**必须**配按名补种 + enabled 工具集合并（v4/v5 有真实升级路径缺陷教训：flag 早退会吞掉新规则）。
6. **内置资产怎么加**（三者机制不同）：
   - 新技能：目录放进 `skills/`，并在 `db/db.go` 的 `builtinSkillVisibility` 登记默认可见角色
     （启动时按「不存在才插入」写绑定，不会覆盖用户改过的开关）。**只给文件不登记 = 没有任何角色能看到它。**
   - 任务模板：往 `db/task_playbook_seed.go` 加**新批次**（新的 settings flag）。已部署实例的旧批次 flag 早已置 done，
     往旧列表里追加是发不出去的；同名模板已存在时跳过，不覆盖。
   - 提示词：见上一条。
7. **Agent 引擎依赖是入库的**：`third_party/norma`（Apache-2.0，原样取自 v0.4.4），`go.mod` 用 `replace` 指向它。
   不要直接改那个目录里的代码；升级方式见 `third_party/README.md`。它是独立模块，`./...` 不会匹配到它。
8. git push 对 github.com 常因本机代理抖动失败——`git -c http.proxy= -c https.proxy= push` 直连 + 重试循环（30-45s 间隔）+ `git ls-remote` 确认落地。
   大仓库整包传输容易中途断开（`early EOF`）：分步取比一次 `--unshallow` 稳——`--deepen=10` 起步、成功再加大，
   20 层以上的大步必卡（2026-11-25 实测：10 层约 6 秒，20/40 层卡死 90~150 秒）。
   **整仓历史级别的推送直连推不上去**（传到一半 TLS 被掐，随后几分钟连不上 443）：WSL 里 `git bundle create` →
   Windows 侧导入裸仓库 → Windows git 经本机 443 加速器**分段推**（每段 40~120 个提交，秒级完成），先推到临时分支，
   对象齐了再推 main 与标签，最后删临时分支。WSL 里用不了那个加速器（网关 443 被拒）。
9. **别把上游的 v0.4.x 标签带进本仓库**：发版流程按 `v*` 标签触发，`git fetch upstream --tags` 后再 `push --tags`
   会给上百个旧版本各建一个 Release。只推显式 refspec（`main`、`vX.Y.Z`）。

## 4. 关键坑（真实踩过的）

- **默认构建不含前端**：`go build ./cmd/pojun` 出的是 stub（页面报「前端未内嵌」）。内嵌必须
  `-tags embedui` 且先 `cp -r web/out server/webui/dist`。别把无 tags 的现场编译版当正式版部署。
- **dev 版本号禁用一键更新**：`CompareVersions` 只认纯净三段式；`0.3.95-dev` 这类会被判不可比较。
  本机交叉编译验证时传 `POJUN_BUILD_VERSION` 要注意。
- **next build 的 cwd 必须是 `web/`**——曾在 `web/src` 下误跑，产物 `.next` 被 `git add -A` 带进仓库。
- **短 sleep 等定时器的测试不许写**：`1ms 超时 + sleep 2ms 断言` 在 CI 高负载翻车（本地必过）。
  用有界轮询。
- **工具实现了必须注册**：`agent/tools_*.go` 里的领域工具要进
  `PlannerTools/WorkerTools/MainAgentTools` 三张清单（tools.go / tools_insert.go），
  还要考虑 `agent/toolcatalog.go` 的 BuiltinToolSeeds。listener（v0.3.87）和
  list_worker_traces（v0.3.89）都栽过「实现了没注册」。
- **静态导出不支持服务端 redirect()**：会被静默丢弃、路由导出成 404 页。用客户端
  `router.replace`（v0.3.88 根路由修复先例）。
- **纯读工具用 readTool/readExpTool**：writeTool 会丢 ReadOnly/Concurrent 标记，被调度串行化。
- **不要在对话/报告里写**：FOFA key、LLM API key、任何密码明文——凭据只进运行时配置。

## 5. 目录速览

```
cmd/            入口（pojun / pojun-hostd 特权助手 / pojun-maint 维护工具）
agent/          多 Agent 层（planner/worker/mainagent 提示词与工具；三张工具清单在 tools.go/tools_insert.go）
db/             PostgreSQL 唯一存储（schema.sql 幂等迁移；拦截规则 seed 批次 v1-v5）
server/         HTTP 面与引擎调度（全部路由注册在 server.go:680-1000 一带）
intercept/      三层拦截（规则+LLM judge+ask 审批；judge prompt 在 prompt.go）
listener/       回连接收器；guard/        审批门（Attributions 已回流 planner 态势）
traffic/ vulnintel/ llmpool/ selfupdate/ report/（手写 OOXML，勿引第三方文档库） skills/
web/            Next.js 前端（src/app 路由；lib/api.ts 单一 API 封装）
docs/           设计文档 + 阶段报告（按日期命名的是阶段性快照，越新的越权威）
```

## 6. 文档权威顺序（同一话题以新为准）

`docs/深度优化方案-2026-10-31.md`（当前路线图）> `docs/产品全面体检-2026-10-31.md`（缺陷基线）>
`docs/模型路由策略.md`（模型选型）> `docs/红队能力建设-2026-10-30.md` > `docs/产品优化清单-2026-10-29.md`
（历史，35% 完成度快照）。部署与排障：`docs/部署手册.md`（含 LAN 源安装配方）。
接手背景：`docs/项目交接文档.md`。
