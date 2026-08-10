# 实现说明 002：Phase 1 P1-04 跨平台 daemon、配置与安全

> 文档 ID：IMPL-002
> 版本：0.2
> 日期：2026-08-10
> 对应任务：P1-04
> 状态：评审整改完成，待用户复核

本文件记录 P1-04 的实际实现结构、与规格的偏差、以及未覆盖范围。它是代码与 `.vibe` 规格
之间的对照表，不重复规格内容。

## 1. 新增仓库结构

```text
internal/config/        JSON 配置加载/校验/save，单活动 node、SSRF、Provider ID 唯一
internal/secretstore/   跨平台秘密抽象（Keychain/CredMgr/Secret Service + 0600 文件回退）
internal/tlsconfig/     自签 ECDSA 证书生成、SHA-256 指纹、PinningTransport
internal/deviceacl/     设备 Token 撤销列表（SHA-256 哈希、常时比较、原子持久化）
internal/install/       macOS LaunchAgent / Linux systemd --user / Windows ScheduledTask
cmd/homepi-node/        拆为 main.go + serve.go + doctor.go + config.go + provider.go + device.go + install.go
cmd/homepi-display/     增加 -node-cert-pin、-no-tls，构造 tlsconfig.PinningTransport 注入 syncclient
internal/nodeapi/       增加 RevokedTokens 字段、hashToken 辅助函数
internal/connector/     registry.go：连接器 ID → factory + UnimplementedFactory 占位
internal/connector/mock/registry.go：注册 mock + 五个 P1-05/06 真实连接器占位
```

外部依赖新增：

- `github.com/zalando/go-keyring v0.2.8`（macOS Keychain / Windows CredMgr / Linux Secret Service）
- `github.com/godbus/dbus/v5 v5.2.2`（keyring 的 Linux D-Bus 依赖）
- `github.com/danieljoos/wincred v1.2.3`（keyring 的 Windows 依赖）

## 2. 配置与子命令

### 2.1 配置文件

`~/.config/homepi-node/config.json`（Windows: `%AppData%`），用 `-config` 覆盖。
schema 详见 `internal/config/config.go`。`schema_version=1` 写入/校验，校验规则：

- 单活动 node（同一 source_node.id 重启允许，进程内已通过 `SourceBinding.Accept` 拒绝第二个）
- Provider ID 唯一、type 在注册中心内、region=custom 必填 https base_url
- SSRF 拒绝：169.254.169.254 元数据、RFC1918 私网、回环（region=custom 时）
- token_ref 必填 device，可选 provider（mock 可空）
- interval ≥ 5s，stale_after ≥ interval

`config validate` 子命令可单跑校验。

### 2.2 子命令清单

| 子命令 | 文件 | 说明 |
|---|---|---|
| `serve` | `serve.go` | 启动 daemon，优先读 config.json，否则回退 P1-03 flag 模式 |
| `doctor` | `doctor.go` | 输出用户/数据目录/secret backend/TLS 指纹/Provider 列表 |
| `config init\|show\|validate` | `config.go` | 写示例、打印当前 JSON（保留 secret_ref）、校验 |
| `provider list\|add\|edit\|test\|remove` | `provider.go` | 完整 Provider 生命周期，secret 走 keyring |
| `device list\|add\|revoke` | `device.go` | 生成 256-bit token、写 keyring、加 revoked.json |
| `install\|start\|stop\|status\|uninstall` | `install.go` | 三平台用户级服务（详见 §4） |

`make run-node`（P1-03 的开发态入口）仍工作：serve 检测不到 config.json 时退回
`-node-id/-device-id/-mock-fixture` flag，并读 `HOMEPI_DEVICE_TOKEN` 作为 device token。
该回退路径仅供本机冒烟；用户实际使用应走 `homepi-node config init` + `device add`。

文件配置加载后统一合并显式 `-addr/-node-id/-node-label/-interval`；设备 ID、设备 token 与
mock fixture flag 仅属于无配置的 P1-03 兼容模式。`config init` 不预置设备或 Provider，
避免首次 `device add`/`provider add` 与占位项冲突。

