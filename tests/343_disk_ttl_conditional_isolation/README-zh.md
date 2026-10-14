# Feature 343: 磁盘时效缓存条件头单向隔离与防压缩测试套件

本目录提供 Feature 343（磁盘时效缓存条件头单向隔离、防 304 黑洞、防 ETag 绑架及防脏续期死锁）的独立测试环境与自动化验证套件。

---

## 1. 目录结构

- `proxy_test.conf`: Feature 343 专用测试配置文件，声明 `ttl:100ms` 的动态元数据规则。
- `mock_server.go`: 模拟上游源站（严格按照 RFC 9110 Section 13.2.2 评估 `If-None-Match` 与 `If-Modified-Since` 条件头）。
- `conditional_isolation_test.go`: 核心自动化测试套件，全面覆盖未建仓条件头剥离、过期重验证 ETag 剥离续期、客户端新/缓存旧版本自愈覆盖及出向防压缩守卫。
- `README-zh.md`: 中文测试说明。
- `README-en.md`: 英文测试说明。

---

## 2. 自动化测试执行

在项目根目录下执行：
```bash
go test -v -count=1 ./tests/343_disk_ttl_conditional_isolation/...
```

---

## 3. 测试覆盖场景

1. **未建仓首拉条件头剥离防 304 黑洞 (`TestUncachedFirstFetch_ConditionalStripped`)**：
   - 客户端携带 `If-None-Match`、`If-Modified-Since` 与 `Accept-Encoding` 请求未缓存的元数据；
   - 验证出向请求中条件标头与压缩标头全部被剥离，源站返回 200 OK，代理成功落盘建仓，彻底阻断 304 空包黑洞。

2. **过期重验证 ETag 剥离防 200 退化 (`TestExpiredRevalidation_ETagStripped_Renew304`)**：
   - 本地缓存过期，客户端携带旧的不匹配 ETag（`If-None-Match`）；
   - 验证出向请求剥离该 ETag，以本地真实 `mtime` 发起协商，源站返回 304，代理成功续期并标记 `REVALIDATED-304`。

3. **客户端新/缓存旧/源站新自愈测试 (`TestSelfHealing_ClientNewer_CacheOlder_OriginNewer`)**：
   - 代理本地为旧版本 $T_1$，源站发布了新版本 $T_2$，客户端已持有 $T_2$ 并携带 $T_2$ 条件头；
   - 验证代理出向请求严格使用本地真实修改时间 $T_1$ 发起协商，杜绝透传客户端条件头导致的“304 脏续期死锁”；
   - 验证源站返回 200 OK，代理拉取最新内容并原子覆盖本地旧缓存，实现缓存自愈。
