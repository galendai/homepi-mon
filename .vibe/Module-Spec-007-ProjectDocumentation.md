# Module Spec 007：项目文档与 GitHub 入口

> 模块 ID：MOD-007
> 版本：0.1
> 状态：已实现并通过自动化验证；GitHub 预览与 Mock Quick start 待用户验收

## 1. 模块目标

为 HomePi Monitor 提供可直接从 GitHub 仓库首页使用的双语项目入口。默认 `README.md` 使用英文，
`README.zh-CN.md` 使用简体中文；两者必须互相链接，并以仓库当前代码、Makefile、脚本和已认证规格为
事实来源，帮助新用户完成安装、首次配置、运行、Raspberry Pi 部署和开发验证。

## 2. 目标读者

- 希望快速了解系统边界与当前完成状态的 GitHub 访问者。
- 在 macOS 或 Linux 上安装 `homepi-node` 的操作者。
- 在 Raspberry Pi 3 / DietPi 上部署只输出 kiosk 的维护者。
- 需要修改 Go daemon、连接器、Web Admin 或 TUI 的开发者。

## 3. 交付物

- 根目录 `README.md`：英文默认入口。
- 根目录 `README.zh-CN.md`：简体中文对等入口。
- 两份文档均包含语言切换、项目概览、架构、功能与阶段状态、系统要求、安装、首次配置、
  Web Admin、daemon 服务、Display 部署、开发、测试、发布、安全、故障排查和规格索引。

## 4. 内容契约

- 所有命令必须能映射到当前 binary、Makefile target 或已提交脚本；不得虚构包管理器、Docker、
  Homebrew、GitHub Release 下载地址或自动 Pi 部署能力。
- 明确区分已实现能力与后续 Phase：Phase 1 核心链路和 Phase 2 Web Admin 已有代码，Display SSH
  部署器、HomeLab、多页面和远程控制仍按开发计划推进。
- 安装说明优先提供源码构建和 `make install-local`；不得把 `install-local` 描述成首次服务注册，
  用户服务仍由 `homepi-node install|start|stop|status|uninstall` 管理。
- Provider 配置优先推荐 loopback Web Admin，同时保留 CLI 入口；秘密只通过密码输入、标准输入或
  受控环境变量进入凭据库，不得示例把真实 Key 放入命令参数、配置文件或仓库。
- Codex 登录生命周期完全由官方 CLI 管理；HomePi 只读现有 `auth.json`，不得声称会登录、刷新 Token
  或切换账号。
- DietPi 文档必须使用 `homepi-display service-unit` 与 `environment-example` 作为部署模板来源，说明
  `/etc/homepi-display/environment` 权限为 `0600`，并避免在 systemd unit 或进程参数中暴露设备 Token。
- TLS 实机路径必须说明通过 `homepi-node doctor` 获取证书指纹；`-no-tls` 只作为 loopback 本地开发路径。
- 不存在 LICENSE 文件时，不添加许可证声明或徽章。

## 5. 双语一致性

- 两份 README 保持相同的信息架构、命令块、路径、环境变量和安全警告。
- 中文版本不是逐字机翻，可使用自然中文，但不得改变能力状态、默认值或操作顺序。
- 相对链接必须在 GitHub 仓库根目录可解析；`.vibe` 规格链接使用 URL 编码或无空格路径。

## 6. 验收标准

- 两份 README 均存在、非空、包含对方语言入口且一级标题唯一。
- 核心章节齐全，所有仓库内相对文件链接均指向真实文件。
- 文档列出的 Make target、脚本、binary 子命令和关键环境变量能在源码中找到对应实现。
- 两份文档的 shell 命令块数量和关键公共命令集合一致。
- 示例不包含疑似真实 Provider Key、设备 Token、用户绝对路径或凭据内容。
- Markdown 无行尾空白，`git diff --check` 通过。
- 标准 `go test ./...` 与 `go vet ./...` 继续通过，证明文档变更未破坏仓库门禁。
