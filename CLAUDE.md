# Claude Hub — 开发约定（给 Claude 看的）

这个项目全程由 Claude 维护，用户只提需求。下面的流程**每次都必须完整执行**，不能因为改动小就跳过。回复用户用中文。

## 项目是什么

在 Mac 上（经 Tailscale）使用服务器 tmux 里的 Claude Code：会话列表/新建/关闭、终端、发图片，附带德州扑克和随 Claude 工作成长的宠物。**所有流量从服务器发出，Mac 端只是界面。** 开源（MIT），社区项目，不是 Anthropic 官方产品。

```
Mac: Claude Hub.app (Tauri 壳) ──Tailscale──> 服务器 claude-hub (Go, :7788)
     webview → 127.0.0.1:17788 (App 内 TCP 转发，绕开 Clash 等系统代理) → 服务器
```

| 目录 | 内容 |
|---|---|
| `server/` | Go 1.22，无框架。`main.go` 路由/鉴权/上传；`internal/tmux` 会话管理与状态识别（有 hooks 时以 hook 事件为准、读屏纠正；没有时读屏：`esc to interrupt` = 工作中）；`internal/hooks` 是 `claude-hub hook` 客户端与 settings.json 安装器；`internal/usage` 增量统计 `~/.claude/projects` 里的 token 用量；`internal/term` pty 跑 `tmux attach` 转 websocket；`internal/poker` 德州引擎+机器人；`internal/pet` 宠物；`internal/hub` 事件广播 |
| `web/` | Vite + React + TS，编译进 `server/webdist` 再 `go:embed`。界面改动只需重新部署服务器，Mac 端 ⌘R 即可 |
| `mac/` | Tauri v2 壳：设置页 + 本地转发 + 原生通知。**尽量不改**——能在 server/web 做的就不要放进 App（App 更新要用户重装） |
| `e2e/` | Playwright 端到端测试：真服务器 + 独立 tmux socket + 假 `claude`（`e2e/bin/claude`） |
| `plugin/` + `.claude-plugin/` | Claude Code 插件 `hub-for-claude-code`，skill `hub` 负责安装/升级/排障 |
| `scripts/` | `check.sh` 总闸、`live-check.sh` 线上实测、`mac-check.sh` Mac 实测 |
| `docs/requests.md` | 需求评审记录 |

安全模型：只监听 Tailscale IP + 127.0.0.1；token（`data/token`）放 HttpOnly cookie；写操作要求 `X-Hub: 1` 头（挡跨站表单）；websocket 校验 Origin。任何改动不得削弱这些。

## 流程：一个需求从提出到推送

### 1. 评审（写代码之前）

在 `docs/requests.md` 顶部追加一条，然后**把结论告诉用户，等用户确认再动手**（纯 bug 修复、文档错字可以直接做，但仍要记录）：

- 需求原话与理解；不清楚就先问
- 价值：解决谁的什么问题
- 成本与风险：改哪些模块、是否要改 Mac App（用户需重装）、是否影响安全模型、是否加依赖、开源用户是否受影响
- 结论：**做 / 不做 / 换个做法做**，以及理由

默认拒绝或要求换做法的情况：让 Mac 直连外部服务、把服务暴露到 tailnet 之外、在仓库里写死个人信息（主机名、用户名、IP、token）、为小功能引入大依赖、只对维护者本人有用却让所有开源用户承担成本（这种做成可配置项）。

### 2. 实现

- 遵循现有结构和风格；Go 改完 `gofmt`；界面文字目前只有中文。
- 行为改动必须配测试：逻辑放单元测试（`server/internal/*/..._test.go`），用户可见流程放进 `e2e/e2e.mjs`。
- 不要动用户正在用的 tmux 会话：测试一律用 `-tmux-socket` 独立 socket；线上实测只操作自己新建、名字带 `selftest` 的会话，测完关掉。

### 3. 本地总闸

```bash
scripts/check.sh      # gofmt、vet、go test -race、tsc、构建、e2e；必须 ALL CHECKS PASSED
```

