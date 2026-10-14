# Feature 344: 磁盘时效缓存 HEAD 回源方法转换测试套件

## 1. 测试目标
验证 `internal/proxy/disk_ttl.go` 中针对时效缓存（Disk TTL）回源构建策略的加固实现：
1. **未缓存首拉 HEAD 强制转 GET 完整落盘**：客户端以 `HEAD` 发起未缓存资源的首次探测，源站接收到的是 `GET` 请求，客户端接收到 200 OK 且正文严格为 0 字节；本地磁盘成功持久化完整实体，杜绝 0 字节空文件损坏；随后的 `GET` 请求保鲜命中（`HIT-DISK-TTL`）且数据完整；
2. **过期资源更新原子覆盖**：预热旧文件并使其过期，源站更新内容，客户端以 `HEAD` 请求，旧文件被原子替换为新版本完整文件，杜绝空文件覆盖；随后的 `GET` 请求获取新版本；
3. **过期资源未改动 304 租期续约**：过期后客户端发 `HEAD` 请求，源站返回 304，本地文件 `mtime` 成功刷新，标记 `REVALIDATED-304`；
4. **并发 HEAD 与 GET 汇聚**：HEAD 率先到达成为 Leader，GET 后到作为 Follower 追读完整流，上游仅收到 1 次请求，两请求分别交付零正文与完整正文。

---

## 2. 套件文件结构
- `proxy_test.conf`：测试专属代理配置，定义 100ms TTL 时效规则；
- `mock_server.go`：模拟支持 HEAD 仅返回响应头、GET 返回实体正文以及 If-Modified-Since 304 协商的上游源站；
- `head_conversion_test.go`：覆盖上述 4 个核心场景的自动化测试用例；
- `README-zh.md` / `README-en.md`：中英双语测试指南。

---

## 3. 运行测试

```bash
# 运行本测试套件
go test -v -count=1 ./tests/344_disk_ttl_head_method_conversion/...

# 全量回归测试
go test -v -count=1 ./tests/...
```
