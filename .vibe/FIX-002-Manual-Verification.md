# FIX-002 手动验收指南：Web Admin 与配置事务

> 对应规格：`HL-Spec.md`、`Module-Spec-005-WebAdmin.md`、`Module-Spec-006-BuildAutomation.md`
> 对应测试：`Test-Module-005-WebAdmin.md`
> 适用版本：FIX-002 审查修复后的工作树
> 构建入口：`make build` / `scripts/build.sh`（构建脚本提交 `df90365`）
> 快速冒烟入口：当前工作树中的 `scripts/smoke-webadmin.sh`；该脚本随 Web Admin 改动等待手动验收后提交
> 当前实机范围：macOS 本机；Windows/Linux 原生服务与 Raspberry Pi Display 不在本指南的通过范围


## 1. 目标与安全边界

本指南供用户复现以下公共行为：

- 首页、CSS、JavaScript 和 CSRF bootstrap 可由普通浏览器完整加载。
- Web Admin 只监听 loopback，严格校验 Host、Origin、CSRF，并按请求活动重置空闲超时。
- 无效 Provider 编辑不会污染草稿；新增或凭据变化的启用 Provider 未测试时不能 Apply。
- 草稿状态、完整 metric ID 冲突、外部配置 revision 变化和脱敏响应可被观察。
- CLI 的 `custom` region、默认 `secret_ref` 和 `provider remove -keep-secret` 保持兼容。
- 正式配置 Apply 使用真实 LaunchAgent 重启与健康检查。

风险分级：

| 级别 | 影响 | 本指南中的操作 |
|---|---|---|
| L0 | 只读 | 静态资源、响应头、端口和状态检查 |
| L1 | 只改临时配置或内存草稿 | 浏览器草稿、校验、测试、revision 冲突、CLI custom |
| L2 | 改系统凭据或正式配置并重启服务 | 临时 Keychain 条目、正式 LaunchAgent Apply |
| L3 | 故障注入，可能影响当前服务 | 健康失败、并发 Apply、跨平台服务生命周期；不要求在当前账号执行 |

重要限制：

- 隔离配置启动的 Web Admin 仍连接当前用户的真实服务管理器。除明确标注“预期在持久化前失败”的步骤外，隔离冒烟期间不要点击 **Apply**。
- macOS secret store 初始化会创建并立即删除探针 Keychain 条目；L1 步骤不写入业务秘密。
- 不在命令行参数、截图、浏览器导出或验收记录中放入真实 Provider Key、完整设备 Token 或 CSRF token。
- `scripts/smoke-webadmin.sh` 只用于快速查看界面：它在前台运行、使用自动清理的临时配置，期间不要点击
  **Apply**。完整执行 M01～M09 时使用第 3.2 节，以保留后续步骤需要的环境变量和日志。
- 若当前主机承载重要采集任务，先完成第 3～8 节；第 9～10 节另约维护窗口执行。

## 2. 前置条件

1. 当前目录为仓库根目录：

   ```bash
   cd /Users/galendai/repo/homepi-mon
   ```

2. macOS 已安装 Go、Make、`curl`、`jq`、`lsof`、`shasum`、`security` 和 `openssl`：

   ```bash
   command -v go make curl jq lsof shasum security openssl
   test -x scripts/build.sh
   test -x scripts/smoke-webadmin.sh
   bash -n scripts/build.sh scripts/smoke-webadmin.sh
   ```

3. 浏览器可打开开发者工具的 Network 和 Console 面板。
4. 第 9 节要求现有 LaunchAgent 已安装并运行；不要把仓库 `bin/` 下的构建产物安装为持久服务 binary。
5. 第 10 节只使用明确无效的临时测试秘密，不使用真实账号 Key。

## 3. 启动方式与隔离测试环境（L1）

### 3.1 快速只看界面

只需确认页面能否打开、样式和交互是否正常时，在仓库根目录执行：

```bash
cd /Users/galendai/repo/homepi-mon
HOMEPI_WEBADMIN_ADDR=127.0.0.1:18765 \
  ./scripts/smoke-webadmin.sh -idle-timeout 10m
```

脚本会构建隔离 binary、创建临时配置并在前台启动 Web Admin。根据终端输出打开
`http://127.0.0.1:18765/`；检查结束后按 `Ctrl-C`，脚本会停止服务并清理其临时目录。

