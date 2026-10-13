# Feature 334: 上游 HTTP 连接池调优与超时加固方案 (Upstream HTTP Transport Pool Tuning & Timeout Hardening)

## 1. 功能目标与背景 (Goal & Background)

### 1.1 现状痛点与风险剖析 (Problem Analysis)
在协议代理与调度核心当前的出向网络实现中，无论是静态文件合并下载（`handleDownload`）、动态元数据免缓存透传与 SWR 协商（`handleBypass`），还是 HTTP Range 分片与 HEAD 探测（`handleRange`、`handleHead`），均直接调用了 Go 标准库默认的全局客户端 `http.DefaultClient`。这种默认配置在代理高并发场景下存在两大严重瓶颈：
1. **单域名空闲连接池极小（`MaxIdleConnsPerHost = 2`）导致连接风暴**：
   - Go 标准库 `http.DefaultTransport` 的 `MaxIdleConnsPerHost` 默认值仅为 **2**。
   - 当局域网内多台客户端同时通过代理拉取同一上游源站（如 `archive.ubuntu.com`、`registry.npmjs.org`、`files.pythonhosted.org`）的多个不同包或元数据索引时，海量并发请求在完成后只能保留 2 条持久连接（Keep-Alive），其余连接全部被立即关闭销毁，后续请求被迫反复重新执行 TCP 三次握手与昂贵的 TLS 握手，显著推高了首字节延迟（TTFB）与本地 TIME_WAIT 端口占用。
2. **缺乏响应头超时（`ResponseHeaderTimeout`）导致协程挂死**：
   - `http.DefaultTransport` 默认未设置 `ResponseHeaderTimeout`（值为 `0`，即无限期等待）。
   - 如果上游服务器在完成 TCP 连接建立并接收完 HTTP 请求头后陷入假死（迟迟不返回 HTTP 响应状态行与响应头），由于此时尚未进入响应体读取循环，下载看门狗或读取超时尚未接管，请求协程将无限期挂起阻塞。

### 1.2 建设目标 (Feature Objectives)
隶属于 `Plan 330 (系统稳定性加固与缓存治理方案)`，本特性旨在为协议代理与调度核心构建专用的高并发、强超时约束的出向 HTTP 客户端传输层：
1. **高并发长连接复用池**：将 `MaxIdleConns` 提升至 `1000`、单主机空闲连接上限 `MaxIdleConnsPerHost` 提升至 `100`（较默认值提升 50 倍），最大化上游 HTTP/1.1 Keep-Alive 与 HTTP/2 连接复用率；
2. **全阶段连接与响应头超时防护**：显式配置 TCP 拨号超时（`10s`）、TLS 握手超时（`10s`）、空闲连接回收超时（`90s`）以及关键的上游响应头等待超时（`ResponseHeaderTimeout: 30s`），彻底杜绝上游挂死导致的协程阻塞；
3. **面向对象依赖隔离**：将定制的 `*http.Client` 封装于协议代理 `Server` 结构体实例中，完全消除对标准库包级全局 `http.DefaultClient` 的隐式耦合。

---

## 2. 核心架构与参数设计 (Architecture & Parameter Design)

### 2.1 标准库默认参数与定制传输层对比

| 传输层参数 | `http.DefaultTransport` 默认值 | `Feature 334` 定制生产值 | 调优收益说明 |
| :--- | :---: | :---: | :--- |
| **`MaxIdleConns`** | `100` | **`1000`** | 支撑全生态 8 大镜像源全局高并发长连接驻留 |
| **`MaxIdleConnsPerHost`** | `2` | **`100`** | 消除同一镜像源并发请求时的连接频繁销毁与 TLS 重握手开销 |
| **`IdleConnTimeout`** | `90s` | **`90s`** | 及时回收长期闲置的上游连接 |
| **`DialContext Timeout`** | `30s` | **`10s`** | 快速感知不可达或黑洞上游 IP，避免长时间卡顿 |
| **`TLSHandshakeTimeout`** | `10s` | **`10s`** | 防止恶劣网络下 TLS 握手无限挂起 |
| **`ResponseHeaderTimeout`** | **`0` (无限制)** | **`30s`** | **核心加固**：上游建立连接后若 30 秒内未返回响应头则主动中断报错，允许不限时长流式传输大体积响应体 |
| **`ExpectContinueTimeout`** | `1s` | **`1s`** | 保持标准 100-Continue 快速协商 |

> **关键设计说明**：我们显式采用 `Transport.ResponseHeaderTimeout`（仅限制从发送完请求到收到响应头的时间），而**不设置** `http.Client.Timeout`（后者会限制包含整个响应体下载在内的总时长，导致数 GB 的 Docker 层或大型安装包下载在中途被误杀中断）。大文件下载过程中的流停滞继续由并发下载合并模块的空闲看门狗（Watchdog）精准守护。

---

## 3. 详细模块设计 (Detailed Module Design)

### 3.1 协议代理与调度核心 (Proxy Server Module)
在 `Server` 结构体中新增专属 `httpClient *http.Client` 字段，并提供标准构造函数 `NewUpstreamTransport()`：

```go
// NewUpstreamTransport 创建针对高并发镜像代理优化的 HTTP Transport
func NewUpstreamTransport() *http.Transport {
    return &http.Transport{
        Proxy: http.ProxyFromEnvironment,
        DialContext: (&net.Dialer{
            Timeout:   10 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          1000,
        MaxIdleConnsPerHost:   100,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
    }
}
```

在 `NewServer` 初始化时，实例化包含该 `Transport` 的 `http.Client`（并保持与现有重定向行为一致），将下载流程、免缓存透传流程、Range 分片流程以及 HEAD 探测流程中所有的 `http.DefaultClient.Do(req)` 统一替换为 `s.httpClient.Do(req)`。

---

## 4. 验证场景与测试标准 (Verification & Test Criteria)

1. **传输层连接池参数初始化验证**：
   - 在单元测试中校验 `NewUpstreamTransport()` 及 `Server` 实例内部 `httpClient.Transport` 的 `MaxIdleConnsPerHost`、`ResponseHeaderTimeout`、`DialContext` 等各项配置值符合规范要求。
2. **上游响应头挂死超时中断验证**：
   - 启动一个模拟假死的测试上游服务（接受连接并读取请求后，挂起不发送响应头）；
   - 将代理实例的 `ResponseHeaderTimeout` 调小（如 `100ms`）发起代理请求，验证代理能在超时阈值到达时迅速中断上游等待并返回错误状态码，无协程永久泄漏。
3. **大体积流式下载不受 `ResponseHeaderTimeout` 误杀验证**：
   - 验证只要上游在阈值内返回了 `200 OK` 响应头，后续持续传输时间超过 `ResponseHeaderTimeout` 的分块数据流依然能够完整下载落盘。
