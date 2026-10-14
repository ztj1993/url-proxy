package config

import (
	"bufio"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// NoCacheRule 免缓存与缓存保护规则 (Feature 305)
type NoCacheRule struct {
	Scope   string         // 作用域: "*", "/dists/", "archive.ubuntu.com", "archive.ubuntu.com/dists/"
	Action  string         // 动作: "bypass" (免缓存) 或 "cache" (白名单例外，强制缓存)
	Type    string         // 匹配类型: "exact", "prefix", "suffix", "regex"
	Pattern string         // 匹配模式串
	Regexp  *regexp.Regexp // 当 Type 为 "regex" 时的预编译正则表达式
	TTL     time.Duration  // 软过期保鲜时长 (Feature 318, 若为 0 则为纯流式直通)
}

// MatchScope 检查请求 Host 与 Path 是否匹配规则作用域
func (r *NoCacheRule) MatchScope(reqHost, reqPath string) bool {
	if r.Scope == "" || r.Scope == "*" {
		return true
	}

	scopeLower := strings.ToLower(r.Scope)
	reqHostLower := strings.ToLower(reqHost)
	reqPathLower := strings.ToLower(reqPath)

	// 路径前缀作用域: 以 "/" 开头，例如 "/dists/"
	if strings.HasPrefix(scopeLower, "/") {
		return strings.Contains(reqPathLower, scopeLower)
	}

	// 域名或 域名+路径 复合作用域: 例如 "archive.ubuntu.com/dists/"
	hostAndPath := reqHostLower + reqPathLower
	if strings.Contains(scopeLower, "/") {
		return strings.Contains(hostAndPath, scopeLower)
	}

	// 纯域名作用域: 例如 "archive.ubuntu.com"
	return reqHostLower == scopeLower || strings.HasPrefix(reqHostLower, scopeLower+":")
}

// MatchPattern 检查 URL 路径及文件名是否符合匹配模式
func (r *NoCacheRule) MatchPattern(urlPath, baseName string) bool {
	if r.Type == "*" || r.Type == "all" || r.Pattern == "*" {
		return true
	}
	baseLower := strings.ToLower(baseName)
	pathLower := strings.ToLower(urlPath)
	patternLower := strings.ToLower(r.Pattern)

	switch r.Type {
	case "exact":
		return baseLower == patternLower || strings.Trim(pathLower, "/") == strings.Trim(patternLower, "/")
	case "prefix":
		return strings.HasPrefix(baseLower, patternLower) || strings.HasPrefix(pathLower, patternLower)
	case "suffix":
		return strings.HasSuffix(baseLower, patternLower) || strings.HasSuffix(pathLower, patternLower)
	case "contains":
		return strings.Contains(pathLower, patternLower) || strings.Contains(baseLower, patternLower)
	case "regex":
		if r.Regexp != nil {
			return r.Regexp.MatchString(urlPath) || r.Regexp.MatchString(baseName)
		}
	}
	return false
}

// TypeDefinition 内容类型定义 (Feature 311)
type TypeDefinition struct {
	Name     string   // 类型别名 (如 "html", "json", "pypi", "text")
	Mimes    []string // 匹配的 Content-Type 列表
	Suffixes []string // 匹配的 URL/文件名后缀列表
}

// DefaultTypeDefinitions 获取系统内置默认内容类型
func DefaultTypeDefinitions() map[string]TypeDefinition {
	return map[string]TypeDefinition{
		"html": {
			Name:     "html",
			Mimes:    []string{"text/html", "application/xhtml+xml"},
			Suffixes: []string{".html", ".htm", ".xhtml"},
		},
		"json": {
			Name:     "json",
			Mimes:    []string{"application/json", "application/vnd.npm.install-v1+json"},
			Suffixes: []string{".json"},
		},
		"xml": {
			Name:     "xml",
			Mimes:    []string{"text/xml", "application/xml"},
			Suffixes: []string{".xml"},
		},
		"pypi": {
			Name:     "pypi",
			Mimes:    []string{"text/html", "application/vnd.pypi.simple"},
			Suffixes: []string{".html"},
		},
		"npm": {
			Name:     "npm",
			Mimes:    []string{"application/json", "application/vnd.npm"},
			Suffixes: []string{".json"},
		},
		"text": {
			Name:     "text",
			Mimes:    []string{"text/"},
			Suffixes: []string{".txt", ".text"},
		},
	}
}

// RewriteRule 响应体动态重写规则 (Feature 311)
type RewriteRule struct {
	Scope       string // 生效的作用域路径前缀 (如 "/pypi/simple/", "/npm/")
	TypeSpec    string // 内容类型规格 (如 "type:json", "type:pypi", "mime:text/html", "suffix:.json", "*" 或空)
	Pattern     string // 待匹配的目标字符串 (如 "https://files.pythonhosted.org/")
	Replacement string // 替换目标字符串模板，支持 {PROXY_HOST} 宏变量
}

// ProxyConfig 统一代理配置结构
type ProxyConfig struct {
	Aliases      map[string]string         // 短别名映射 (Feature 306): short_alias -> upstream_base_url
	AliasPlugins map[string]string         // 别名绑定的路由插件 (Feature 317): short_alias -> plugin_name
	Forwards     map[string]string         // 域名转发映射 (Feature 103): source_host -> gateway_base_url
	NoCacheRules []NoCacheRule             // 免缓存与白名单例外规则 (Feature 305)
	Types        map[string]TypeDefinition // 内容类型定义 (Feature 311): type_name -> TypeDefinition
	RewriteRules []RewriteRule             // 响应体动态重写规则 (Feature 311)
	SourceFile   string                    // 实际生效的配置文件路径或说明
}

// NewProxyConfig 初始化统一配置对象
func NewProxyConfig() *ProxyConfig {
	return &ProxyConfig{
		Aliases:      make(map[string]string),
		AliasPlugins: make(map[string]string),
		Forwards:     make(map[string]string),
		NoCacheRules: make([]NoCacheRule, 0),
		Types:        DefaultTypeDefinitions(),
		RewriteRules: make([]RewriteRule, 0),
	}
}

// Parse 解析 proxy.conf 统一配置流
func Parse(r io.Reader) (*ProxyConfig, error) {
	cfg := NewProxyConfig()
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		directive := strings.ToLower(fields[0])
		switch directive {
		case "alias":
			if len(fields) >= 3 {
				name := strings.TrimPrefix(fields[1], "/")
				cfg.Aliases[name] = fields[2]
				for _, opt := range fields[3:] {
					optLower := strings.ToLower(opt)
					if strings.HasPrefix(optLower, "plugin:") {
						cfg.AliasPlugins[name] = strings.TrimPrefix(optLower, "plugin:")
					}
				}
			} else {
				log.Printf("invalid alias directive (expected 'alias <name> <upstream_url> [plugin:<name>]'): %s", line)
			}
		case "forward":
			if len(fields) >= 3 {
				cfg.Forwards[fields[1]] = fields[2]
			} else {
				log.Printf("invalid forward directive (expected 'forward <host> <gateway_url>'): %s", line)
			}
		case "bypass", "nocache":
			var ttl time.Duration
			cleanFields := make([]string, 0, len(fields))
			for _, f := range fields {
				fLower := strings.ToLower(f)
				if strings.HasPrefix(fLower, "ttl:") {
					durStr := strings.TrimPrefix(fLower, "ttl:")
					if d, err := time.ParseDuration(durStr); err == nil {
						ttl = d
					} else {
						log.Printf("invalid ttl duration in bypass rule: %s: %v", f, err)
					}
				} else {
					cleanFields = append(cleanFields, f)
				}
			}

			if len(cleanFields) >= 4 {
				rule := NoCacheRule{
					Scope:   cleanFields[1],
					Action:  "bypass",
					Type:    strings.ToLower(cleanFields[2]),
					Pattern: strings.Join(cleanFields[3:], " "),
					TTL:     ttl,
				}
				if rule.Type == "regex" {
					re, err := regexp.Compile(rule.Pattern)
					if err != nil {
						log.Printf("invalid regex in bypass rule: %s: %v", rule.Pattern, err)
						continue
					}
					rule.Regexp = re
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else if len(cleanFields) == 2 || (len(cleanFields) == 3 && cleanFields[2] == "*") {
				rule := NoCacheRule{
					Scope:   cleanFields[1],
					Action:  "bypass",
					Type:    "*",
					Pattern: "*",
					TTL:     ttl,
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else {
				log.Printf("invalid bypass directive: %s", line)
			}
		case "cache":
			if len(fields) >= 4 {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "cache",
					Type:    strings.ToLower(fields[2]),
					Pattern: strings.Join(fields[3:], " "),
				}
				if rule.Type == "regex" {
					re, err := regexp.Compile(rule.Pattern)
					if err != nil {
						log.Printf("invalid regex in cache rule: %s: %v", rule.Pattern, err)
						continue
					}
					rule.Regexp = re
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else if len(fields) == 3 && fields[2] == "*" {
				rule := NoCacheRule{
					Scope:   fields[1],
					Action:  "cache",
					Type:    "*",
					Pattern: "*",
				}
				cfg.NoCacheRules = append(cfg.NoCacheRules, rule)
			} else {
				log.Printf("invalid cache directive: %s", line)
			}
		case "type":
			if len(fields) >= 2 {
				name := strings.ToLower(fields[1])
				tDef, exists := cfg.Types[name]
				if !exists {
					tDef = TypeDefinition{Name: name}
				}
				for _, spec := range fields[2:] {
					lowerSpec := strings.ToLower(spec)
					if strings.HasPrefix(lowerSpec, "mime:") {
						val := spec[5:]
						tDef.Mimes = append(tDef.Mimes, strings.ToLower(val))
					} else if strings.HasPrefix(lowerSpec, "suffix:") {
						ext := spec[7:]
						if !strings.HasPrefix(ext, ".") {
							ext = "." + ext
						}
						tDef.Suffixes = append(tDef.Suffixes, strings.ToLower(ext))
					} else {
						if strings.Contains(spec, "/") {
							tDef.Mimes = append(tDef.Mimes, strings.ToLower(spec))
						} else {
							ext := spec
							if !strings.HasPrefix(ext, ".") {
								ext = "." + ext
							}
							tDef.Suffixes = append(tDef.Suffixes, strings.ToLower(ext))
						}
					}
				}
				cfg.Types[name] = tDef
			} else {
				log.Printf("invalid type directive (expected 'type <name> <matcher>...'): %s", line)
			}
		case "rewrite":
			if len(fields) >= 5 {
				rule := RewriteRule{
					Scope:       fields[1],
					TypeSpec:    fields[2],
					Pattern:     fields[3],
					Replacement: fields[4],
				}
				cfg.RewriteRules = append(cfg.RewriteRules, rule)
			} else if len(fields) == 4 {
				rule := RewriteRule{
					Scope:       fields[1],
					TypeSpec:    "*",
					Pattern:     fields[2],
					Replacement: fields[3],
				}
				cfg.RewriteRules = append(cfg.RewriteRules, rule)
			} else {
				log.Printf("invalid rewrite directive (expected 'rewrite <scope> [type_spec] <pattern> <replacement>'): %s", line)
			}
		default:
			log.Printf("unrecognized config directive: %s", line)
		}
	}
	return cfg, scanner.Err()
}

// LogSummary 打印统一配置加载汇总日志
func (cfg *ProxyConfig) LogSummary() {
	log.Printf("proxy configuration summary [%s]: %d aliases, %d forwards, %d cache/bypass rules, %d types, %d rewrite rules",
		cfg.SourceFile, len(cfg.Aliases), len(cfg.Forwards), len(cfg.NoCacheRules), len(cfg.Types), len(cfg.RewriteRules))
}

// Load 加载统一代理配置，支持多级自动探测机制
func Load(confPath string) *ProxyConfig {
	// 1. 若用户在命令行显式指定了自定义路径（且不是默认未指定的 "proxy.conf"）
	if confPath != "" && confPath != "proxy.conf" {
		if f, err := os.Open(confPath); err == nil {
			defer f.Close()
			cfg, err := Parse(f)
			if err == nil {
				cfg.SourceFile = confPath
				log.Printf("proxy config loaded from explicit CLI argument: %s", confPath)
				cfg.LogSummary()
				return cfg
			}
			log.Printf("failed to parse config file %s: %v", confPath, err)
		} else {
			log.Printf("specified config file not found: %s: %v", confPath, err)
		}
	}

	// 2. 自动探测 proxy.conf (优先当前目录，次选二进制所在目录)
	searchPaths := []string{"proxy.conf"}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		searchPaths = append(searchPaths, filepath.Join(exeDir, "proxy.conf"))
	}

	for _, p := range searchPaths {
		if f, err := os.Open(p); err == nil {
			defer f.Close()
			cfg, err := Parse(f)
			if err == nil {
				cfg.SourceFile = p
				log.Printf("proxy config file loaded: %s", p)
				cfg.LogSummary()
				return cfg
			}
			log.Printf("failed to parse config file %s: %v", p, err)
		}
	}

	// 3. 未找到配置文件，使用默认空配置
	cfg := NewProxyConfig()
	cfg.SourceFile = "(none)"
	log.Printf("no proxy configuration found, using empty default configuration")
	return cfg
}