## 3. TLS 与设备 Token

### 3.1 证书

`homepi-node serve` 首次启动时在 `~/.config/homepi-node/cert.pem` 与 `key.pem` 生成自签
ECDSA P-256 证书（CN=source_node.id，SAN=127.0.0.1 + ::1 + localhost），权限 0600，
目录 0700。已存在则跳过生成。证书私钥文件权限在 `EnsureCert` 写入后显式 chmod 收紧。

`tlsconfig.Fingerprint(path)` 输出 `sha256:HEX:HEX:...`（大写、冒号分隔），
与 `openssl x509 -fingerprint -sha256` 一致；`ParseFingerprint` 接受该格式。

### 3.2 服务端

`serve.go` 把 `*tls.Config`（`tlsconfig.ServerTLSConfig`，MinVersion=TLS 1.2）传入
`http.Server.TLSConfig`；TLS 模式显式调用 `http.Server.ListenAndServeTLS("", "")`，
已有内存证书时无需再次传文件名。显式 cert/key 路径成对加载，`auto` 才调用 `EnsureCert`。
`-no-tls` flag 仅供开发态，本机明文 HTTP。

`nodeapi.Server` 新增 `Options.RevokedTokens map[string]struct{}`（token SHA-256 → struct{}）。
`authorize` 常时比对前调用动态撤销检查器重载原子 `revoked.json`，命中按"未知 token"返回
相同 401；事件流轮询时也复核 token，以关闭撤销前已建立的连接。撤销命令从配置移除设备
后再删除凭据，零设备 daemon 仍可启动并提供健康探针。

### 3.3 客户端

`homepi-display run -node-url https://dev-mac:8443 -node-cert-pin sha256:...`：

- HTTPS 必填 pin，否则 `kiosk` 拒绝启动。
- HTTP 必须 `-no-tls` 且 host 必须是 127.0.0.1 / ::1 / localhost；其他 host 拒绝。
- `resolveHTTPClient` 构造 `http.Client{Transport: tlsconfig.PinningTransport(parsed)}`，
  同时配置 30 秒 client timeout、拨号与 TLS 握手 timeout，再注入
  `syncclient.Options.HTTPClient`。

## 4. 三平台服务

详见 `internal/install/install_*.go`。三平台统一语义（Install/Start/Stop/Status/Uninstall），
不匹配 GOOS 返回 `ErrUnsupported`。每个实现都只调用平台自带命令
（launchctl / systemctl / PowerShell），不需要 root/SYSTEM。

| 平台 | 安装文件 | 启动方式 |
|---|---|---|
| macOS | `~/Library/LaunchAgents/com.galendai.homepi-node.plist` | `launchctl load -w` |
| Linux | `~/.config/systemd/user/homepi-node.service` + `loginctl enable-linger` | `systemctl --user enable --now` |
| Windows | Scheduled Task "HomePi Monitor" | `Register-ScheduledTask` (PowerShell) |

Windows 调用 PowerShell 写入/查询 ScheduledTask，macOS/Linux 调用 launchctl/systemctl，
全部 `os/exec`。仅 `internal/install/install_*.go` 三个文件导入 `os/exec`，全仓扫描
确认：

```sh
$ grep -rn '"os/exec"' cmd/ internal/
internal/install/install_darwin.go:10:  "os/exec"
internal/install/install_linux.go:10:   "os/exec"
internal/install/install_windows.go:10: "os/exec"
```

`provider test` 与 `Collect` 路径不包含 `os/exec`，符合 ADR-013 不调起 CLI 的要求。

## 5. 秘密存储

`internal/secretstore` 抽象 `Store { Get/Set/Delete, Backend() }`。
`Open(Options{FileDir, DataDir})` 探测 zalando/go-keyring；失败回退到 `FileBackend`：

