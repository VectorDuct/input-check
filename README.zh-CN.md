# Input Check (`input-check`)

**[English](./README.md)** · **[检查项与 API](./docs/checks-and-api.md)** · **[配置说明](./docs/configuration.md)** · **[信封编码格式](./docs/encoding.md)**

> **VectorDuct 核心原语：Wire-Level Guard（线级边界守卫）** ([vectorduct.com](https://vectorduct.com/#stack))

面向大模型应用的请求结构校验器与固定上游网关。支持通过本地命令行或 HTTP 接口校验 Chat Completions 请求结构，在不回显任何提示词正文的前提下输出元数据诊断报告，并可作为前置校验网关部署于固定上游服务之前。

## 核心特性

- **底层编码与 JSON 严格校验**：强制执行 UTF-8 有效性、Unicode 转义合规性、重复 JSON 键拒绝，以及可配置的嵌套深度与字节体积上限。
- **Chat Completions 结构检查**：校验消息角色（Role）序列与内容块（Content Block）的数据结构。
- **可配置定界符与标记检查**：检测未闭合或交叉的标签、孤立的标题标记、异常控制字符/双向文本字符（Bidi）以及受损的编码信封。
- **零内容泄露诊断报告**：仅输出固定诊断码、计数与基于零的字节偏移量（Byte Offset），日志与响应中绝不回显提示词片段。
- **本地 CLI、HTTP 校验 API 与固定上游网关**：支持离线单次检查、带鉴权的 `/v1/check` 校验服务，以及支持独立双 Token 鉴权、模型白名单映射与 SSE 流式转发的固定上游网关。
- **纯 Go 标准库实现**：基于 Go 1.25+ 标准库构建，无外部第三方依赖，内置并发上限控制、请求超时截止时间与优雅停机机制。

## 快速开始

编译二进制文件并在本地运行结构检查：

```bash
CGO_ENABLED=0 GOMAXPROCS=1 go build -p 1 -trimpath -o bin/inputcheck ./cmd/inputcheck
./bin/inputcheck check -input examples/request.json
./bin/inputcheck check -strict -input examples/warnings.json
```

在 `-strict`（严格模式）下，包含结构告警的请求会返回退出码 `1` 及如下纯元数据诊断结果：

```json
{
  "valid": true,
  "warnings": 1,
  "findings": [
    {
      "code": "unclosed_tag",
      "severity": "warning",
      "message_index": 0,
      "block_index": -1,
      "offset": 0
    }
  ]
}
```

| 退出码 | 含义 |
| :--- | :--- |
| `0` | 文档结构合法（非严格模式下允许存在告警）。 |
| `1` | 文档结构合法，但存在被 `-strict` 策略拒绝的告警项。 |
| `2` | 输入格式非法、配置错误、超出资源上限或执行异常。 |

## HTTP 校验接口 (`/v1/check`)

在环境变量 `INPUTCHECK_TOKEN` 中配置至少 16 字节的随机客户端令牌并启动服务：

```bash
./bin/inputcheck serve -config examples/config.inspect.json -check-config
./bin/inputcheck serve -config examples/config.inspect.json
```

发送校验请求：

```bash
curl --fail-with-body \
  -H "Authorization: Bearer $INPUTCHECK_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @examples/request.json \
  http://127.0.0.1:9090/v1/check
```

## 可选固定上游网关模式 (`/v1/chat/completions`)

复制 `examples/config.proxy.json` 为 `config.json`，配置固定上游源地址、模型映射白名单及独立的双端鉴权环境变量（`INPUTCHECK_TOKEN` 用于验证下游客户端，`INPUTCHECK_UPSTREAM_TOKEN` 用于向固定上游鉴权）：

```bash
./bin/inputcheck serve -config config.json -check-config
./bin/inputcheck serve -config config.json
```

当配置开启 `reject_warnings: true` 时，包含结构告警的请求将在网关层被直接拦截，不会向上游发起任何调用。

## 确定性传输信封 (`encode`)

针对需要在多跳中间件链路中保持特殊字符与边界完整性的工作流，可使用显式信封编码：

```bash
./bin/inputcheck encode -input examples/request.json
```

仅在请求通过无告警校验后执行，将指定文本区域封装为带编号的 Base64 分块与 SHA-256 校验和，同时保持工具调用（Tool Calls）、参数及非文本字段原样不变。

## Docker 部署

设置 `INPUTCHECK_TOKEN` 后启动容器（默认以 `UID/GID 65532`、只读文件系统及无 Linux 特权模式运行于 `127.0.0.1:9090`）：

```bash
docker compose up --build -d
```

## 开发与测试

```bash
make test
make vet
make build
python3 tools/runtime_smoke.py --binary bin/inputcheck
```

## 开源许可

MIT License. 由 [VectorDuct](https://vectorduct.com) 维护。