失败就修，不许跳过或删测试凑通过。界面有改动时打开 `e2e/out/*.png` 亲眼看一遍。

### 4. 实测（必须，每次）

在开发服务器上部署并验证真实环境：

```bash
deploy/install.sh && scripts/live-check.sh     # 必须 LIVE CHECK OK
```

再按改动范围补做：

- **动了 tmux/终端/上传**：用 API 新建一个 `selftest` 会话（真 claude），上传一张图，`tmux capture-pane` 确认输入框出现 `[Image #N]`，再关掉该会话。
- **动了界面**：用 Playwright 对线上地址截图检查（token 从 `data/token` 读，截图放 scratchpad，不入库）。
- **动了 `mac/`**：`scripts/mac-check.sh`（`MAC_HOST` 从 `.dev.env` 读）——在测试 Mac 上编译、安装、启动，并确认 App 真的连上了服务器。

实测有任何异常：不推送，先修。

### 5. 提交与推送

- `CHANGELOG.md` 的 `## Unreleased` 下写一行用户能看懂的说明。
- 提交信息用英文、祈使句，说清楚做了什么、为什么。
- `git push`（pre-push 钩子会再跑一次 `scripts/check.sh`；首次克隆后执行 `git config core.hooksPath .githooks`）。
- 推送后检查 GitHub Actions；CI 红了立刻修。

### 6. 发版（用户要求发布、或攒够改动时提议）

1. `CHANGELOG.md` 把 `Unreleased` 改成 `## vX.Y.Z — YYYY-MM-DD`
2. 同步版本号：`plugin/.claude-plugin/plugin.json`、`mac/src-tauri/tauri.conf.json`
3. 提交、推送、等 CI 绿 → `git tag vX.Y.Z && git push origin vX.Y.Z`
4. Release 工作流会编译通用版 Mac App 并发布 `Claude-Hub-macos.zip`
5. 只改了 server/web：提醒用户在服务器上 `git pull && deploy/install.sh`；改了 mac：提醒用户重装 App

## 本机信息

个人环境（测试 Mac 的 SSH 地址等）放在不入库的 `.dev.env`：

```bash
MAC_HOST=user@macbook.your-tailnet.ts.net
```

## 常用命令

```bash
./build.sh                                   # 构建（web → webdist → bin/claude-hub）
deploy/install.sh                            # 构建 + 安装/重启 systemd --user 服务
journalctl --user -u claude-hub -f           # 日志
cd server && go test ./internal/poker -run X # 单测
cd e2e && E2E_HEADED=1 node e2e.mjs          # 有界面跑 e2e（需要显示器）
claude plugin validate . && claude plugin validate plugin   # 改了插件/skill 后
```

## 已知坑

- Mac 上的 Clash 等系统代理会把 100.x / `*.ts.net` 发到外网 → 浏览器白屏。App 用 127.0.0.1 转发规避；浏览器要在代理里加绕过 `100.64.0.0/10,*.ts.net`。
- `mac/src-tauri/Info.plist` 里不要加 `NSAllowsLocalNetworking` / `NSAllowsArbitraryLoadsInWebContent`：有它们时 macOS 会忽略 `NSAllowsArbitraryLoads`。
- tmux 用 argv[0] 命名 pane，假 claude 因此要 `exec -a claude`。
- 图片靠 tmux 括号粘贴（`paste-buffer -p`）让 Claude Code 识别成 `[Image #N]`，不要改成 `send-keys`。
- 插件名不能以 `claude-` 开头（官方保留）。
- 用户的 `~/.claude/settings.json` 只能由用户本人改（让用户执行 `! ~/claude-hub/bin/claude-hub hooks install`）；测试里一律用临时 `CLAUDE_CONFIG_DIR`。
- 按 Esc 打断时 Claude Code 不触发 Stop hook，所以 hook 状态要靠读屏纠正（`hookstate.go` 的 `resolve`）。
