# 域名规则转发配置设计方案（已废弃 / DEPRECATED）

> [!WARNING]
> **【已废弃 / DEPRECATED】**
> - **废弃状态**：本文档所记载的 `forwarding.conf` 独立配置文件及其无指令动词语法（`<域名> <前缀>`）已**全面废弃并移除向后兼容**。
> - **演进替代方案**：域名转发（Host Forwarding）功能现已并入 **Feature 304 统一代理配置系统 (`proxy.conf`)**，语法规范升级为单一真理源指令：`forward <源Host> <前置代理BaseURL>`。完整规范请参见 Feature 304 统一代理配置系统 (`proxy.conf`)。
> - **测试状态**：对应的测试套件 `tests/103_host_forwarding` 已标记为废弃，并在后续自动化测试运行中显式跳过（SKIP）。最新测试用例请参见 `tests/304_unified_config`。

本文档仅保留作为历史版本归档参考。

## 1. 功能目标 (Goal)
支持针对特定域名（Host）定义重定向或镜像网关前缀转发规则。使得代理在请求该域名时，能够自动重写上游请求地址，方便在内网环境下搭建前置镜像加速源、内网镜像源或针对特定源站进行前置网关转发。

## 2. 核心机制 (Core Mechanism)
采用 **基于配置文件的域名匹配与前缀拼接 (Rule-based Host Rewriting)**：

- **规则文件自动发现**：服务启动时依次检测当前工作目录及可执行文件所在目录下的 `forwarding.conf` 文件，无需繁琐的命令行参数传入。
- **逐行映射解析**：配置文件按行读取，每行格式为 `<目标Host> <重定向或前置代理BaseURL>`，使用空格或制表符分隔。
- **URL 动态前缀拼接**：当请求的 `uris.Host` 命中映射规则时，自动构造形如 `<前置BaseURL>/<原URI>` 的目标请求，透明切换上游拉取链路。

## 3. 技术方案 (Technical Solution)

### 3.1 配置文件格式 (`forwarding.conf`)
```text
# 语法: <域名> <转发目标地址前缀>
registry-1.docker.io http://mirror.local/docker
github.com https://ghproxy.example.com
```

### 3.2 规则自动载入 (`forwardConfig`)
```go
func forwardConfig() map[string]string {
    forwards := make(map[string]string)
    configFiles := []string{
        "forwarding.conf", // 当前工作目录
    }
    if exePath, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exePath)
        configFiles = append(configFiles, filepath.Join(exeDir, "forwarding.conf"))
    }
    // 扫描文件并按行解析
    ...
    return forwards
}
```

### 3.3 运行时目标重写
```go
targetUri := uri
if _, ok := forwards[uris.Host]; ok {
    targetUri = forwards[uris.Host] + "/" + uri
}
log.Printf("[%d] job download: %s", reqID, targetUri)
```

## 4. 预期收益 (Benefits)
1. **透明无感重定向**：客户端请求保持原生域名不变，上游下载链路可由运维人员按需重定向至高速镜像或内网加速代理。
2. **免维护即时加载**：默认探测当前工作目录下的规则文件，支持开箱即用。
3. **高扩展性**：每个域名均可指定独立的前置服务前缀，互不影响。