该模式不会在调用终端保留 `HOMEPI_MANUAL_*` 变量和测试日志，因此不能代替下方的完整验收环境；
隔离 Web Admin 仍能访问当前用户的服务管理器，快速查看期间不要点击 **Apply**。

### 3.2 创建完整验收环境

```bash
cd /Users/galendai/repo/homepi-mon

VERSION=0.1.0-manual ./scripts/build.sh

export HOMEPI_MANUAL_ROOT="$(mktemp -d)"
export HOMEPI_MANUAL_BIN="$PWD/bin/homepi-node"
export HOMEPI_MANUAL_CONFIG="$HOMEPI_MANUAL_ROOT/config.json"
export HOMEPI_MANUAL_DATA="$HOMEPI_MANUAL_ROOT/data"
export HOMEPI_MANUAL_SECRETS="$HOMEPI_MANUAL_ROOT/secrets"
export HOMEPI_MANUAL_PORT=18765
export HOMEPI_MANUAL_URL="http://127.0.0.1:$HOMEPI_MANUAL_PORT"

mkdir -p "$HOMEPI_MANUAL_DATA" "$HOMEPI_MANUAL_SECRETS"
chmod 700 "$HOMEPI_MANUAL_ROOT" "$HOMEPI_MANUAL_DATA" "$HOMEPI_MANUAL_SECRETS"
test -x "$HOMEPI_MANUAL_BIN"
"$HOMEPI_MANUAL_BIN" --version

export HOMEPI_DATA_DIR="$HOMEPI_MANUAL_DATA"
export HOMEPI_SECRET_DIR="$HOMEPI_MANUAL_SECRETS"
unset HOMEPI_PROVIDER_SECRET HOMEPI_NODE_CONFIG

"$HOMEPI_MANUAL_BIN" config init -out "$HOMEPI_MANUAL_CONFIG"
"$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_MANUAL_CONFIG"
shasum -a 256 "$HOMEPI_MANUAL_CONFIG" > "$HOMEPI_MANUAL_ROOT/config.initial.sha256"

printf 'manual root: %s\n' "$HOMEPI_MANUAL_ROOT"
printf 'mock fixture: %s\n' "$PWD/examples/mock-fixture.json"
```

预期构建结果位于 `bin/homepi-node` 和 `bin/homepi-display`，版本输出包含
`0.1.0-manual`；配置显示 `ok (0 providers, 0 devices)`，目录权限为 `0700`，配置权限为 `0600`，
正式配置和服务均未改变。仓库根目录的旧 `homepi-node` 文件不会被本步骤读取或覆盖。

启动 Web Admin：

```bash
"$HOMEPI_MANUAL_BIN" configure \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -addr "127.0.0.1:$HOMEPI_MANUAL_PORT" \
  -idle-timeout 10m \
  -no-browser \
  >"$HOMEPI_MANUAL_ROOT/webadmin.log" 2>&1 &
export HOMEPI_MANUAL_PID=$!

for attempt in 1 2 3 4 5; do
  curl -fsS "$HOMEPI_MANUAL_URL/api/healthz" >/dev/null && break
  sleep 1
done
kill -0 "$HOMEPI_MANUAL_PID"
```

若端口已占用，停止进程后更换 `HOMEPI_MANUAL_PORT` 和 `HOMEPI_MANUAL_URL`，再重新启动。

## 4. 浏览器公共路径与安全边界（L0/L1）

### M01：静态资源和安全响应头

```bash
for path in / /static/app.css /static/app.js /api/bootstrap /api/status; do
  curl -sS -o /dev/null -w "%{http_code}  $path\n" "$HOMEPI_MANUAL_URL$path"
done

curl -sS -D - -o /dev/null "$HOMEPI_MANUAL_URL/" | \
  rg -i 'content-security-policy|cache-control|referrer-policy|x-content-type-options'

lsof -nP -a -p "$HOMEPI_MANUAL_PID" -iTCP -sTCP:LISTEN
```

预期：

- 五个路径均返回 `200`，`app.css` 和 `app.js` 不再是 404。
- 响应包含 CSP、`Cache-Control: no-store`、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`。
- 监听地址只有 `127.0.0.1:18765`，没有 `0.0.0.0` 或 LAN 地址。

验证非 loopback 绑定在监听前失败：

```bash
if "$HOMEPI_MANUAL_BIN" configure \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -addr 0.0.0.0:18766 \
  -no-browser; then
  echo "FAIL: non-loopback bind was accepted"
