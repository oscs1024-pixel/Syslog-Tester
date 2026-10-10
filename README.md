# Syslog Tester

基于 **Go + MyGo Native UI** 的 Syslog 外发测试桌面应用。面向 SIEM、SOC、态势感知、日志采集器联调，支持 UDP / TCP 和 TXT 批量重放。

## 功能

- **原生 macOS 风格**：Apple Music 风格浅色双栏界面、侧边栏 Vibrancy、MyGo `plugins/glass` 液态玻璃交互；无 WebView、无底部播放器控制条。
- **目标集中管理**：名称、主机/IP、端口、UDP/TCP、TCP 分帧、源 IP、默认目标；发送页面仅通过下拉框选择目标。
- **单条发送**：原样透传完整 Syslog，或自动构造 RFC3164 / RFC5424 消息。
- **TXT 导入**：仅支持 `.txt`，可逐行发送或整个文件作为一条消息，BOM/CRLF 支持，按需流式读取，预览最多 30 条。
- **发送任务**：重复循环次数、发送间隔、暂停、继续、取消、失败立即停止、发送数量和字节数统计。
- **历史记录**：本地保存最近 300 个任务摘要，保存任务开始时的不可变接收目标快照。
- **模拟事件模板**：Linux SSH、sudo、防火墙阻断、交换机端口变化。

> **重要**：UDP/TCP `Write` 成功仅表示 Socket 写入完成，并不能证明日志平台已经接收、解析或索引。请在目标平台检索消息内容确认。

## 构建与运行

MyGo v0.3.7 需要 **Go 1.27.1**。项目采用 `desktop/` 独立 Go module，核心模块独立使用标准库（Go 1.23+），方便快速单测和故障注入。

```bash
# 核心单测与竞态检测（不需要桌面/网络依赖）
go test ./...
go test -race ./...
go vet ./...

# 运行桌面应用：Mac 上安装 Go 1.27.1
cd desktop
go mod tidy
go run .
```

桌面应用没有命令行用户界面或后台 HTTP 服务；`go run .` 仅是开发者的启动方式。发布时通过 `go build -o SyslogTester .` 打包桌面程序。

## 项目结构

- `internal/domain`: 接收目标和字段验证。
- `internal/syslog`: Syslog RFC3164/5424 构造、格式判断和 Raw 透传。
- `internal/transport`: UDP、TCP、Octet Counting、LF/CRLF 分帧，连接/写入超时与取消。
- `internal/source`: 单条字节输入和 TXT 流式消息读取。
- `internal/sender`: 单 worker 有界发送、顺序限流、任务状态与统计。
- `internal/store`: 目标和历史的本地 JSON 文件原子替换。
- `internal/templates`: 内置模拟安全事件。
- `desktop`: MyGo 浅色 Native UI 主窗口、目标管理、单条发送、TXT、模板和历史。

## 发送行为及安全边界

- 传统 RFC3164 模式限制构造消息至 1024 字节；RFC5424 不额外截断；原样透传保留输入字节。
- UDP 一条消息一个 Datagram，上限为 65507 字节，实际发送建议远低于网络 MTU。
- TCP 默认 Octet Counting (RFC6587)；支持 LF/CRLF 分隔符，分隔符模式拒绝带内换行以防帧歧义。
- Raw 指消息体不重新封装，**不表示关闭 TCP 分帧**。
- TCP 建连最多 3 次（在连接总超时预算内，指数退避与抖动），UDP 建连只尝试一次；网络失败不自动重传消息，避免重复告警。外发写入超时与用户取消有上界；文件重放采用同步单 worker 串行消费以提供背压。
- TXT 单条记录最多 1 MiB，整文件模式最大 1 MiB；解析时不一次性读取整个文件。
- 本地配置存于系统用户配置目录 `SyslogTester/settings.json`，持久化采用临时文件 + fsync + rename；存储失败不更新内存状态。
- 传输层无 HTTP API、CLI 接口或 Syslog 监听服务。

## 测试

核心测试覆盖 RFC PRI、格式检测、原始日志字节保持、超长拒绝、UDP 报文边界、TCP 分帧、TCP 连接取消、TXT BOM/CRLF/长行限制、任务完成/暂停/取消、历史并发写入、任务 ID 幂等写入、有限重试及持久化失败回滚。CI 运行核心 Go 单测、race、vet 和桌面模块编译。

## 开发依据

MyGo 官方示例：[`examples/vibrancy`](https://github.com/egoist/mygo/tree/v0.3.7/examples/vibrancy)、[`examples/gallery`](https://github.com/egoist/mygo/tree/v0.3.7/examples/gallery)、[`plugins/glass`](https://github.com/egoist/mygo/tree/v0.3.7/plugins/glass)。
