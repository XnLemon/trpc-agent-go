# Platform Phase 1 Design

## Scope

第一阶段目标是把 tRPC-Agent-Go 从单应用框架扩展出一个可验证的多租户平台最小闭环：租户配置、Channel Binding、IM 标准化输入、Gateway 路由、Runner 调用、Redis/PostgreSQL Session 接入、基础审计、消息事件、outbound handoff 和 OpenTelemetry trace 契约。当前实现以小 PR 垂直切片推进，优先落可复用契约、内存实现和验收测试，不在阶段一引入真实企业微信/Telegram SDK 绑定或生产部署模板。

## Package Structure

- `platform`: 多租户平台公共契约包，定义 `Tenant`、`AgentApp`、`ChannelBinding`、`StorageProfile`、`AuditPolicy`、`InboundMessage`、`OutboundMessage`、`MessageEvent`、`AuditRecord`、`UsageRecord`、idempotency、预算、灰度、迁移、脱敏和配置生命周期相关类型与校验方法。
- `platform/channeladapter`: IM 适配层抽象，提供 `Adapter`、`InboundNormalizer`、`OutboundDispatcher`、`OutboxStore`、dead-letter/replay 能力和内存 outbox，实现“外部 callback -> 标准 InboundMessage”与“OutboundMessage -> 平台投递”的边界。
- `platform/gateway`: 最小平台入口，负责验证标准 inbound、生成 request/session/idempotency key、获取 binding/app/runner、执行 session lease、调用 Runner、写 audit/message event、写 outbound，并创建 `im.callback`、`gateway.route`、`runner.run`、`im.reply` trace span。
- `platform/storagerouter`: 存储路由契约，表达租户级 session/memory/summary/artifact/knowledge/audit backend 选择、迁移模式和路由状态，不直接替代现有 session/memory 实现。
- `platform/toolpolicy`: 工具治理桥接，围绕现有 `tool.PermissionPolicy` 增加白名单、拒绝、ask/deny 决策、审批摘要和审计记录。
- `internal/telemetry` 与 `telemetry/semconv/trace`: 统一阶段一 trace marker vocabulary，使用 `trpc.go.agent.trace.span` 标记 `tool.call`、`memory.search`、`memory.write`、`summary.create` 等稳定平台契约。
- 现有 `runner`、`session/redis`、`session/postgres`、`memory/tool`、`internal/flow/processor`: 保持原包边界，通过小补丁补齐 Gateway/Runner/Session/Memory/Tool 的 trace 连接点。

## Runtime Flow

1. Channel Adapter 验签、解析平台事件并归一化为 `platform.InboundMessage`。阶段一以接口和内存 outbox 为主，真实 IM SDK 接入留给后续独立适配 PR。
2. `gateway.Service.HandleInbound` 创建 `im.callback` root span，校验消息、计算 request ID 和稳定 session ID；同一 `tenant/channel/account/message_id` 写入 idempotency store，重复投递直接返回已完成/处理中结果，避免重复调用模型。
3. Gateway 通过 registry 读取 `ChannelBinding`、app runtime 和 Runner，创建 `gateway.route` span；同一 session 通过 `SessionLeaseStore` 串行化，避免多 worker 同时追加同一会话事件。
4. Gateway 将文本 content part 转为 `model.Message` 并调用 Runner，Runner 下游继续使用现有 session/memory/tool/model 流程；阶段一补齐 `runner.run`、`session.get`、`session.append_event`、`tool.call`、`memory.search`、`memory.write`、`summary.create` marker。
5. 执行完成后 Gateway 聚合 assistant 输出，写 `MessageEvent`、`AuditRecord` 和 `OutboundMessage`，创建 `im.reply` span；trace ID 保存在 audit/message/outbound 记录中，便于从 IM 消息反查完整链路。

## Main Implementation Points

