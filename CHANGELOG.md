# Changelog

## Unreleased

- New 📊 Usage tab: today's Claude Code tokens on the server (input / output / cache write / cache read, subagent share) by model and project, and the last 7 days.
- Fix: building from a fresh clone failed outside the maintainer's cloud (npm lockfiles pointed at a private mirror).
- Claude Code hooks (optional, `claude-hub hooks install`): exact status per session — thinking, running a tool, waiting for permission, error — instead of reading the screen; each conversation's context usage with a warning at 85%; the pet gets worried when a session waits for you. Without hooks everything falls back to screen reading.
- Fix: redeploying the server could kill every tmux session (including the Claude doing the deploy) when the hub had started the tmux server.
- Fix: the poker table could freeze when chips changed (e.g. a Claude task reward) while a bot was thinking.
- First public version: tmux session list / new / rename / close, xterm.js terminal, image upload pasted as Claude attachments, finish/ask notifications, Texas Hold'em vs 4 bots, pet that grows while Claude works.
- Mac app (Tauri) with a local forwarder so system proxies (Clash etc.) can't break it.
- Claude Code plugin `hub-for-claude-code` with an install/update/troubleshoot skill.
