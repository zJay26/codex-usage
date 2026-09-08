# 可选更新实现与维护

## 检查与用户选择

`GET /api/v1/updates` 返回当前版本、最新稳定版、自动检查设置、可安装状态和最近结果。服务启动后按持久化时间戳每 6 小时检查一次；后台检查仅请求 [GitHub latest release API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release)。`POST /api/v1/updates/check` 手动检查，`POST /api/v1/updates/preferences` 接收 `{"auto_check":false}` 关闭自动检查。

`POST /api/v1/updates/install` 必须包含 `{"version":"明确选择的版本","confirm":true}`，并且版本必须与已检查的更新一致。只接受稳定的 `vMAJOR.MINOR.PATCH`，不降级、不安装 prerelease 或 draft。所有修改操作沿用本机同源校验。仅在已安装程序中启用安装，便携版和预览服务不会替换其他安装。

不下载发布说明 HTML，不执行日志或 API 文本，不发送统计数据。下载只接受本项目对应 tag 的官方命名文件及 SHA256SUMS；限制大小、超时和 HTTPS 重定向来源；SHA256 与可选 GitHub asset digest 必须一致。关闭自动检查后仍可手动检查。

## 下载目录

“软件更新”提供下载目录输入框、保存、恢复默认、打开文件夹和复制路径，并显示最近下载文件的完整路径。默认使用 Windows 系统 Downloads 已知文件夹（支持重定向），macOS / Linux 使用 `~/Downloads`，其下创建 `codex-usage`。无法解析用户下载目录时退回状态目录的 `downloads`。

`POST /api/v1/updates/preferences` 接受可选 `download_dir` 字段，可与 `auto_check` 一起原子保存；空字符串恢复默认。保存和下载前都检查绝对路径及可写性。更新执行中拒绝修改目录。`GET /api/v1/updates` 返回生效路径、默认路径、自定义设置和最近下载路径；`POST /api/v1/updates/open-directory` 只打开已保存的目录，沿用同源校验，无桌面会话时保留复制路径功能。

下载包保存在所选目录下独立的 `codex-usage-v版本-随机编号` 文件夹，文件名使用对应平台的正式发布名称。成功下载后，将安装包复制到私有更新工作目录并再次校验 SHA256；helper 和数据库备份始终留在状态目录中。修改目录只影响未来下载，不迁移或删除已有文件；原来的更新缓存同样保留。

## 替换与恢复

下载期间旧服务继续工作。下载校验完成后复制当前程序作为独立 helper，传入状态目录下的任务文件。helper 再次验证摘要和目标程序版本，停止旧进程并等待退出，随后备份旧程序、配置、SQLite 主文件及其 WAL / SHM。目标程序启动后必须在 45 秒内通过匹配版本的健康检查；失败则停止新进程、恢复备份并重启旧版本。

Windows helper 使用隐藏独立进程，按完整安装路径停止目标程序。Linux systemd 服务中的 helper 由 `systemd-run --user` 独立 transient unit 启动，避免停止原 service 的 cgroup 时杀死 helper。macOS LaunchAgent 的 helper 使用单独的 launchd job，在主服务 bootout 后继续执行，并在更新或恢复后重新 bootstrap 主服务；helper 不设置 KeepAlive。普通后台进程使用 detached helper。更新保留原有登录启动配置。安装器记录真实目标路径，避免登录启动设置 CODEX_USAGE_HOME 后改变安装目录的解析。

更新结果保存执行进程 PID；服务重启后仍能区分执行中、成功、失败及已恢复。执行进程消失时将中断任务显示为失败，不按耗时猜测正在运行的 helper 已结束。备份不自动清理，断电或无法恢复时可据此手动修复。该功能不承诺对操作系统故障或磁盘损坏自动恢复。

## 测试边界

Go 测试覆盖不下载检查、版本选择、重复安装、校验失败、偏好持久化、任务中断、URL 和校验清单验证、同源及明确确认要求、更新事务的数据恢复。真实进程测试在隔离目录中构建两个版本并替换、重启，移除原始 JSONL 后核对已有统计仍保留；不注册或修改真实登录自启项。Playwright 覆盖“稍后”、关闭检查、主动安装、恢复提示、中英文和窄屏。

Linux systemd 管理分支还依赖宿主机正常的 user bus；启动 helper 失败会在替换旧程序前结束。备份事务测试覆盖新版本修改数据库后的恢复；真实进程测试验证成功路径。

macOS 自动安装依赖图形登录会话的 `gui/<uid>` launchd domain；无法 bootstrap 时会明确报错。原生 CI 覆盖两个架构上的安装、健康检查、卸载保留数据库和重新安装，以及 plist 校验和进程身份检查。相关路径和分发限制见 [macOS 安装说明](macos.md)。