- **多租户契约**: `platform/types.go` 将租户、应用、模型、工具策略、通道绑定、存储策略、审计和消息事件建模为轻量 Go struct；校验方法集中在 `platform/validation.go` 和各专题文件，避免把平台概念侵入 core runner。
- **幂等与 session 串行化**: `platform.IdempotencyStore` 保存 request 状态；`platform/gateway` 使用 message key 识别重复 callback，用 `SessionLeaseStore` 为同一 session 建立 lease/fencing 边界。
- **Gateway 最小闭环**: `NewService` 注入 registry、idempotency store、outbound store、audit/message event sink；`HandleInbound` 串起 validate、dedup、route、run、audit、outbox，内存实现支撑单测和验收测试。
- **审计和脱敏**: `platform.AuditSink`、`AuditRecord`、operation summary 和 redaction helpers 只记录 secret ref、hash、error type 等安全字段；trace/error sanitizer 避免明文 token、DSN、tool args 泄漏。
- **Trace 契约**: span 名保持兼容已有实现，新增稳定 marker attribute。Tool 使用 `MarkToolCallSpan`；memory search/write 覆盖 preload 与 memory tool；Redis/PostgreSQL summary create 均标记 `summary.create`。
- **Trace 安全错误记录**: memory search/write 的失败路径统一走 `recordSafeSpanError`，先用 `ToErrorType` 归一化为低基数 `error.type`，再写入 span status、`error.type` attribute 和 recorded error；不把 raw `err.Error()`、memory text、query、memory_id、Bearer token、API key 或后端错误明文写入 trace。
- **Storage 边界**: 阶段一复用现有 Redis/PostgreSQL session 模块，只补 trace 与验收点；`storagerouter` 先沉淀租户后端选择和迁移状态，为第二阶段多后端治理做接口准备。

## Testing

- 平台契约测试覆盖类型校验、租户配置生命周期、灰度、预算、审计、usage、message event、idempotency、storage migration/status 和 redaction。
- Gateway 测试覆盖最小文本 loop、outbound handoff、重复投递不重复执行、session lease、audit trace correlation、safe error redaction 和完整 trace skeleton。
- Trace 聚焦测试覆盖 `tool.call`、`memory.search`、`memory.write`、`summary.create`，其中 summary create 同时覆盖 Redis 与 PostgreSQL。
- Trace 安全回归测试分两层：`internal/telemetry` 验证 helper 不记录 raw error text；`memory/tool` 的 add/update/delete/clear 失败路径使用包含 memory text、memory_id、`Authorization: Bearer`、`api_key` 的 mock error，并扫描 span status、attributes、events，确认只暴露低基数 `error.type`。
- 当前已运行并记录在对应 PR 的验证是 PR 级聚焦验证，包括相关 `go test`、`go build ./...`、`git diff --check`，子模块测试在各自目录执行，例如 `cd session/postgres && go test -run 'TestCreateSessionSummary_WithTracing' -count=1`。这些结果证明各切片本身可验证，但不等同于阶段一主干验收；所有 PR 按序合入后仍需在 `main` 重跑等价 CI 和阶段验收用例。

## PR And Merge State

阶段一实现已拆成小范围 PR 栈推到 `XnLemon/trpc-agent-go:main`，核心 PR 包括平台契约、adapter/outbox、gateway session lease、storage router、minimum loop acceptance、trace skeleton、安全错误、runner/session trace、message event trace、tool/memory/summary trace。每个实现 PR 均保持单职责，合并前应按依赖顺序从底层契约到 Gateway/trace 逐个同步主干、解决冲突、重新跑对应验证。

当前 PR 栈摘要：

