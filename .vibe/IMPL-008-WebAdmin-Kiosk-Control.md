# 实施记录 008：Web Admin 远程 Kiosk 操作

> 对应模块：Module 005 Web Admin、Module 004 远程控制
> 目标：让本机 loopback Web Admin 复用 Phase 4 已存在的受限命令通道，向已配置 Kiosk 发布固定允许列表操作。

## 1. 设计冻结

- 浏览器只提交固定的操作 DTO，不接收任意 `kind`、`params`、Shell、URL 或控制凭据。
- Web Admin 后端通过现有 `POST /v1/control/devices/{device}/commands` 和
  `GET /v1/control/commands/{command}` 调用 daemon；控制凭据仍从本机 secret store 读取，绝不进入 HTML、JavaScript、API 响应、URL、日志或浏览器存储。
- Web Admin 只增加同源、CSRF/Origin 保护的 `POST /api/display/control` 和
  `GET /api/display/control/{command_id}`；现有 loopback Host、CSP、请求大小和速率限制继续生效。
- 服务端把 UI 操作转换为 `protocol.CommandRequest` 并调用协议校验；`refresh` 的连接器 ID 必须来自当前配置，目标设备必须由 daemon 再次确认。
- 发布响应只返回 command ID、目标、kind、sequence、时间和脱敏结果状态；消息全文不写入响应或日志。前端对非最终状态轮询，最终状态显示 `accepted/executed/rejected/expired/failed` 与稳定错误码。

## 2. UI 操作范围

Display 页面新增 Remote Kiosk control 面板：

| 操作 | UI 参数 | 服务端边界 |
|---|---|---|
| Show page | 五个固定页面、持续时间 | 页面白名单，duration 5–300 秒 |
| Next/Previous | 持续时间 | duration 5–300 秒 |
| Rotation | On/Off、间隔、持续时间 | interval/duration 5–300 秒 |
| Refresh | 可选已配置连接器列表 | 最多 16 个，空表示全部启用连接器 |
| Message | ASCII 文本、级别、持续时间 | 116 字节，`info/warning/critical`，禁止控制字符 |
| Brightness | 0–100 | 由 Kiosk 能力返回 `unsupported_capability` 时不得伪成功 |

目标设备从 Display profile 的 `device_id` 读取，浏览器不能指定任意设备。配置缺失或控制通道不可用时，页面显示可操作的脱敏错误。

## 3. 实现边界

- `internal/webadmin` 定义 Kiosk controller 接口、严格 DTO、协议映射和 API handlers。
- `cmd/homepi-node/configure.go` 注入适配器，复用 Phase 4 CLI 已验证的 TLS pinning、独立 secret 引用和控制 HTTP 客户端。
- 静态前端使用现有自包含 vanilla JS/CSS；不引入依赖、CDN、内联脚本或新的认证机制。
- 测试覆盖协议映射、未知字段、越界/控制字符、CSRF/Origin、状态轮询、无控制器降级和静态页面契约。

## 4. 不在本次范围

- 不新增 daemon/Pi 入站端口，不把远程控制凭据下发给浏览器。
- 不支持任意命令、Shell、关机、重启、文件管理、Provider 凭据修改或多设备聚合。
- 不改变 Phase 4 命令协议、队列、ACK、幂等和 Pi 仲裁实现。
