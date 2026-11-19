# Fast 和 Ultrafast 用量与费用

Dashboard 按常规、Fast、Ultrafast 展示原始 Token。趋势图同时绘制总量和加速合计，可筛选任一速度单独查看。模式未确认的记录暂归常规，事件导出保留判断依据。

## 模式证据

计数继续只来自 Session JSONL。JSONL `turn_context.service_tier` 的明确值优先；缺少该字段时，扫描器只读同一 Codex Home 的 `logs_*.sqlite`，使用 `codex_core::session::handlers` 中 `TurnInput` 的 submission ID 与 session ID 精确关联用量 turn。

`fast`、`priority` 表示 Fast，`ultrafast` 表示 Ultrafast，`default` 表示常规。诊断字段 `Some(None)` 表示明确清除速度设置，归为常规。`None` 表示没有覆写，保持未确认。同一等级的证据发生冲突时，该轮保留未确认分类。

解析器按结构识别字段，跳过提示词中的字符串及转义内容；数据库只保存模式元数据，不保存诊断正文。记录表示请求选择的模式，不能证明服务端最终处理等级或真实扣费。

## 历史与升级

v2.8.0 将 schema v11 升至 v12，按已保存的明确 `service_tier` 补认 Ultrafast，只更新事件和轮次的模式。费用在查询时按新目录重算。此前为 6.2 正式标识或其日期快照设置的临时别名、单价由内置价格接替，其他本机映射继续生效。

schema v7 到 v8 为增量迁移，不清除账目或扫描游标，不要求全量重建。更早版本已有的重建要求继续保留。首次扫描会读取旧 JSONL 的模式元数据，并分批导入尚存在的诊断记录；后续按诊断游标增量补齐，晚到证据可重新分类已入账事件。

补齐只更新模式字段，不更改 token、事件 ID、日期、模型、任务或归属。日志丢失、锁定、轮换或格式不兼容时，计数继续工作，无法确认的部分暂按常规统计。已持久化的证据不会因源日志被清理而丢失。历史覆盖率取决于本机保留的证据，Fast 为零不意味着从未使用 Fast。

## 费用口径

加速费用以 Standard 短上下文 API 文本价格为基数，乘以 Codex 订阅内含额度倍率。下表用于本地估算，包括旧型号的历史数据。定价核对日期为 2026-11-19。[OpenAI 速度倍率](https://learn.chatgpt.com/docs/pricing)

| 模型 | Fast | Ultrafast |
|---|---:|---:|
| GPT-6.2 Sol、GPT-6.2 Astra | 2× | 6× |
| GPT-6.1 Sol、GPT-6 Astra | 2.5× | 8× |
| GPT-6 Sol/Luna、GPT-5.6 Sol/Terra/Luna、GPT-5.5 | 2.5× | — |
| GPT-5.4 | 2× | — |

缺少价格或倍率的部分列入未定价 Token。6.2 的 Cache Write 采用 30 分钟保留价。长上下文、12 小时缓存、购买 credits、API 或 Enterprise PAYG 的实际费用请按对应账单口径核对。

## API 与导出

- 汇总、趋势、分解、任务和费用报告中，`modes.regular + modes.fast` 为全部原始 Token。`modes.ultrafast` 是 `modes.fast` 的子集，`modes.unknown` 是 `modes.regular` 的子集。
- 统计及导出接口支持 `mode=regular|fast_only|ultrafast|fast|unknown`。省略表示全部，`fast_only` 仅选 Fast，`fast` 选两种加速模式的合计。
- `/api/v1/cost-estimate`、`/api/v1/sessions`、`/api/v1/session-estimates` 接受 `cost_basis=codex_fast_weighted`。省略参数仍返回原有 Standard API 估算，Dashboard 显式使用新口径。
- `regular_mode_usd + fast_mode_usd` 为已定价费用总额，`ultrafast_mode_usd` 已包含在 `fast_mode_usd` 内。`standard_base_usd` 为基础估算，`fast_surcharge_usd` 为两种加速模式产生的附加费用。
- JSON/CSV 原始事件导出增加 `service_mode`、`service_tier`、`mode_source`、`mode_assumed`。`service_mode=unknown` 与 `mode_assumed=true` 保留事实边界，不将暂按常规估算写成已确认的常规模式。
- 状态接口的 `mode_backfill` 展示诊断补齐进度；模式变化会提升数据版本，失效相关统计与费用缓存。

## 验证

运行 `go test ./...`、`go vet ./...`、`npm test`、`git diff --check`。测试覆盖模式切换、缺失字段、伪字段、重启与日志轮换、证据冲突、增量迁移、定价与模式筛选、缓存隔离和响应式界面。

可选本机验收：先使用 SQLite online backup 将账目复制到 `dist/fast-acceptance/usage.sqlite`，再设置 `CODEX_USAGE_AUDIT_COPY` 和 `CODEX_USAGE_AUDIT_HOME`，运行 `go test ./internal/usage -run '^TestModeBackfillLocalSnapshot$' -v -count=1`。它只更新副本的模式，比较补齐前后的原始事件 SHA-256，并输出同目录的 `audit.json`。
