<div align="center">

<img src="mac/app-icon.png" width="120" alt="" />

# Claude Hub

**在 Mac 上用服务器 tmux 里的 Claude Code —— 走 Tailscale。**

[English](README.md) · 简体中文

</div>

- **会话**：列出所有跑着 `claude` 的 tmux 会话，实时显示工作中 / 等你确认 / 空闲；「新对话」= 新开一个 tmux 会话。
- **终端**：完整的 xterm.js 终端，Shift+Enter 换行。
- **发图片**：拖进来、⌘V 粘贴截图或点 📎，文件传到服务器后以 `[Image #N]` 贴进 Claude。
- **通知**：Claude 干完活或需要你确认时提醒。
- **可选的 Claude Code hooks**：精确显示思考中 / 运行工具 / 等你授权 / 出错，以及每个对话的上下文占用。
- **用量**：服务器上 Claude Code 今日 token 用量（输入/输出/缓存），按模型、项目拆分，以及最近 7 天走势。只统计 token，不折算费用。
- **德州扑克**：等 Claude 的时候和 4 个机器人打牌；**像素宠物**：Claude 工作时它成长，完成任务奖励筹码。

所有流量从服务器发出，Mac 只负责显示。服务只监听 Tailscale IP 和本机，需要 token 登录。

> 社区项目，与 Anthropic 无关。

## 用 Claude Code 一键安装（推荐）

```
/plugin marketplace add simple-YoungBadBoy/claude-hub
/plugin install hub-for-claude-code@hub-for-claude-code
```

然后在**服务器**上的 Claude Code 里说「在这台服务器上安装 Claude Hub」，在 **Mac** 上说「安装 Claude Hub 的 Mac App」。skill 会检查环境、安装、验证，并告诉你地址和 token。

## 手动安装

**服务器**（Linux + systemd；需要 tmux、Claude Code、Tailscale、Go ≥ 1.22、Node ≥ 18）：

```bash
git clone https://github.com/simple-YoungBadBoy/claude-hub ~/claude-hub
~/claude-hub/deploy/install.sh        # 编译并启动 systemd --user 服务，端口 7788
cat ~/claude-hub/data/token           # 在 App 里输入一次
```

**Mac**：从 [Releases](https://github.com/simple-YoungBadBoy/claude-hub/releases/latest) 下载 `Claude-Hub-macos.zip`，把 App 拖进「应用程序」，再执行 `xattr -dr com.apple.quarantine "/Applications/Claude Hub.app"`（未签名）。填 `http://<服务器>:7788` 和 token。

也可以直接用浏览器打开 `http://<服务器>:7788`。如果开着 Clash 等系统代理，要在代理的绕过列表里加 `100.64.0.0/10,*.ts.net`（App 不需要）。

**升级**：`git -C ~/claude-hub pull && ~/claude-hub/deploy/install.sh`，App 里按 ⌘R。只有发布说明写了 App 有改动时才需要重装 App。

## 开发

项目由 Claude Code 维护，流程（评审 → 实现 → `scripts/check.sh` → 实测 → 推送）见 [CLAUDE.md](CLAUDE.md)，需求决策见 [docs/requests.md](docs/requests.md)。每次克隆后执行一次 `git config core.hooksPath .githooks` 启用推送前检查。

## 许可

MIT
