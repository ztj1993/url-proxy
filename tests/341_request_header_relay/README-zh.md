# Feature 341: 请求标头全量透传与逐跳标头剥离测试套件

本目录提供 Feature 341（出向请求标头全量透传与 RFC 9110 逐跳标头剥离治理方案）的独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 341 专用测试配置文件，声明静态规则、动态旁路规则与时效元数据规则。
- `mock_server.go`: 模拟上游源站（记录出向请求的详细 Headers、Host、Method 与 Path）。
- `header_relay_test.go`: 核心自动化测试套件，全面覆盖多值标头传递、凭证保留、动态逐跳标头过滤、Host 隔离、落盘防压缩与条件请求防传染机制。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/341_request_header_relay/...
```

---

## 3. 测试覆盖场景

1. **Bypass 分支多值标头与逐跳剥离 (`TestBypassHeaderRelay_MultiValueAndDynamicHops`)**：
   - 客户端携带多值标头（`Accept`, `X-Custom-List`），验证出向请求保持多值保真；
   - 客户端携带凭证标头（`Authorization`, `Cookie`），验证安全透传；
   - 客户端携带静态逐跳标头（`Connection`, `Proxy-Connection`, `Keep-Alive`, `Upgrade`），验证全部剔除；
   - 客户端携带通过 `Connection: ..., X-Ephemeral-Hop` 声明的动态逐跳标头，验证动态头及 `Connection` 自身被成功过滤；
   - 客户端请求携带代理自身地址的 `Host`，验证出向请求的 `Host` 被正确重写为目标源站主机名。

2. **静态制品下载防压缩与条件请求防传染 (`TestStaticDownloadHeaderRelay_CompressionAndConditionalGuard`)**：
   - 客户端携带 `Accept-Encoding: gzip, deflate`，验证出向请求剥离该头，防止 gzip 压缩流落盘；
   - 本地未缓存时客户端携带 `If-None-Match` / `If-Modified-Since`，验证出向请求剥离条件头，防止返回 304 导致单飞合并并发失败；
   - 验证应用层业务头（如 `User-Agent: apt/...`）正常透传。

3. **HEAD 与 Range 分支标头透传 (`TestHeadAndRangeHeaderRelay`)**：
   - 验证未命中缓存时的 HEAD 与 Range 请求，正确透传 `Range`、自定义业务标头并剥离逐跳标头。