else
  echo "PASS: non-loopback bind was rejected"
fi
```

### M02：普通浏览器加载、CSRF 与同源请求

1. 打开 `http://127.0.0.1:18765/`，打开开发者工具 Network 面板并刷新。
2. 确认 `app.css`、`app.js`、`api/bootstrap`、`api/status` 均为 200。
3. 确认没有 CDN、外部字体、分析脚本或其他非 `127.0.0.1:18765` 请求。
4. `api/bootstrap` 响应应为 `no-store`；不要复制、截图或导出 CSRF token。
5. 在 Console 执行不带 CSRF header 的请求：

   ```javascript
   fetch("/api/draft", {
     method: "POST",
     headers: {"Content-Type": "application/json"},
     body: "{}"
   }).then(function (response) { console.log(response.status); });
   ```

   预期输出 `403`。

6. 在终端发送错误 Origin：

   ```bash
   curl -sS -o /dev/null -w '%{http_code}\n' \
     -X POST \
     -H "Origin: http://127.0.0.1:18766" \
     -H "X-CSRF-Token: intentionally-invalid" \
     -H "Content-Type: application/json" \
     --data '{}' \
     "$HOMEPI_MANUAL_URL/api/draft"
   ```

   预期输出 `403`。后续正常 Save/Test 请求应带实际 Origin
   `http://127.0.0.1:18765` 和非空 `X-CSRF-Token`，且能够成功。


## 5. 草稿、Provider 测试与 pending 状态（L1）

### M03：合法 mock 草稿不会提前落盘

在 Providers 页面填写：

| 字段 | 输入 |
|---|---|
| ID | `manual-mock` |
| Type | `mock` |
| Account label | `Manual mock` |
| Region | `global` |
| Interval | `60s` |
| Stale after | `5m` |
| Mock fixture | 第 3 节打印的绝对路径 |
| Enabled | 勾选 |

1. 点击 **Save draft**。
2. 确认 Configured 表出现 `manual-mock`，draft diff 显示 `added`。
3. 切回 Overview，确认新 Provider 也出现且仅它标记为 `pending`。
4. 回到 Providers，保持 ID 为 `manual-mock`，点击 **Test**。
5. 预期返回 `class` 为空、`metric_count` 大于 0，且不显示原始 fixture 响应。
6. 检查临时配置仍未变化：

   ```bash
   shasum -a 256 -c "$HOMEPI_MANUAL_ROOT/config.initial.sha256"
   ```

   预期为 `OK`。

在 Network 中检查成功的 `POST /api/draft` 与 `POST /api/draft/test`：

- 请求带同源 Origin 和 CSRF header。
- 请求 URL/query、响应和 Console 中没有候选秘密。
- `POST /api/draft/apply` 尚未发生。

### M04：被拒绝的编辑不会污染草稿

将表单改为新 ID `manual-invalid`，其余保持 mock 合法值，只把 Interval 改为 `1s`，点击
**Save draft**。

预期：

- 页面提示 400/invalid draft。
- Configured 表和 draft diff 均不出现 `manual-invalid`。
- `manual-mock` 仍在草稿中，配置 checksum 仍为 `OK`。

### M05：Provider 修改使既有测试失效

1. 重新填写 `manual-mock`，把 Interval 从 `60s` 改为 `61s`，点击 **Save draft**。
2. 不重新 Test，点击一次 **Apply**。
3. 预期 Apply 返回 conflict/validation 错误，指出 Provider 必须先通过只读测试。
4. 再次检查配置 checksum 为 `OK`，LaunchAgent 未被重启。

这一步虽然点击 Apply，但错误发生在任何秘密或配置持久化之前，因此可用于隔离配置。

### M06：检测真实 metric ID 冲突

当前 `manual-mock` fixture 已声明 `codex.coding.5h` 等完整 metric ID。再新增一个草稿：

| 字段 | 输入 |
|---|---|
| ID | `manual-codex` |
| Type | `codex_usage` |
| Account label | `Manual Codex` |
| Region | `global` |
| Interval | `60s` |
| Stale after | `5m` |
| Enabled | 勾选 |