| PR | Branch | Module | Boundary | Validation state |
| --- | --- | --- | --- | --- |
| #16-#25 | `feat/*platform*` | `platform` | usage、audit、budget、gray、migration、capacity 等平台契约 | PR 级单元测试/校验已在各 PR 记录；合入后需主干回归 |
| #26-#31 | `feat/gateway-*`, `feat/channel-*`, `feat/storage-*` | `platform/gateway`, `platform/channeladapter`, `platform/storagerouter`, `platform/toolpolicy` | session lease、outbox handoff、adapter/outbox、storage router、tool policy bridge、text loop | PR 级聚焦测试已记录；合入顺序从契约到 gateway |
| #32-#36 | `feat/platform-*` | `platform`, `platform/gateway` | 多租户契约聚合、budget decision audit、minimum loop/outbound acceptance | 验收测试在 PR 内验证；主干阶段验收待合入后执行 |
| #37-#42 | `feat/platform-*-trace` | `platform/gateway`, `runner`, `internal/flow/processor` | audit trace correlation、安全错误、trace skeleton、runner/session/message event/tool trace | PR 级 trace 测试已记录；依赖前序 gateway 合入 |
| #43-#45 | `feat/platform-memory-*-trace`, `feat/platform-summary-create-trace` | `memory/tool`, `internal/flow/processor`, `session/redis`, `session/postgres` | memory search/write、summary create trace marker；memory trace 安全错误记录 | 聚焦测试和独立 review 已完成，P0/P1 clear；合入后需与完整 trace skeleton 联测 |
| #46 | `feat/platform-phase1-design` | `docs` | 第一阶段实现设计说明 | 文档-only；不改变运行时代码 |

建议合入顺序：

1. `platform` 基础契约、审计、预算、灰度、usage、配置生命周期。
2. `channeladapter` outbox/dead-letter 与 `storagerouter` 路由契约。
3. `gateway` session lease、outbound handoff、minimum loop acceptance。
4. trace/audit correlation、安全错误、runner/session/message event trace。
5. tool/memory/summary trace marker PR。

## Known Follow-ups

- `adapter-follow-up`: 企业微信和 Telegram 真实 SDK adapter、webhook 验签/解密、文件下载和平台限频退避仍需后续 PR 接入。
- `observability-follow-up`: `summary.create` 当前覆盖成功路径 marker；失败路径 error status/exception 记录可在 observability 增强 PR 中统一补齐。
- `tracing-consistency-follow-up`: Redis tracing 通过 `WithEnableTracing` 显式开关，PostgreSQL summary create 使用全局 tracer；若后续要求后端语义完全一致，需要补统一 tracing option。
- `merge-validation-follow-up`: 第一阶段代码已形成最小闭环实现和验收测试，但 PR 栈尚未合入 `main`；阶段完成声明应以 PR review、CI 通过和按序合入后的主干验证为准。

## Review And Verification Closure

- #43 `feat/platform-memory-search-trace`: memory search raw error 泄漏 P1 已修复并复审 clear；验证命令包括 `go test ./internal/telemetry -run 'TestTraceMemorySearch' -count=1`、`go test ./internal/telemetry -count=1`、`go test ./platform/... -count=1`、`go build ./...`、`git diff --check`。
- #44 `feat/platform-memory-write-trace`: memory write 原始错误记录和 add/update/delete/clear 调用层测试缺口均已修复并复审 P0/P1 clear；验证命令包括 `go test ./memory/tool -run 'TestMemoryTool_(AddMemory|UpdateMemory|DeleteMemory|ClearMemory)_RecordsMemoryWriteTraceContractOnError' -count=1`、`go test ./memory/tool -count=1`、`go test ./internal/telemetry -count=1`、`go test ./platform/... -count=1`、`go build ./...`、`git diff --check`。
- #45 `feat/platform-summary-create-trace`: 已同步 #44 的安全测试保护，summary create marker 未被冲突解决破坏；验证命令包括 `go test ./memory/tool -run 'TestMemoryTool_(AddMemory|UpdateMemory|DeleteMemory|ClearMemory)_RecordsMemoryWriteTraceContractOnError' -count=1`、`go test ./memory/tool -count=1`、`go test ./internal/telemetry -count=1`、`go test ./platform/... -count=1`、`go build ./...`、`git diff --check`。
