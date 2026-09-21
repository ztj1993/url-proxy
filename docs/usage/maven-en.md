# Maven / Gradle Dependency Repository Caching Proxy Usage Guide

`url-proxy` natively provides 100% out-of-the-box support for Maven 2 repository layout specifications. Java build tools (Maven and Gradle) retrieve `.pom` files and binary `.jar` artifacts using standard hierarchical REST paths.

---

## 1. Server Configuration (`proxy.conf`)

Add Maven Central proxy rules to `proxy.conf`:

```ini
# --- Upstream Maven Central alias ---
alias /maven https://repo1.maven.org/maven2

# --- Dynamic metadata: Passthrough without saving to disk ---
# maven-metadata.xml tracks versions and releases and must remain fresh
bypass /maven/ exact maven-metadata.xml
bypass /maven/ suffix maven-metadata.xml.sha1
bypass /maven/ suffix maven-metadata.xml.md5
# Pass through SNAPSHOT builds to prevent stale development dependencies
bypass /maven/ contains -SNAPSHOT

# --- Package whitelist: 100% persistent cache for release artifacts ---
# Release artifacts in Maven Central are permanently immutable
cache  /maven/ suffix .jar
cache  /maven/ suffix .pom
cache  /maven/ suffix .aar
cache  /maven/ suffix .war
cache  /maven/ suffix .sha1
cache  /maven/ suffix .md5
```

Start `url-proxy`:
```bash
url-proxy -conf proxy.conf -cache /var/cache/url-proxy -port 8888
```

---

## 2. Client Configuration

### Method A: Maven (`~/.m2/settings.xml`)

Configure `<mirrors>` in your client `~/.m2/settings.xml`:

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

### Method B: Gradle (`build.gradle` / `init.gradle`)

Declare the repository in `build.gradle` or globally in `~/.gradle/init.gradle`:

```groovy
allprojects {
    repositories {
        maven {
            url "http://192.168.1.100:8888/maven"
            // Gradle 7+ requires explicit flag for plain HTTP
            allowInsecureProtocol = true
        }
    }
}
```

---

## 3. Verification

Trigger dependency resolution in your Java project:

```bash
mvn clean compile
# or Gradle:
./gradlew build --refresh-dependencies
```

**Server Logs**:
- `maven-metadata.xml`: Logged as `[BYPASS]`, ensuring dynamic discovery.
- `spring-boot-starter-web-3.2.0.jar`: Downloaded and cached on first request (`[CACHE-MISS]`). Subsequent builds across your team or CI fleet hit `[CACHE-HIT]`, dramatically accelerating multi-module builds.