点击 **Save draft**。预期：

- Save 在草稿边界直接拒绝，错误包含冲突的完整 metric ID（例如 `codex.coding.5h`）以及双方 Provider。
- Configured 表和 draft diff 不出现 `manual-codex`，无需也不得继续点击 Apply。
- checksum 仍为 `OK`。

不要在本节对 `manual-mock` 执行成功 Apply。

## 6. 磁盘 revision 冲突（L1）

1. 重新 Test `manual-mock`，确保草稿当前可通过测试。
2. 在终端保存当前文件并追加合法 JSON 空白，模拟另一个 CLI/编辑器修改磁盘内容：

   ```bash
   cp "$HOMEPI_MANUAL_CONFIG" "$HOMEPI_MANUAL_ROOT/config.pre-revision.json"
   perl -0pi -e 's/\z/ /' "$HOMEPI_MANUAL_CONFIG"
   shasum -a 256 "$HOMEPI_MANUAL_CONFIG" > "$HOMEPI_MANUAL_ROOT/config.external.sha256"
   ```

3. 浏览器点击 **Apply**。
4. 预期返回 revision conflict，磁盘内容未被旧草稿覆盖：

   ```bash
   shasum -a 256 -c "$HOMEPI_MANUAL_ROOT/config.external.sha256"
   ```

5. 预期为 `OK`。停止 Web Admin，并恢复隔离配置：

   ```bash
   kill -INT "$HOMEPI_MANUAL_PID"
   wait "$HOMEPI_MANUAL_PID" || true
   unset HOMEPI_MANUAL_PID

   cp "$HOMEPI_MANUAL_ROOT/config.pre-revision.json" "$HOMEPI_MANUAL_CONFIG"
   chmod 600 "$HOMEPI_MANUAL_CONFIG"
   "$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_MANUAL_CONFIG"
   ```

## 7. 按请求活动重置空闲超时（L1）

启动一个 15 秒空闲超时的全新进程：

```bash
"$HOMEPI_MANUAL_BIN" configure \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -addr "127.0.0.1:$HOMEPI_MANUAL_PORT" \
  -idle-timeout 15s \
  -no-browser \
  >"$HOMEPI_MANUAL_ROOT/idle.log" 2>&1 &
export HOMEPI_IDLE_PID=$!

for sample in 1 2 3 4; do
  curl -fsS "$HOMEPI_MANUAL_URL/api/healthz" >/dev/null
  sleep 8
done

if kill -0 "$HOMEPI_IDLE_PID" 2>/dev/null; then
  echo "PASS: request activity kept the server alive"
else
  echo "FAIL: server closed despite request activity"
fi

sleep 18
if kill -0 "$HOMEPI_IDLE_PID" 2>/dev/null; then
  echo "FAIL: idle server is still alive"
  kill -INT "$HOMEPI_IDLE_PID"
else
  echo "PASS: server closed after true inactivity"
fi
wait "$HOMEPI_IDLE_PID" || true
unset HOMEPI_IDLE_PID
```

预期：持续请求超过原始 15 秒后进程仍存活；最后一次请求后静置超过 15 秒，进程才退出，
`idle.log` 包含 idle timeout 信息。

## 8. CLI 兼容：custom region 与默认 secret_ref（L1）

本节只修改临时配置，不写候选秘密：

```bash
export HOMEPI_CUSTOM_ID="manual-custom-$(date +%s)"

"$HOMEPI_MANUAL_BIN" provider add \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -id "$HOMEPI_CUSTOM_ID" \
  -type deepseek_api \
  -account-label "Manual custom" \
  -region custom \
  -base-url http://127.0.0.1:18080

jq -r --arg id "$HOMEPI_CUSTOM_ID" \
  '.providers[] | select(.id == $id) | [.region, .base_url, .secret_ref] | @tsv' \
  "$HOMEPI_MANUAL_CONFIG"

"$HOMEPI_MANUAL_BIN" provider remove \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -id "$HOMEPI_CUSTOM_ID"

"$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_MANUAL_CONFIG"
```

预期：

- add 成功，不因 `region=custom` 被拒绝。
- 输出依次为 `custom`、`http://127.0.0.1:18080`、`keyring:provider-key:<本次 ID>`，证明未传
  `-secret-ref` 时仍应用默认引用。
