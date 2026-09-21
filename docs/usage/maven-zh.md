# Maven / Gradle 依赖仓库缓存代理使用指南

`url-proxy` 原生 100% 完美支持 Maven 2 仓库布局标准。Java 生态中的 Maven 和 Gradle 在拉取依赖构件时，均通过规范的 REST 路径从指定仓库中下载 `.pom` 与 `.jar`。

---

## 1. 服务端配置 (`proxy.conf`)

在 `proxy.conf` 中追加 Maven 中央仓库代理规则：

```ini
# --- 声明 Maven Central 上游镜像别名 ---
alias /maven https://repo1.maven.org/maven2

# --- 动静分离规则：版本元数据与快照元数据实时透传 ---
# maven-metadata.xml 包含版本列表与最新发布状态，不可持久化强缓存
bypass /maven/ exact maven-metadata.xml
bypass /maven/ suffix maven-metadata.xml.sha1
bypass /maven/ suffix maven-metadata.xml.md5
# 若涉及 SNAPSHOT 快照版本，亦需跳过缓存保证时效性
bypass /maven/ contains -SNAPSHOT

# --- 强制缓存规则：正式发布版依赖构件 100% 永久安全落盘 ---
# Maven Central 规则严格禁止对已发布的 Release 版本进行篡改覆盖
cache  /maven/ suffix .jar
cache  /maven/ suffix .pom
cache  /maven/ suffix .aar
cache  /maven/ suffix .war
cache  /maven/ suffix .sha1
cache  /maven/ suffix .md5
```

启动 `url-proxy`：
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. 客户端配置

### 方式 A：Maven (`~/.m2/settings.xml`)

在客户端机器的 `~/.m2/settings.xml` 中配置 `<mirrors>`：

```xml
<settings>
  <mirrors>
    <mirror>
      <id>url-proxy-mirror</id>
      <mirrorOf>central</mirrorOf>
      <name>Internal url-proxy Maven Mirror</name>
      <url>http://192.168.1.100:8888/maven</url>
    </mirror>
  </mirrors>
</settings>
```

### 方式 B：Gradle (`build.gradle` / `init.gradle`)

在项目根目录的 `build.gradle` 或全局 `~/.gradle/init.gradle` 中声明仓库：

```groovy
allprojects {
    repositories {
        maven {
            url "http://192.168.1.100:8888/maven"
            // 若为 HTTP 协议，Gradle 7+ 需显式允许不安全协议
            allowInsecureProtocol = true
        }
    }
}
```

---

## 3. 运行与验证

在 Java 项目中触发依赖解析与构建：

```bash
mvn clean compile
# 或 Gradle:
./gradlew build --refresh-dependencies
```

**服务端日志**：
- 查询 `maven-metadata.xml`：通过 `[BYPASS]` 流式实时拉取；
- 下载 `spring-boot-starter-web-3.2.0.jar` 等构件：首次 `[CACHE-MISS]` 写入磁盘，后续全公司/全集群构建全部 `[CACHE-HIT]`，显著缩短大型 Java 工程构建周期。
