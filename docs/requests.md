# 需求评审记录

新需求追加在最上面。格式：需求 → 价值 → 成本/风险 → 结论。流程见 `CLAUDE.md`。

---

## 2026-11-22 · 部署时卡死（bug）

**现象**：Claude 执行 `deploy/install.sh` 后停在 live-check，界面像卡死。

**原因**：服务器重启后若第一个 tmux 会话是从网页新建的，tmux server 由 hub 拉起，落在 `claude-hub.service` 的 cgroup 里；`systemctl restart` 默认 `KillMode=control-group` 会把它连同全部会话（包括正在部署的 Claude）一起杀掉。已用独立 socket + 临时 unit 复现。

**结论**：直接修（bug）。服务改 `KillMode=process`；`install.sh` 重启前后核对 tmux 会话名单，不一致就报错；`live-check.sh` 检查 KillMode。**状态**：已完成。

---

## 2026-11-21 · 参考 NormanSMA/ClaudeHub，吸收好用的功能

**需求**：用户发现 https://github.com/NormanSMA/ClaudeHub （MIT，Claude Code 用量看板 + 桌面吉祥物），让评审哪些功能值得加进来。

**对方是什么**：读取本机 `~/.claude/projects/**/*.jsonl` 统计 token/费用（按天/模型/会话/项目）、每个活跃对话的上下文占用与告警、套餐 5 小时/每周额度（靠 statusline 记录 `rate_limits`）、主代理 vs 子代理消耗、Claude Code hooks 驱动吉祥物状态（思考/用工具/等授权/出错）、桌面「灵动岛」浮窗。只监听 127.0.0.1，Windows 安装包，界面西班牙语。

**逐项结论**：

| 功能 | 价值 | 成本/风险 | 结论 |
|---|---|---|---|
| **Hooks 精确状态**：Claude Code 的 UserPromptSubmit / PreToolUse / Notification / Stop 等 hook 把事件发给 hub | 高。我们现在靠读屏幕上的 `esc to interrupt` 判断，Claude Code 改文案就失灵；hook 还能区分「思考 / 用工具 / 等你授权」。hook 进程继承 `TMUX_PANE`，能精确对应到哪个 tmux 会话 | 中。需在 `~/.claude/settings.json` 加 hooks（必须经用户同意，skill 里做，且不覆盖用户已有设置）；没装 hook 时退回读屏 | **做（第一优先）** |
| **每个会话的上下文占用 %** + 快满时提醒 | 高。侧栏直接看到哪个对话快要满、该 /compact 了 | 中。hook 给出 `transcript_path`，读最后一条 assistant 的 usage 计算；依赖上一项 | **做（随上一项）** |
| **今日用量统计**（token 按天/模型/项目，折合 API 价值） | 中。订阅用户关心「今天用了多少」 | 中。全量扫描 jsonl 要做增量读取；价格表需要维护 | **做，简化版**：新增「用量」标签，只做 token 统计；费用折算放后面 |
| 套餐 5 小时/每周额度 | 中高 | 高。只能靠 statusline 拿 `rate_limits`，会和用户自己的 statusline 冲突 | **暂不做**，记下来以后再评 |
| 宠物状态跟随 hook（等授权时着急、出错时难过） | 中，好玩 | 低（有了 hook 后只是前端） | **做（随第一项）** |
| 桌面浮窗 / 灵动岛（不开主窗口也能看到宠物和会话状态） | 中，挺吸引人 | 中高。要改 Mac App，用户需重装；Tauri 多窗口+置顶 | **以后做**，单独评审 |
| Codex / Gemini 等其他来源、浏览器扩展、Windows 安装包、多语言 | 低（与本项目定位无关） | — | **不做** |

**做法约束**：参考其思路自己实现（Go 服务端），不直接拷代码；若借鉴了具体算法，在 README 致谢并保留 MIT 署名。

**状态**：用户确认做前三项（hooks 精确状态、上下文占用、宠物跟随状态）。已实现：`claude-hub hook` 客户端（只转发事件名/工具名/会话 ID，TMUX_PANE 对应会话）、`claude-hub hooks install|uninstall|status`（备份、只增删自己的条目）、Esc 打断不触发 Stop 时由读屏纠正、上下文 >200k 推断 1M 窗口、85% 提醒、宠物「着急」表情。修改用户 `~/.claude/settings.json` 必须由用户本人执行（auto mode 也会拦截 Claude 自己改）。用量统计页、桌面浮窗留待以后。

---

## 2026-11-21 · 开源 + 一个 skill 能装好 + 规范开发流程

**需求**：项目要开源；用户装一个 skill 就能用；用 GitHub 升级；全程由 Claude 维护，每个需求先审核、做了就要测试和实测，没问题才推送。

**结论**：做。插件 `hub-for-claude-code`（skill `hub`：装服务端/装 Mac App/升级/排障/卸载）；`CLAUDE.md` 写明流程；`scripts/check.sh`（总闸）+ pre-push 钩子 + `scripts/live-check.sh`（线上实测）+ `scripts/mac-check.sh`（Mac 实测）；GitHub Actions：CI + 打 tag 自动发布通用版 Mac App。插件名不能用 `claude-` 开头（官方保留），用户决定项目仍叫 Claude Hub、插件叫 `hub-for-claude-code`，README 注明非官方。

**状态**：已完成。

## 2026-11-21 · Mac 上的 Clash 代理导致白屏

**结论**：App 内加 127.0.0.1:17788 TCP 转发，webview 不走系统代理；浏览器使用者在代理里加绕过 `100.64.0.0/10,*.ts.net`（skill 的排障表里有）。**状态**：已完成。