- remove 成功，临时配置再次通过 validate。


## 9. 正式配置 Apply 与真实 LaunchAgent（L2，可选）

仅在维护窗口、当前 macOS 用户服务已安装且运行时执行。本节会修改正式配置并重启 daemon 两次。

先移除隔离环境覆盖，并确认 LaunchAgent 使用仓库和临时目录之外的持久 binary。完成本节前不要执行
`make clean`，因为当前 Web Admin 命令使用 `bin/homepi-node`：

```bash
unset HOMEPI_DATA_DIR HOMEPI_SECRET_DIR HOMEPI_NODE_CONFIG HOMEPI_PROVIDER_SECRET

export HOMEPI_REAL_CONFIG="$HOME/Library/Application Support/homepi-node/config.json"
export HOMEPI_PLIST="$HOME/Library/LaunchAgents/com.galendai.homepi-node.plist"
export HOMEPI_REAL_BACKUP="$HOMEPI_MANUAL_ROOT/config.real.before.json"
export HOMEPI_REAL_PORT=18767
export HOMEPI_REAL_URL="http://127.0.0.1:$HOMEPI_REAL_PORT"

test -f "$HOMEPI_REAL_CONFIG"
test -f "$HOMEPI_PLIST"
"$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_REAL_CONFIG"
"$HOMEPI_MANUAL_BIN" status
/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' "$HOMEPI_PLIST"
cp -p "$HOMEPI_REAL_CONFIG" "$HOMEPI_REAL_BACKUP"
chmod 600 "$HOMEPI_REAL_BACKUP"

launchctl print "gui/$(id -u)/com.galendai.homepi-node" | \
  awk '/pid =/{print $3; exit}' > "$HOMEPI_MANUAL_ROOT/pid.before"
cat "$HOMEPI_MANUAL_ROOT/pid.before"
```

出现以下任一情况就停止：

- `status` 不是 `installed=true running=true`。
- plist 中 binary 路径位于 `$HOMEPI_MANUAL_ROOT`、当前仓库的 `bin/` 目录，或文件不存在。
- 服务实际使用自定义配置路径，而不是上述默认配置；当前 LaunchAgent 模板不会继承终端中的
  `HOMEPI_NODE_CONFIG`。

启动正式配置的 Web Admin：

```bash
"$HOMEPI_MANUAL_BIN" configure \
  -config "$HOMEPI_REAL_CONFIG" \
  -addr "127.0.0.1:$HOMEPI_REAL_PORT" \
  -idle-timeout 10m \
  -no-browser \
  >"$HOMEPI_MANUAL_ROOT/webadmin-real.log" 2>&1 &
export HOMEPI_REAL_WEB_PID=$!
```

打开 `http://127.0.0.1:18767/`，新增一个不会采集的 Provider：

| 字段 | 输入 |
|---|---|
| ID | `manual-apply-<当前时间戳>` |
| Type | `mock` |
| Account label | `Manual apply` |
| Region | `global` |
| Interval | `60s` |
| Stale after | `5m` |
| Mock fixture | 仓库 `examples/mock-fixture.json` 的绝对路径 |
| Enabled | **取消勾选** |

1. 点击 **Save draft**，确认只有该 ID 标记 pending。
2. 点击 **Apply**。
3. 预期 HTTP 200，响应同时为 `restarted: true`、`healthy: true`，`step_log` 包含
   `persisting`、`restarting`、`verifying` 和 `completed`。
4. 检查真实服务和新 PID：

   ```bash
   "$HOMEPI_MANUAL_BIN" status
   launchctl print "gui/$(id -u)/com.galendai.homepi-node" | \
     awk '/pid =/{print $3; exit}' > "$HOMEPI_MANUAL_ROOT/pid.after-add"
   diff "$HOMEPI_MANUAL_ROOT/pid.before" "$HOMEPI_MANUAL_ROOT/pid.after-add"
   "$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_REAL_CONFIG"
   ```

   预期服务 running、PID 发生变化、正式配置有效。