- 路径：`~/.config/homepi-node/secrets/<sha256(ref)>.secret`，权限 0600；不同合法引用不碰撞
- 原子替换：temp + fsync + rename + dir fsync
- `Set/Delete` 必写 stderr warning（"file fallback"），让操作者立即看到降级

Reference 名格式：`keyring:<name>`。`<name>` 限 `[A-Za-z0-9._@-]` 加分隔符 `:/`，长度 ≤256，
且 secretstore 拒绝路径穿越字符。`ValidateRef` 单测覆盖。

## 6. 连接器注册中心

`internal/connector/registry.go` 提供 `Register / HasType / KnownTypes / Build`。
通过 `init()` 注册：

- `mock`（真实可用）
- `minimax_coding` / `codex_usage` / `kimi_coding` / `deepseek_api` / `kimi_api`
  （`UnimplementedFactory`，Collect 返回 `ErrUnsupported`，UI 渲染为 N/A）

CLI 子命令（`provider list/add/edit/test/remove`）通过 registry 验证 type 与构造 connector，
P1-05/06 只需替换对应 factory，无需改 CLI。

## 7. 与规格的偏差

| 项 | 规格 | 实现 | 理由 |
|---|---|---|---|
| 配置格式 | 未指定 | JSON（DisallowUnknownFields 在严格解析路径） | 与 protocol 一致，不引入 YAML 依赖 |
| Token 类型 | HL-Spec §6.1 提到 mTLS 备选 | 仅 Bearer Token + TLS 指纹固定 | mTLS 增加证书分发复杂度，Phase 1 暂不实现 |
| revoke 命令 | 写入 ACL 后立即生效 | 动态重读 `revoked.json`，关闭旧流；配置移除设备 | 与规格一致 |
| 安装的 plist 日志路径 | 未指定 | `~/.config/homepi-node/homepi-node.{out,err}.log` | 与 dataDir 同级，方便操作者 grep |

## 8. 未覆盖范围

- 真实 Provider 连接器：P1-05/P1-06 实现；registry 已留好。
- Codex 登录态只读解析：P1-06；secretstore 已就绪。
- Windows/Linux 服务在本机未实机验证（无对应主机）；CI 跨编译通过。
- Pi 上的 systemd + autostart：P1-07。
- 24 小时稳定性 + 跨平台实机验收：P1-08。

## 9. 已知问题

- **e2e 测试 `TestOfflineOutranksCriticalInHeader` / `TestMockDataFlowsToScreen` /
  `TestForeignSnapshotIsRejected` 在并发与 race 模式下偶发失败**：经 git 还原 nodeapi
  修改前后对比，确认此 flaky 来自 P1-03，
  非本任务引入。建议 P1-08 之前把 `waitFor` timeout 从 3s 调高到 5s 并改为基于
  snapshot version 的同步条件，而非基于 render 文本包含。本轮不在 P1-04 范畴内修复，
  留作 P1-08 之前的清理项。

## 10. 验证命令与实际输出

执行日期：2026-08-10，环境：macOS 25.5.0 arm64，Go 1.26.5。

```text
$ make check
== gofmt ==
== go vet ==
== go test ==
ok  github.com/galendai/homepi-mon/cmd/homepi-display
ok  github.com/galendai/homepi-mon/cmd/homepi-node
ok  github.com/galendai/homepi-mon/internal/...（全部目标包）

$ go test -race -count=1 ./...
ok  github.com/galendai/homepi-mon/cmd/...
ok  github.com/galendai/homepi-mon/internal/...（最终全仓通过）

$ env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /dev/null ./cmd/homepi-node
$ env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /dev/null ./internal/install
$ env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -o /dev/null ./cmd/homepi-display
# 三项均通过
```

首次全仓 race 命中 `TestOfflineOutranksCriticalInHeader`，首次目标重跑命中
`TestForeignSnapshotIsRejected` 的 TempDir 清理竞态；第二次目标重跑和最终全仓 race 通过。
本任务不修改这些 P1-03 flaky，但需在 P1-08 前修复。

