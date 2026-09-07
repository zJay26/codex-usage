# Fast 模式统计与费用折算

Dashboard 以总 Token 为主要指标，概览下方显示常规 / Fast 拆分；趋势、模型和任务显示总量与 Fast。原始 token 不乘倍率，常规与 Fast 相加始终等于总 Token。模式未确认记录归入常规，界面不单独提示；API 和导出保留原始模式元数据供追溯。

## 模式证据

计数继续只来自 Session JSONL。JSONL `turn_context.service_tier` 的明确值优先；缺少该字段时，扫描器只读同一 Codex Home 的 `logs_*.sqlite`，使用 `codex_core::session::handlers` 中 `TurnInput` 的 submission ID 与 session ID 精确关联用量 turn。

`fast`、`priority` 表示 Fast，`default` 表示常规。诊断字段 `Some(None)` 表示明确清除 Fast 设置，归为常规；`None` 只表示没有覆写，保持未确认。单独的 ThreadSettings、当前配置、模型名、运行速度及时间接近程度都不用于推断模式。未知等级或同等级来源发生冲突时，保留未确认分类。

解析器按结构识别字段，跳过提示词中的字符串及转义内容；数据库只保存模式元数据，不保存诊断正文。记录表示请求选择的模式，不能证明服务端最终处理等级或真实扣费。

## 历史与升级

schema v7 到 v8 为增量迁移，不清除账目或扫描游标，不要求全量重建。更早版本已有的重建要求继续保留。首次扫描会读取旧 JSONL 的模式元数据，并分批导入尚存在的诊断记录；后续按诊断游标增量补齐，晚到证据可重新分类已入账事件。

补齐只更新模式字段，不更改 token、事件 ID、日期、模型、任务或归属。日志丢失、锁定、轮换或格式不兼容时，计数继续工作，无法确认的部分暂按常规统计。已持久化的证据不会因源日志被清理而丢失。历史覆盖率取决于本机保留的证据，Fast 为零不意味着从未使用 Fast。

## 费用口径

常规沿用既有 Standard API 价格及本机定价覆写。Fast 的相同 token 分类基础费用乘以 ChatGPT Codex 额度倍率：GPT-6 Astra、GPT-5.6 Sol/Terra/Luna、GPT-5.5 为 2.5；GPT-5.4 为 2。别名与日期快照通过模型目录解析，不将 mini、Spark、未知模型或未确认的自定义模型自动套入倍率。

界面统一称为“API 等价成本”，以一句话显示 API 价格和 Fast 额度规则的更新日期。计算仍为额度倍率加权的估算，不是实际 API Priority 账单，也不是账号额度扣除记录。未知单价、缺失缓存写入价格、缺少 Fast 倍率的部分继续显示为未定价。

倍率核对于 2026-09-07：[OpenAI Fast 模式说明](https://learn.chatgpt.com/docs/agent-configuration/speed)。历史估算使用当前目录与倍率，不模拟历史账单。

## API 与导出

- 汇总、趋势、分解、任务和费用报告增加 `modes.regular`、`modes.fast`、`modes.unknown`，各自包含原有 token 分类；`unknown` 是 `regular` 的子集。原有 `usage` 与 `grand_total` 保持全部原始 token 语义。
- 统计及导出接口接受 `mode=regular|fast|unknown`，省略表示全部。常规筛选包含模式未确认记录。
- `/api/v1/cost-estimate`、`/api/v1/sessions`、`/api/v1/session-estimates` 接受 `cost_basis=codex_fast_weighted`。省略参数仍返回原有 Standard API 估算，Dashboard 显式使用新口径。
- 费用新增 `regular_mode_usd`、`fast_mode_usd`、`standard_base_usd`、`fast_surcharge_usd`。`usd` 是当前口径下已定价部分的合计；基础估算可包含因倍率未知而未参与加权合计的部分，应同时查看未定价原因。
- JSON/CSV 原始事件导出增加 `service_mode`、`service_tier`、`mode_source`、`mode_assumed`。`service_mode=unknown` 与 `mode_assumed=true` 保留事实边界，不将暂按常规估算写成已确认的常规模式。
- 状态接口的 `mode_backfill` 展示诊断补齐进度；模式变化会提升数据版本，失效相关统计与费用缓存。

## 验证

运行 `go test ./...`、`go vet ./...`、`npm test`、`git diff --check`。测试覆盖模式切换、缺失字段、伪字段、重启与日志轮换、证据冲突、增量迁移、定价与模式筛选、缓存隔离和响应式界面。

可选本机验收：先使用 SQLite online backup 将账目复制到 `dist/fast-acceptance/usage.sqlite`，再设置 `CODEX_USAGE_AUDIT_COPY` 和 `CODEX_USAGE_AUDIT_HOME`，运行 `go test ./internal/usage -run '^TestModeBackfillLocalSnapshot$' -v -count=1`。它只更新副本的模式，比较补齐前后的原始事件 SHA-256，并输出同目录的 `audit.json`。