5. 在 Web Admin 删除该测试 Provider，再点击 **Apply**。
6. 再次预期 `restarted: true`、`healthy: true`，服务 running，测试 ID 已从正式配置移除：

   ```bash
   "$HOMEPI_MANUAL_BIN" status
   jq -e '.providers[] | select(.id | startswith("manual-apply-"))' \
     "$HOMEPI_REAL_CONFIG" >/dev/null && \
     echo "FAIL: manual provider remains" || \
     echo "PASS: manual provider removed"
   ```

7. 停止 Web Admin，但不要停止 daemon：

   ```bash
   kill -INT "$HOMEPI_REAL_WEB_PID"
   wait "$HOMEPI_REAL_WEB_PID" || true
   unset HOMEPI_REAL_WEB_PID
   ```

若任一 Apply 失败，先检查 UI 的 rollback step、正式配置是否仍有效、服务是否仍 running。只有自动
回滚未恢复时才执行以下人工恢复；不要在成功场景无条件覆盖正式配置：

```bash
"$HOMEPI_MANUAL_BIN" stop
cp "$HOMEPI_REAL_BACKUP" "$HOMEPI_REAL_CONFIG.manual-restore"
chmod 600 "$HOMEPI_REAL_CONFIG.manual-restore"
mv "$HOMEPI_REAL_CONFIG.manual-restore" "$HOMEPI_REAL_CONFIG"
"$HOMEPI_MANUAL_BIN" config validate -config "$HOMEPI_REAL_CONFIG"
"$HOMEPI_MANUAL_BIN" start
"$HOMEPI_MANUAL_BIN" status
```

保留 `config.real.before.json`，直到用户确认正式配置与采集结果正确。

## 10. 版本化秘密与 `-keep-secret`（L2，可选）

本节会在 macOS Keychain 创建一个明确无效的临时条目，并在结尾删除。仅当 Overview 显示
secret backend 为 macOS Keychain 时执行；不要替换为真实 Provider Key。

```bash
export HOMEPI_KEY_ID="manual-key-$(date +%s)"
export HOMEPI_DUMMY_SECRET="$(openssl rand -hex 16)"

printf '%s\n' "$HOMEPI_DUMMY_SECRET" | \
  "$HOMEPI_MANUAL_BIN" provider add \
    -config "$HOMEPI_MANUAL_CONFIG" \
    -id "$HOMEPI_KEY_ID" \
    -type deepseek_api \
    -account-label "Manual dummy key" \
    -secret-stdin
unset HOMEPI_DUMMY_SECRET

export HOMEPI_KEY_REF="$(jq -r --arg id "$HOMEPI_KEY_ID" \
  '.providers[] | select(.id == $id) | .secret_ref' "$HOMEPI_MANUAL_CONFIG")"
printf 'versioned ref: %s\n' "$HOMEPI_KEY_REF"
security find-generic-password \
  -s github.com/galendai/homepi-mon \
  -a "$HOMEPI_KEY_REF" >/dev/null
```

预期引用为 `keyring:provider-key:<本次 ID>@1`，证明候选秘密写入新版本引用，没有覆盖基础引用。
`security` 命令只检查存在性，不加 `-w`，不得把秘密打印到终端。

验证删除 Provider 时保留秘密：

```bash
"$HOMEPI_MANUAL_BIN" provider remove \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -id "$HOMEPI_KEY_ID" \
  -keep-secret

security find-generic-password \
  -s github.com/galendai/homepi-mon \
  -a "$HOMEPI_KEY_REF" >/dev/null && \
  echo "PASS: -keep-secret retained the key" || \
  echo "FAIL: -keep-secret lost the key"
```

重新引用该条目并用默认删除完成清理：

```bash
unset HOMEPI_PROVIDER_SECRET
"$HOMEPI_MANUAL_BIN" provider add \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -id "$HOMEPI_KEY_ID" \
  -type deepseek_api \
  -account-label "Manual dummy key" \
  -secret-ref "$HOMEPI_KEY_REF"

"$HOMEPI_MANUAL_BIN" provider remove \
  -config "$HOMEPI_MANUAL_CONFIG" \
  -id "$HOMEPI_KEY_ID"

if security find-generic-password \
  -s github.com/galendai/homepi-mon \
  -a "$HOMEPI_KEY_REF" >/dev/null 2>&1; then
  echo "FAIL: temporary key still exists"
else
  echo "PASS: temporary key was pruned"
fi
unset HOMEPI_KEY_ID HOMEPI_KEY_REF
```