```text
$ ./bin/homepi-node --version
homepi-node 0.1.0
commit:   f909a04
built:    2026-08-10T...Z
platform: darwin/arm64
go:       go1.26.5

$ ./bin/homepi-node --help
homepi-node 0.1.0 (...)

Usage:
  homepi-node serve                       ...
  homepi-node doctor                      ...
  homepi-node config init|show|validate   ...
  homepi-node provider list|add|edit|test|remove ...
  homepi-node device list|add|revoke      ...
  homepi-node install|start|stop|status|uninstall ...
  homepi-node --version                   ...

$ ./bin/homepi-node config init
wrote /Users/galendai/Library/Application Support/homepi-node/config.json

$ ./bin/homepi-node doctor
homepi-node 0.1.0 (...)
...
user:     uid=501 euid=501
hostname: dev-mac.lan
data dir: /Users/.../homepi-node
secretstore: backend=keychain
tls:      fingerprint=sha256:AB:CD:...
revoked:  no revocations recorded
config:   /Users/.../homepi-node/config.json (ok)
          node=dev-mac addr=127.0.0.1:8443 devices=0 providers=0

registered connector types:
  - codex_usage
  - deepseek_api
  - kimi_api
  - kimi_coding
  - minimax_coding
  - mock
```

本轮没有修改用户真实 LaunchAgent 状态；macOS stop 通过临时 fake launchctl 验证命令可执行，
真实 install/start/status/stop/uninstall 留在 §11 由用户复核。Windows/Linux 仅完成交叉编译。

## 11. 手动验收步骤（macOS）

按 Development-Plan.md P1-04 验收段落。

```sh
# 1. 准备
mkdir -p /tmp/p104 && cd /tmp/p104
git clone /Users/galendai/repo/homepi-mon
cd homepi-mon
make build

# 2. 初始化配置
./bin/homepi-node config init
# 3. 添加 mock provider（用 examples/mock-fixture.json）
./bin/homepi-node provider add \
  --id mock-demo --type mock --account-label demo \
  --region global --interval 60s --stale-after 5m \
  --mock-fixture examples/mock-fixture.json
./bin/homepi-node provider list

# 4. 添加 display device（生成 token 写入 keyring）
./bin/homepi-node device add --id pi-kiosk
# 输出末尾会打印 token (print once)，复制备用

# 5. 启动 daemon
./bin/homepi-node serve
# 观察 stderr：secretstore=keychain、tls fingerprint、listening on 127.0.0.1:8443

# 6. 在另一个终端启动 display
export HOMEPI_DEVICE_TOKEN=<paste token from step 4>
./bin/homepi-display run \
  --node-url https://127.0.0.1:8443 \
  --node-cert-pin $(./bin/homepi-node doctor 2>&1 | awk -F'fingerprint=' '/fingerprint/{print $2}' | head -1 | tr -d ' \n') \
  --device-id pi-kiosk --node-id dev-mac \
  --data-dir /tmp/p104/display-data
# 屏幕应显示 60×20 网格，三个 Coding Plan + 两个 API 余额，顶栏 LIVE

# 7. 改动 fixture
sed -i '' 's/"value":"68"/"value":"7"/' examples/mock-fixture.json
# 数秒内屏幕应出现 CRIT

# 8. 安装为用户服务
./bin/homepi-node install
./bin/homepi-node status    # installed=true running=true
# Ctrl-C 当前 serve 进程
./bin/homepi-node start     # launchctl kickstart
./bin/homepi-node status    # 仍 running=true

# 9. 撤销 device
./bin/homepi-node device revoke --id pi-kiosk
# 当前 display 应在约 1 秒内断开并进入重连；新连接立即收到 401
./bin/homepi-node device list  # (no devices configured)
# 重启 daemon 后 healthz 仍可用，设备接口继续统一返回 401

# 10. 清理
./bin/homepi-node uninstall
rm -rf /tmp/p104
```

清理：`rm -rf bin dist`。
