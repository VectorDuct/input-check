# Input Check · 结构化输入检查工具

为模型应用检查 Chat Completions 请求：验证格式与边界，定位问题，返回不含提示词原文的诊断报告。既可作为本地命令行，也可作为检查 API 或固定上游前的网关。

[English](README.md) · [检查项与 API](docs/checks-and-api.md) · [配置说明](docs/configuration.md) · [可选编码](docs/encoding.md)

**默认只检查，不改写内容。** 检查通过只表示满足已实现的结构规则，不代表内容安全或模型一定可以执行。

## 能检查什么

- UTF-8、Unicode 转义、重复 JSON 字段、嵌套深度和输入大小。
- 消息角色、内容形状和文本块格式。
- 指定标签未闭合、交叉或多余闭合，以及独立标题标记。
- 非常规控制字符、双向显示控制符和损坏的编码封装。

报告只包含固定问题代码、统计和位置，不包含提示词片段。它不判断文字含义，也不冒充内容审核或提示注入防护产品。

## 本地使用

需要 Go 1.25 或更新版本。项目仅使用标准库，无外部 Go 模块。

```sh
CGO_ENABLED=0 GOMAXPROCS=1 go build -p 1 -trimpath -o bin/inputcheck ./cmd/inputcheck
./bin/inputcheck check -input examples/request.json
./bin/inputcheck check -strict -input examples/warnings.json
```

最后一条演示未闭合标签并退出 1。普通 `check` 允许警告；`-strict` 把警告作为拒绝条件。格式或资源错误退出 2，正常退出 0。

省略 `-input` 或设置 `-input -` 时从标准输入读取。`-tags input,customer_note,资料` 可覆盖要识别的标签名称；只接受字面名称，不接受自定义正则。默认名称为 `input`、`user_input`、`context`、`content`、`用户输入`。

`check` 和 `encode` 不访问网络，也不会调用模型。诊断位置为解码后文本块内的 UTF-8 字节偏移。

## 检查 API

通过自己使用的凭据管理方式，把一个至少 16 字节的随机 Token 放入 `INPUTCHECK_TOKEN`。然后执行：

```sh
./bin/inputcheck serve -config examples/config.inspect.json -check-config
./bin/inputcheck serve -config examples/config.inspect.json
```

```sh
curl --fail-with-body \
  -H "Authorization: Bearer $INPUTCHECK_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/request.json \
  http://127.0.0.1:9090/v1/check
```

示例配置没有上游。请求只会被检查，返回诊断报告。`-check-config` 校验配置结构，不读取凭据、不尝试连接；它不能证明密钥或上游可用。

## 放在应用前面

从 `examples/config.proxy.json` 复制自己的私有配置，填写固定上游地址和模型映射，分别配置入站与上游 Token。

- `/v1/check`：始终只检查，不向上游发送。
- `/v1/chat/completions`：配置上游后才启用；先检查，再转发。
- `mode: "inspect"`：默认模式。模型映射为同名时，完整请求字节不变；改了模型别名时会序列化 JSON，但不改写解码后的文本内容。
- `reject_warnings: true`：警告也拒绝，不进入上游。

客户端的 Cookie、授权头和其他私有头不会转发；网关单独设置上游授权。它不是任意 URL 代理，不自动重试或切换备用上游，不扩大模型权限，也不处理计费和账号。

## 显式编码

如果应用确实需要分块文本封装，可以执行：

```sh
./bin/inputcheck encode -input examples/request.json
```

或在网关配置中明确设置 `mode: "encode"`。编码前必须检查有效且没有警告；只改写指定文本区域，保留工具结果、工具参数及非文本块。

编码格式使用 Base64 和 SHA-256 校验，**可逆，不是加密、脱敏、审核或提示注入防护**。它通常增加输入长度并改变传给模型的文本表示，下游是否正确理解需要由自己的应用验证。原有 `OBF-B64/1` 标记为兼容保留，具体说明见[格式文档](docs/encoding.md)。编码命令会按请求输出内容，需自行决定输出去向。

## 部署

设置好 `INPUTCHECK_TOKEN` 后，可用 `docker compose up --build -d`。默认仅映射到本机 `127.0.0.1:9090`，使用非 root 用户、只读文件系统和资源限制，没有配置模型上游。真实配置和密钥不要上传仓库。

默认输入 1 MiB、转发请求 8 MiB、并发 2、总期限 60 秒。检查报告和请求正文均不落盘。转发模式会将有效请求交给你配置的上游，并原样传回上游响应；代理不检查输出内容。对外访问请通过自己的 HTTPS／访问控制入口，Token 不应经过不可信明文网络。

它仅支持文档列出的 Chat Completions 结构和日常输入诊断，不是所有提供商的完整协议校验器。非文本内容视为不透明数据，不从图片、工具返回或函数参数中推断风险。

## 测试、发布与许可

```sh
make test
make vet
make build
python3 tools/runtime_smoke.py --binary bin/inputcheck
python3 tools/release.py
```

测试只使用虚构内容和本地假上游，不需要真实凭据，不调用模型。发布脚本只打包白名单源码、示例和文档，附逐文件 SHA-256；部署配置、运行数据、缓存及内部证据不会随包发布。

MIT 许可证。编码与代理核心提取自此前自用网关，检查器和通用接口为本次新增，详见[来源声明](PROVENANCE.md)。当前是独立发布候选，尚未向 GitHub 上传。