脱敏响应可在默认删除前另启隔离 Web Admin 检查：`GET /api/draft` 只能包含掩码引用，不能包含
完整 `secret_ref` 或候选秘密；普通表格只显示 `set`/`none`。不要导出含 token 的 HAR。


## 11. 真实 Provider（L2，可选）

仅在用户持有测试账号并同意真实只读请求时执行：

1. 在隔离 Web Admin 中添加真实 Key Provider，将候选 Key 只输入密码字段。
2. 保存草稿后检查配置 checksum 未变化，密码字段已清空，`/api/draft` 没有完整引用或 Key。
3. 输入故意错误的 Key 并 Test，预期显示脱敏认证分类；不要点击成功 Apply。
4. 输入有效测试 Key 并 Test，预期 30 秒内返回指标数；与官方控制台对账另按 Provider 验收文档执行。
5. 修改 region/base URL/interval/secret 后，Apply 应再次要求 Test。
6. 若要对正式配置轮换 Key，必须先完成第 9 节备份，并确认测试成功；成功后检查新版本引用和旧引用清理。

真实 Provider 未执行时，应记录为“未执行外部验收”，不能写成通过。

## 12. 不建议在当前账号手工注入的场景

以下行为已由自动化测试覆盖，手工复现需要隔离 macOS 用户、虚拟机或对应原生平台：

| 场景 | 原因 | 应保留的自动化或后续证据 |
|---|---|---|
| 两个 Apply 同时进入事务 | 时序不稳定，可能重复重启真实服务 | configtx 并发 revision/race 测试 |
| Restart 成功后 HealthCheck 失败 | 需要可控的假服务管理器 | 恢复旧配置、再次重启旧服务、删除候选秘密的单元测试 |
| 配置写入或回滚中断电 | 需要文件系统故障注入 | 原子临时文件、rename、目录同步测试与代码审查 |
| 缺省 `DisplayStatus` 回调 | 生产 CLI 总会注入回调 | webadmin nil-callback 回归测试 |
| Windows/Linux 用户服务 | 当前没有原生测试主机 | 交叉构建不替代原生 install/start/stop/status/uninstall |
| Raspberry Pi Display 实屏 | 属于 P2-03/P2-04 | Mac→DietPi 浏览器到实屏 E2E 记录 |

若在隔离账号执行故障注入，必须单独保存输入、预期输出、实际输出、旧/新 PID、配置摘要和服务日志；
不能把当前账号上“服务仍可启动”当作回滚全链路已经通过。

## 13. 验收结果记录

执行后填写“实际输出/证据”和结果；没有执行的项目填“未执行”，不要留空或推断通过。

本次执行日期：2026-08-12；环境：macOS darwin/arm64；构建版本：`0.1.0-manual`（构建提交 `df90365`）；范围：隔离配置 M01～M09。

