# 统一代理配置文件系统设计方案

## 1. 功能目标 (Goal)

构建统一、规范、集中式的代理配置文件系统（Unified Proxy Configuration System），彻底解决多配置文件碎片化与 CLI 参数膨胀问题：
1. **统一真理源 (`proxy.conf`)**：通过单一文件集中纳管路径短别名路由（`alias`）、域名规则转发（`forward`）、免缓存规则（`bypass`/`nocache`）以及白名单例外（`cache`）。
2. **纯配置驱动，零代码硬编码 (Zero Code Hardcoding)**：所有路由与缓存规则完全由配置文件定义，Go 核心代码中不包含任何包格式或特定前缀的硬编码。
3. **单行指令语法风格 (Single-Line Directive Syntax)**：采用整洁、清晰的纯单行指令语法，具备极高的可读性与自动化运维便利性。
4. **统一命令行参数与多级自动探测**：提供 `-conf <path>` 统一参数；支持当前工作目录、可执行文件目录的多级优先探测。

---

## 2. 语法规范与核心指令 (`proxy.conf`)

配置文件支持以 `#` 开头的单行注释以及空行。规则主体为基于空格或制表符切分的单行指令：

### 2.1 核心指令表

| 指令前缀 | 完整语法格式 | 语义说明 | 引入阶段 |
| :--- | :--- | :--- | :--- |
| **`alias`** | `alias <短别名> <上游BaseURL>` | 声明 URL 第一级路径短别名路由映射 | Feature 306 |
| **`forward`** | `forward <源Host> <网关BaseURL>` | 声明源站 Host 重写与镜像网关引流 | Feature 103 |
| **`bypass`**<br>*(或 `nocache`)* | `bypass <Scope> <Type> <Pattern>` | 声明动态资源免缓存规则，纯流式透传 | Feature 305 |
| **`cache`** | `cache <Scope> <Type> <Pattern>` | 声明强制落盘缓存与白名单例外，优先级高于 bypass | Feature 305 |

### 2.2 作用域 (`Scope`) 与匹配类型 (`Type`)
- **Scope（作用域）**：
  - `*`：全局作用域；
  - `/prefix/`：请求路径包含指定前缀；
  - `domain.com`：特定源站 Host；
  - `domain.com/prefix/`：域名与路径复合作用域。
- **Type（匹配模式）**：
  - `exact`：严格文件名匹配；
  - `prefix`：文件名前缀匹配；
  - `suffix`：文件名后缀匹配；
  - `regex`：完整路径或文件名正则表达式匹配。

---

## 3. 架构设计与技术实现 (Technical Solution)

### 3.1 核心数据结构 (`ProxyConfig`)
```go
// NoCacheRule 免缓存与缓存保护规则
type NoCacheRule struct {
	Scope   string         // 作用域: "*", "/dists/", "archive.ubuntu.com", 等
	Action  string         // 动作: "bypass" 或 "cache"
	Type    string         // 匹配类型: "exact", "prefix", "suffix", "regex"
	Pattern string         // 模式串
	Regexp  *regexp.Regexp // 当 Type 为 "regex" 时的预编译正则表达式
}

// ProxyConfig 统一代理配置结构
type ProxyConfig struct {
	Aliases      map[string]string // 短别名映射: alias -> upstreamBaseUrl
	Forwards     map[string]string // 域名转发: host -> gatewayBaseUrl
	NoCacheRules []NoCacheRule     // 免缓存与白名单例外规则
	SourceFile   string            // 生效的配置文件路径
}
```

### 3.2 配置文件多级探测流程
```
                     启动 url-proxy -conf <path>
                                  │
           用户通过 -conf 显式传参指定且存在? ── 是 ──> 解析指定配置文件
                                  │ 否
                 当前工作目录存在 proxy.conf? ── 是 ──> 解析 CWD/proxy.conf
                                  │ 否
                二进制所在目录存在 proxy.conf? ── 是 ──> 解析 ExeDir/proxy.conf
                                   │ 否
                                   ▼
                      未找到配置文件，使用默认空配置
```

### 3.3 单行指令解析引擎 (`parseProxyConfig`)
解析器逐行读取配置流，去除行首尾空白并剔除注释行：
1. 提取指令动词（首字段，忽略大小写）。
2. 分流至 `alias`、`forward`、`bypass`/`nocache`、`cache` 分支处理器。
3. 非法指令或未知动词统一记录警告日志并安全忽略。
4. 正则表达式规则在加载阶段完成预编译并校验合法性，避免运行期编译损耗。

---

## 4. 验证与测试标准 (Verification)
1. **全指令解析测试**：验证 `alias`、`forward`、`bypass`、`cache` 的解析正确性。
2. **CLI 覆盖测试**：验证 `-conf <custom.conf>` 能够准确覆盖默认探测路径。
3. **自动发现测试**：验证在当前目录下自动发现并加载 `proxy.conf`。
