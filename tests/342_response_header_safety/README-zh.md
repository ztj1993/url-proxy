# Feature 342: 响应头安全治理与重写标头清洗测试套件

本目录提供 Feature 342（入向响应头 RFC 9110 逐跳清洗、响应体重写深度标头净化、非标 Gzip 强制落盘解压及生态强校验透传）的独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 342 专用测试配置文件，声明响应体重写规则、动态免缓存规则与时效元数据规则。
- `mock_server.go`: 模拟上游源站（支持返回静态与动态逐跳响应头、待重写 HTML、强行 Gzip 压缩包、Docker 镜像元数据及标准制品）。
- `response_safety_test.go`: 核心自动化测试套件，全面覆盖响应逐跳过滤、重写标头清洗、非标 Gzip 透明落盘解压与生态 Digest/X-Cache-Status 可观测性。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/342_response_header_safety/...
```

---

## 3. 测试覆盖场景

1. **RFC 9110 逐跳响应标头剥离 (`TestResponseHopByHopStripping`)**：
   - 验证上游返回的静态逐跳标头（`Transfer-Encoding: chunked`, `Keep-Alive`, `Upgrade`, `Proxy-Connection`, `Connection: close`）被彻底剥离，杜绝下游 HTTP/2 客户端报 `PROTOCOL_ERROR` 或 TCP 连接被意外切断；
   - 验证通过 `Connection` 头动态声明的私有单跳头（如 `X-Dynamic-Upstream-Hop`）被同步解析并过滤；
   - 验证端到端业务响应头（如 `Server`, `X-Custom-Echo`）安全完整透传。

2. **响应体重写标头安全清洗 (`TestRewriterResponseHeaderSanitization`)**：
   - 验证响应体发生动态替换后，与旧正文绑定的失效哈希标头（`ETag`, `Content-MD5`, `Digest`）被彻底删除；
   - 验证 `Accept-Ranges` 与 `Content-Encoding` 被清除，防止客户端进行错误的分片请求或发生二次解压失败；
   - 验证 `Content-Length` 重新计算并精确匹配重写后的正文字节长度。

3. **非标源站强行 Gzip 传输的落盘解压与防污染 (`TestForcedGzipStaticDownloadDecompression`)**：
   - 模拟畸形源站强行返回 `Content-Encoding: gzip` 静态二进制制品；
   - 验证代理透明解压流，磁盘文件保存的为 100% 原始未压缩制品（排查魔数 `0x1f 0x8b`）；
   - 验证响应头中的 `Content-Encoding` 被剥离，客户端无缝读取原始制品。

4. **生态强校验标头原样透传与全链路 X-Cache-Status 可观测性 (`TestEcosystemDigestAndCacheStatusObservability`)**：
   - 验证 Docker Registry Manifest 的 `Docker-Content-Digest` 等强校验标头绝对保真透传；
   - 验证静态制品首次回源下载下发 `X-Cache-Status: MISS`，已有缓存重复下载下发 `X-Cache-Status: HIT`。
