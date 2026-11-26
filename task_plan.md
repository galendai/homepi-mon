# 第四屏 OpenClash 节点状态开发方案

## 目标
基于 homepi-mon 现有三屏架构，设计第四屏实时展示 `side-router` 上 OpenClash 节点状态的方案。调研与后续实现对旁路由配置文件严格只读，不改写配置、不切换节点、不重启服务。

## 阶段
- [complete] 1. 梳理现有屏幕、数据链路、实时更新与部署方式
- [complete] 2. 只读调研 side-router 的系统、OpenClash 版本和可用接口
- [complete] 3. 验证节点状态字段、访问控制和刷新机制
- [complete] 4. 设计第四屏 UI、后端采集和前端更新方案
- [complete] 5. 给出实施步骤、验收标准、风险与回滚方案
- [complete] 6. 校正主动测速调度，避免阻塞 5 秒状态刷新
- [complete] 7. 实现 OpenClash 协议、状态、只读采集器与配置校验
- [complete] 8. 实现 Go 页面模型、legacy、轮播和远程控制
- [complete] 9. 实现 LVGL 第四屏与 Go/C 桥接
- [complete] 10. 集成测试、真实 API 冒烟与问题修复
- [complete] 11. 更新 README 与 durable 文档
- [complete] 12. 运行完整验证并独立审查
- [complete] 13. 构建并部署 Node/Display，配置 OpenClash Provider 与四屏，重启并实机验证
- [complete] 14. 临时用 `-page-only OPENCLASH` 固定第四屏并实机验证
- [complete] 15. 起草与前三页风格一致的 OPENCLASH ASCII 设计稿并等待用户审核
- [complete] 16. 实现“每节点一卡”的 OPENCLASH 纯卡片页并部署验证
- [complete] 17. OPENCLASH 卡片改为单行“名称+ping”，使用14/12px字体并垂直居中，部署实机验证
- [complete] 18. 取消 OPENCLASH 副标题栏并用亮色背景高亮当前出口卡片

## 约束
- 仅执行远端只读命令与只读 API 请求。
- 不读取或展示密钥、订阅链接、认证凭据等敏感内容。
- 用户已授权按既有设计修改本仓库产品代码；不部署、不修改 side-router。
- 方案结论必须区分实测事实与待实施验证项。

## 遇到的错误
| 错误 | 尝试次数 | 解决方案 |
|------|---------|----------|
| jsonfilter 对带连字符字段的括号表达式转义失败 | 1 | 不再用该表达式，改用远端 Ruby JSON 解析 |
| Ruby 汇总命令的内层单引号导致 shell 引号未闭合 | 1 | Ruby 字符串统一改用 `%q`/`%Q`，避免嵌套单引号 |
| 远端 Ruby 缺少 `json` 标准库，curl 管道随之 Broken pipe | 1 | 改用本机 Python 解析 SSH 返回的 JSON，不安装任何远端依赖 |
| 主动测速 URL 未加 shell 引号，`&` 被远端 shell 解释，响应体为空 | 1 | 给完整 URL 加双引号并让 curl 使用 `--fail-with-body` |
| Todo append 未指定既有 phase，工具拒绝操作 | 1 | 使用 `phase=方案` 追加校正任务 |
| 新增阶段后沿用旧错误表行号，编辑工具拒绝锚点 | 1 | 重新读取位移后的 error table，再追加记录 |
| gofmt 使用了不存在的 `internal/config/validate_test.go`，后续测试未执行 | 1 | 通过 glob 确认实际文件为 `internal/config/openclash_test.go` 后重跑 |
| `protocol.cloneBool` 与现有 HomeLab helper 重复，相关包编译失败 | 1 | 删除 OpenClash 重复 helper，复用 `homelab.go` 现有实现 |
| Connector 测试按数组位置断言 Unicode 节点连接数，与稳定名称排序冲突 | 1 | 改为按节点名断言，生产排序不变 |
| Pi 传输默认 scp/SFTP 失败：远端无 `/usr/lib/sftp-server` | 1 | 改用 SSH 标准输入流传输 tar 归档 |
| Pi 传输 `scp -O` 失败：远端无 `scp` 客户端 | 1 | 继续改用 SSH 标准输入流传输 tar 归档 |
| 临时 drop-in 命名早于既有 `split-card-pages.conf`，ExecStart 未被覆盖 | 1 | 改用后加载的 `zz-openclash-debug.conf` 并移除无效文件 |
| Pi备份使用错误的display路径，安装在实际替换前停止 | 1 | 读取systemd实际ExecStart，改用 `/usr/local/bin/homepi-display`；首次失败未重启服务 |
| 上次备份失败导致临时回滚路径文件不存在 | 1 | 在同一成功命令中重建备份目录、备份和路径文件后再替换 |
| Pi临时framebuffer抓取helper不存在 | 1 | 直接只读 `/dev/fb0` 的RGB565字节并在本机解码，不安装远端依赖 |
