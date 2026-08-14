# DietPi Python 调试环境执行记录

## 目标

在当前 HomePi Display 使用的 DietPi 设备上安装基础 Python 运行环境，供后续诊断脚本、协议探测和调试辅助使用；不修改 HomePi Go 服务，不改变 `homepi-display` 的 systemd 配置。

## 安装范围

- 使用 Debian 12 官方 APT 包：`python3`、`python3-venv`、`python3-pip`。
- 在 `/opt/homepi-debug/.venv` 创建独立虚拟环境。
- 不安装第三方 Python 包，不写入 Provider 凭据、设备 Token 或 HomePi 配置。
- 安装后验证 Python、虚拟环境、`pip` 和最小导入/执行路径。

## 安全与回滚边界

- 仅通过既有 `ssh dietpi` 维护入口操作目标设备。
- 安装前检查 APT/dpkg 是否正在执行其他事务。
- 不删除现有文件；若安装失败，保留 APT 错误信息，不启动或重启 HomePi 服务。

## 验收状态

- 安装前基线：DietPi Debian 12 bookworm，`aarch64`，Python 与 pip 均未安装，根分区约有 53 GiB 可用；无并发 APT/dpkg 事务。
- APT 安装：已完成。`python3=3.11.2-1+b1`、`python3-venv=3.11.2-1+b1`、`python3-pip=23.0.1+dfsg-1+rpt1`。
- 虚拟环境验证：已完成。`/opt/homepi-debug/.venv/bin/python` 为 Python 3.11.2，pip 23.0.1；`venv`、`ssl` 导入和最小 JSON 调试脚本均成功，运行架构为 `aarch64`。
- HomePi 服务未受影响：已确认。`homepi-display.service` 为 `active`，`MainPID=4885`，`NRestarts=0`；未要求重启，系统不需要 reboot。
- 第三方包：未安装，后续按具体调试脚本需求添加到该 venv。
