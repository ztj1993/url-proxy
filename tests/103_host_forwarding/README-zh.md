# 域名规则转发配置测试指南（已废弃 / DEPRECATED）

> [!WARNING]
> **【已废弃 - 测试跳过 / DEPRECATED - SKIP】**
> - **废弃说明**：本测试套件基于历史旧版本的 `forwarding.conf` 独立配置文件及无指令动词语法（`<host> <gateway>`）。自统一代理配置系统（Feature 304）发布后，`url-proxy` 已**全面废弃并彻底移除了对旧版 `forwarding.conf` 及无指令语法的向后兼容支持**。
> - **替代方案**：域名转发能力已统一迁移至单一真理源配置文件 `proxy.conf`（语法规范为 `forward <host> <gateway>`）。最新完整验证请参见 [`tests/304_unified_config`](../304_unified_config/)。
> - **自动化测试策略**：后续所有全量自动化测试与回归测试均**明确跳过（SKIP）**此测试套件，不再作为测试准入指标。

本文档保留作为旧版本功能参考。

## 1. 测试原理

验证 `url-proxy` 是否能够根据 `forwarding.conf` 规则自动对特定 Host 进行请求重写：
- 规则配置：`example.internal http://localhost:9993/mirror`
- 当客户端请求 `http://localhost:8888/http://example.internal/package.tar.gz` 时，`url-proxy` 会将目标地址动态重写为 `http://localhost:9993/mirror/http://example.internal/package.tar.gz`，从而将请求引流至内网镜像网关。

## 2. 准备测试环境

在当前目录下，我们提供了一个简单的 Go 程序 `forward_server.go` 来模拟镜像网关，以及一份配置文件 `forwarding.conf`。

### 步骤 2.1：启动模拟镜像网关服务器
打开一个终端，运行以下命令启动服务器：
```bash
go run forward_server.go
```
*预期输出：`测试用镜像转发服务器已启动，监听端口 :9993`*

### 步骤 2.2：启动 url-proxy（加载转发配置）
`url-proxy` 会自动检测其启动工作目录下的 `forwarding.conf`。
打开**第二个**终端，将本目录的配置文件复制（或软链接）至项目根目录，并在根目录下启动：
```bash
# 复制配置文件到根目录
cp tests/103_host_forwarding/forwarding.conf ./forwarding.conf

# 启动 url-proxy
go run url-proxy.go -addr :8888
```
*预期输出：包含 `forward config file loaded: forwarding.conf`*

## 3. 执行测试

打开**第三个**终端，使用 `curl` 请求被配置重定向的域名 `example.internal`：
```bash
curl -i http://localhost:8888/http://example.internal/package.tar.gz
```

## 4. 观察与验证

### 4.1 curl 响应验证
检查终端输出：
```text
HTTP/1.1 200 OK
Content-Type: text/plain
X-Forwarded-Mock: true
...

url-proxy host forwarding test: PASSED (target: /mirror/http://example.internal/package.tar.gz)
```
*验证要点*：
1. 客户端请求的虽然是不可直连的私有域名 `example.internal`，但成功收到了来自镜像服务器的响应。
2. 响应包含镜像网关特定的 `X-Forwarded-Mock: true` 头。

### 4.2 forward_server 终端日志验证
观察 `forward_server` 终端日志：
```text
[...] [镜像网关] 成功接收到被转发重写的请求: /mirror/http://example.internal/package.tar.gz
```

### 4.3 url-proxy 终端日志验证
观察代理输出：
```text
[1] req: http://example.internal/package.tar.gz
[1] job leader: http://example.internal/package.tar.gz
[1] job download: http://localhost:9993/mirror/http://example.internal/package.tar.gz
```
日志中明确显示 `job download` 的目标地址已带上前缀 `http://localhost:9993/mirror/`。

## 5. 结论
通过上述测试，您可以验证 `url-proxy` 能够根据 `forwarding.conf` 精确匹配目标 Host 并完成前缀重定向。