| ID | 输入或环境 | 预期输出 | 实际输出或证据 | 结果 |
|---|---|---|---|---|
| M01 | 隔离配置，静态路径与响应头 | 五路径 200；安全头完整；仅 loopback | `/`、`/static/app.css`、`/static/app.js`、`/api/bootstrap`、`/api/status` 均为 200；响应含 `Cache-Control: no-store`、CSP、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`；`lsof` 仅显示 `127.0.0.1:18765`；`0.0.0.0:18766` 启动被拒绝 | 通过 |
| M02 | 普通浏览器、无 CSRF、错误 Origin | UI 可用；非法修改请求 403 | 浏览器成功加载 Overview/Providers/Display；DOM 资源仅为同源 `app.css`/`app.js`，控制台无 warning/error；无 CSRF 请求 403，错误 Origin 请求 403 | 通过 |
| M03 | 新增并测试 `manual-mock` | 指标数大于 0；配置未变；新增项 pending | 浏览器 Save draft 后 diff 为 `added manual-mock`；Overview 标记 pending；Test 返回 `metric_count: 7`、`class: ""`；配置 checksum `OK`，磁盘仍为 0 providers | 通过 |
| M04 | `manual-invalid`，interval=`1s` | 400；无效项不进入草稿 | Save draft 触发浏览器错误提示；Configured/draft diff 未出现 `manual-invalid`，`manual-mock` 保留；配置 checksum `OK` | 通过 |
| M05 | 测试后修改 `manual-mock` | 未重新测试的 Apply 被拒；磁盘未变 | Apply 返回 409，错误为 `provider "manual-mock" must pass a read-only test before apply`；`restarted: false`；配置 checksum `OK`，隔离进程仍存活 | 通过 |
| M06 | mock 与 Codex 完整 metric ID 重合 | Save 在草稿边界指出完整冲突 ID | 原示例 ID `manual-codex` 不产生真实冲突，首次保存被接受；改用真正冲突的 Codex ID `codex.coding` 后 Save draft 触发浏览器拒绝提示，Configured/diff 未加入该 Provider，`manual-mock` 保留，checksum `OK` | 通过（按真实冲突 ID 复测） |
| M07 | 外部追加合法 JSON 空白 | 旧草稿 revision conflict；外部内容未覆盖 | Apply 返回 409 `configtx: revision conflict`；外部文件 checksum `OK`；停止 Web Admin 后恢复隔离配置并通过 validate | 通过 |
| M08 | 15 秒超时，8 秒一次请求 | 活跃期间不退出；真正空闲后退出 | 4 次间隔 8 秒的 `/api/healthz` 请求期间进程保持存活；静置 18 秒后退出；日志含 `webadmin idle timeout; closing listener` | 通过 |
| M09 | CLI custom + 缺省 secret ref | custom 成功；默认引用正确 | `provider add` 成功；字段输出为 `custom`、`http://127.0.0.1:18080`、`keyring:provider-key:<临时 ID>`；remove 后 validate 通过 | 通过 |
| M10 | 正式配置、已运行 LaunchAgent | 两次 Apply 都真实重启且 healthy | 未执行（可选；本次不修改正式配置、不重启 LaunchAgent） | 未执行（可选） |
| M11 | 临时 Keychain 候选秘密 | `@1` 版本化；keep 保留；默认删除清理 | 未执行（可选；本次未写入 Keychain） | 未执行（可选） |
| M12 | 真实 Provider 测试账号 | 错误 Key 脱敏；有效 Key 指标可对账 | 未执行（可选；没有使用真实 Provider 凭据） | 未执行（可选） |

验收结论只能使用：

- **通过**：本次要求范围的步骤全部实际执行并符合预期。
- **有条件通过**：L0/L1 和 macOS L2 通过，真实 Provider、Windows/Linux 或 Pi 明确列为未执行风险。
- **不通过**：任一已执行步骤与预期不符；保留日志和备份，不提交。

## 14. 清理

1. 确认所有 Web Admin PID 已退出；若仍存活，对对应 PID 执行 `kill -INT <PID>` 并 `wait <PID>`。
2. 确认正式服务仍运行、正式配置有效，且第 10 节临时 Keychain 条目已删除。
3. 若执行过第 9 节，保留正式配置备份直到用户确认；确认后再清理临时目录。
4. 所有使用 `bin/homepi-node` 的 Web Admin 进程退出后，在仓库根目录清理标准构建产物：

   ```bash
   cd /Users/galendai/repo/homepi-mon
   make clean
   test ! -e bin
   test ! -e dist
   ```

   `make clean` 只删除仓库的 `bin/` 和 `dist/`，不会删除正式配置、LaunchAgent plist 或用户数据。

5. 仅当路径确认为系统临时目录时执行：

   ```bash
   case "${HOMEPI_MANUAL_ROOT:-}" in
     /tmp/*|/private/tmp/*|/var/folders/*)
       rm -rf -- "$HOMEPI_MANUAL_ROOT"
       ;;
     *)
       echo "拒绝清理非临时路径: ${HOMEPI_MANUAL_ROOT:-<empty>}"
       ;;
   esac

   unset HOMEPI_MANUAL_ROOT HOMEPI_MANUAL_BIN HOMEPI_MANUAL_CONFIG
   unset HOMEPI_MANUAL_DATA HOMEPI_MANUAL_SECRETS HOMEPI_MANUAL_PORT HOMEPI_MANUAL_URL
   unset HOMEPI_DATA_DIR HOMEPI_SECRET_DIR HOMEPI_PROVIDER_SECRET
   unset HOMEPI_WEBADMIN_ADDR
   ```

清理不得删除正式配置、LaunchAgent plist、正式日志或用户尚未确认的备份。
