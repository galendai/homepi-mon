# 实现说明 004：Phase 2 Web Admin 规划调整

> 日期：2026-08-11
> 状态：规划中
> 变更类型：阶段插入与后续计划顺延

## 1. 决策

产品所有者确认将远端主机本地 Web Admin 设为新的 Phase 2。该阶段以浏览器配置界面统一管理
API Provider、系统凭据引用、配置校验、只读连接测试、服务应用与 Raspberry Pi Display 参数，
避免操作者重复组合多条命令。

Web Admin 仅监听远端主机 loopback，不在 Raspberry Pi 上提供设置页面或管理端口；Provider
凭据继续只保存在远端主机系统凭据库，Pi 永不接收 Provider Key、`auth.json`、Cookie 或原始响应。
现有 CLI 保留为自动化、救援和高级诊断入口，并与 Web Admin 复用同一配置事务与校验服务。

## 2. 阶段映射

| 调整前 | 调整后 | 内容 |
|---|---|---|
| Phase 1 | Phase 1 | 可运行首版，不变 |
| — | Phase 2 | 本地 Web Admin 与 Display 配置编排 |
| Phase 2 | Phase 3 | 多页面、自动轮播与 HomeLab |
| Phase 3 | Phase 4 | 远程显示控制 |

任务编号同步调整：

- 新 Web Admin：`P2-01..P2-03`。
- 原 `P2-01..P2-04`：顺延为 `P3-01..P3-04`。
- 原 `P3-01..P3-03`：顺延为 `P4-01..P4-03`。

## 3. Phase 2 边界

Phase 2 包含：

- loopback-only Web Admin、同源安全策略和脱敏状态 API。
- Provider 草稿、字段校验、只读测试、密钥轮换、批量应用、服务重启、健康确认与失败回滚。
- Mac 端 Display 配置页，通过固定允许操作的 SSH 流程向 `dietpi` 原子下发环境文件、校验、
  重启和回滚。
- `homepi-display config validate` 非交互校验入口。
- macOS + 目标 DietPi 实机验收；Windows/Linux 保留接口契约，本阶段按产品决策暂缓实机。

Phase 2 不包含：

- Pi 本地设置菜单、键盘、触摸或浏览器管理界面。
- LAN/公网管理端口、多租户、RBAC、历史指标或通用远程 Shell。
- Provider 登录、登出、OAuth、Token 刷新或账号切换。
- HomeLab 连接器、多页面轮播和远程显示命令；这些分别属于顺延后的 Phase 3 和 Phase 4。

## 4. 文档同步范围

- `HL-Spec.md`：新增 Web Admin 架构、安全决策、模块映射和阶段输入。
- `PRD.md`：新增 Phase 2 需求、里程碑和验收；原 Phase 2/3 顺延。
- `Module-Spec-005-WebAdmin.md`：定义 Web Admin、配置事务和 Display 部署器。
- `Test-Module-005-WebAdmin.md`：定义 Unit、E2E、安全、回滚和实机验收用例。
- `Development-Plan.md`：插入新 Phase 2、重排任务 ID、依赖和统计。
- Module/Test/UI 002–004：同步 Phase 3/4 语义和引用。

## 5. 完成标准

1. 所有阶段名称、任务 ID、依赖和门禁引用一致。
2. 所有 Module 都有对应测试文档，Web Admin 新增 Module 005/Test 005。
3. 全仓 `.vibe` 不残留把 HomeLab 称为 Phase 2、把远程控制称为 Phase 3 的有效规划文本。
4. 历史 Phase 1 实施记录保持不变，不将新规划改写为已实现或已验证。
5. 本轮只修改项目文档，用户确认后再提交。
