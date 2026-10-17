package core

// 发布页域名采集：站源内置域名失效时，从「发布页」按规则提取候选域名，
// 逐个探测可用性，把第一个可达的域名作为该站源的运行时 Base 覆盖内置地址。
// 配置与探测结果都持久化在数据目录 publish_sources.json，重启后继续生效。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	publishMaxFileBytes = 64 * 1024
	publishMaxCandidates = 12
	publishProbeTimeout = 6 * time.Second
	publishPageTimeout = 12 * time.Second
	publishAutoInterval = 20 * time.Minute
	publishAutoStartDelay = 8 * time.Second
)

type publishConfig struct {
	URL  string `json:"url"`
	Rule string `json:"rule,omitempty"`
}

type publishDomain struct {
	Base      string    `json:"base"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type publishRegistry struct {
	mu      sync.RWMutex
	path    string
	configs map[string]publishConfig
	domains map[string]publishDomain
}

func newPublishRegistry(directory string) *publishRegistry {
	return &publishRegistry{
		path:    filepath.Join(directory, "publish_sources.json"),
		configs: map[string]publishConfig{},
		domains: map[string]publishDomain{},
	}
}

func (registry *publishRegistry) load() {
	body, err := os.ReadFile(registry.path)
	if err != nil || len(body) > publishMaxFileBytes {
		return
	}
	var record struct {
		Configs map[string]publishConfig `json:"configs"`
		Domains map[string]publishDomain `json:"domains"`
	}
	if json.Unmarshal(body, &record) != nil {
		return
	}
	registry.mu.Lock()
	registry.configs = map[string]publishConfig{}
	registry.domains = map[string]publishDomain{}
	for id, config := range record.Configs {
		config.URL = strings.TrimSpace(config.URL)
		if config.URL == "" {
			continue
		}
		registry.configs[normalizePublishSourceID(id)] = config
	}
	for id, domain := range record.Domains {
		domain.Base = strings.TrimRight(strings.TrimSpace(domain.Base), "/")
		if domain.Base == "" {
			continue
		}
		registry.domains[normalizePublishSourceID(id)] = domain
	}
	registry.mu.Unlock()
}

// 调用方必须已持有 registry.mu 写锁。
func (registry *publishRegistry) save() error {
	record := struct {
		Configs map[string]publishConfig `json:"configs"`
		Domains map[string]publishDomain `json:"domains"`
	}{Configs: registry.configs, Domains: registry.domains}
	body, err := json.Marshal(record)
	if err != nil || len(body) > publishMaxFileBytes {
		return errors.New("发布页配置过大")
	}
	return writeNativeCacheFile(registry.path, body)
}

// normalizePublishSourceID 让 custom: 前缀与规范源 ID 一致比较。
func normalizePublishSourceID(id string) string {
	return canonicalProviderSource(strings.TrimSpace(id))
}

func (registry *publishRegistry) configFor(id string) (publishConfig, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	config, found := registry.configs[normalizePublishSourceID(id)]
	return config, found
}

func (registry *publishRegistry) overrideFor(id string) string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.domains[normalizePublishSourceID(id)].Base
}

func (registry *publishRegistry) set(id string, config publishConfig) error {
	id = normalizePublishSourceID(id)
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("发布页地址需为 http 或 https 开头的完整地址")
	}
	if rule := strings.TrimSpace(config.Rule); rule != "" {
		if !strings.HasPrefix(rule, ".") {
			if _, err := regexp.Compile(rule); err != nil {
				return fmt.Errorf("域名规则不是有效的正则表达式：%w", publicError(err))
			}
		}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.configs[id] = publishConfig{URL: config.URL, Rule: strings.TrimSpace(config.Rule)}
	return registry.save()
}

func (registry *publishRegistry) remove(id string) error {
	id = normalizePublishSourceID(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.configs, id)
	delete(registry.domains, id)
	return registry.save()
}

func (registry *publishRegistry) recordDomain(id, base string) {
	id = normalizePublishSourceID(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.domains[id] = publishDomain{Base: base, UpdatedAt: time.Now()}
	_ = registry.save()
}

func (registry *publishRegistry) clearDomain(id string) {
	id = normalizePublishSourceID(id)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.domains[id]; !exists {
		return
	}
	delete(registry.domains, id)
	_ = registry.save()
}

type publishStatus struct {
	Source    string        `json:"source"`
	URL       string        `json:"url"`
	Rule      string        `json:"rule"`
	Current   string        `json:"current"`
	UpdatedAt time.Time     `json:"updatedAt,omitempty"`
}

func (registry *publishRegistry) snapshot() []publishStatus {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	items := make([]publishStatus, 0, len(registry.configs))
	for id, config := range registry.configs {
		domain := registry.domains[id]
		items = append(items, publishStatus{
			Source:    id,
			URL:       config.URL,
			Rule:      config.Rule,
			Current:   domain.Base,
			UpdatedAt: domain.UpdatedAt,
		})
	}
	return items
}

// publishResolvedBase 返回发布页采集到的域名，作为站源 Base 的最高优先级覆盖。
func publishResolvedBase(id string) string {
	engine := nativeEngineSnapshot()
	if engine == nil || engine.publishRegistry == nil {
		return ""
	}
	return engine.publishRegistry.overrideFor(id)
}

var (
	publishLinkPattern = regexp.MustCompile(`(?i)https?://[a-z0-9.-]+(?::\d+)?`)
	publishHostPattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b`)
)

// extractPublishCandidates 按规则从发布页内容提取候选域名：
//   - 规则为空：提取页面里全部链接域名（保序去重）
//   - 规则以 . 开头：只保留以该后缀结尾的域名
//   - 其余规则按正则处理：取捕获组（若有）或整段匹配，再从中取域名
func extractPublishCandidates(body, rule string) ([]string, error) {
	rule = strings.TrimSpace(rule)
	var hosts []string
	if rule == "" {
		hosts = publishLinkPattern.FindAllString(body, -1)
	} else if strings.HasPrefix(rule, ".") {
		suffix := strings.ToLower(rule)
		for _, link := range publishLinkPattern.FindAllString(body, -1) {
			parsed, err := url.Parse(link)
			if err == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), suffix) {
				hosts = append(hosts, link)
			}
		}
	} else {
		expression, err := regexp.Compile(rule)
		if err != nil {
			return nil, fmt.Errorf("域名规则无效：%w", publicError(err))
		}
		for _, match := range expression.FindAllStringSubmatch(body, -1) {
			value := match[0]
			if len(match) >= 2 && strings.TrimSpace(match[1]) != "" {
				value = match[1]
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if strings.Contains(value, "://") {
				hosts = append(hosts, value)
			} else {
				host := publishHostPattern.FindString(value)
				if host != "" {
					hosts = append(hosts, "https://"+host)
				}
			}
		}
	}
	seen := map[string]bool{}
	candidates := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimRight(strings.TrimSpace(host), "/")
		parsed, err := url.Parse(host)
		if err != nil || parsed.Host == "" {
			continue
		}
		key := strings.ToLower(parsed.Host)
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, parsed.Scheme+"://"+parsed.Host)
		if len(candidates) >= publishMaxCandidates {
			break
		}
	}
	return candidates, nil
}

// publishProbe 探测域名是否可达：能建立连接并拿到任意 HTTP 响应即视为可用
//（不少站点对脚本返回 403/404，但域名本身是活的）。
func (d *Downloader) publishProbe(ctx context.Context, base string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return false
	}
	request.Header.Set("User-Agent", userAgent)
	probeCtx, cancel := context.WithTimeout(ctx, publishProbeTimeout)
	defer cancel()
	response, err := d.client.Do(request.WithContext(probeCtx))
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return true
}

// publishRefresh 抓取发布页、按规则提取候选并探测，返回第一个可用域名。
func (d *Downloader) publishRefresh(ctx context.Context, source string) (string, error) {
	engine := nativeEngineSnapshot()
	if engine == nil || engine.publishRegistry == nil {
		return "", errors.New("本地核心尚未就绪")
	}
	config, found := engine.publishRegistry.configFor(source)
	if !found {
		return "", errors.New("该站源未配置发布页")
	}
	pageCtx, cancel := context.WithTimeout(ctx, publishPageTimeout)
	defer cancel()
	body, err := d.fetchProviderText(pageCtx, config.URL, config.URL)
	if err != nil {
		return "", fmt.Errorf("发布页获取失败：%w", publicError(err))
	}
	candidates, err := extractPublishCandidates(body, config.Rule)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", errors.New("发布页未提取到任何域名，请检查规则")
	}
	dead := publishResolvedBase(source)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if candidate == dead {
			continue
		}
		if d.publishProbe(ctx, candidate) {
			engine.publishRegistry.recordDomain(source, candidate)
			return candidate, nil
		}
	}
	return "", fmt.Errorf("发布页候选域名均不可达（共 %d 个）", len(candidates))
}

// publishAutoRefreshLoop 周期性检查已配置发布页的站源：
// 当前覆盖域名不可达（或尚未采集）时自动重新采集。
func (engine *nativeEngine) publishAutoRefreshLoop(ctx context.Context) {
	select {
	case <-time.After(publishAutoStartDelay):
	case <-ctx.Done():
		return
	}
	ticker := time.NewTicker(publishAutoInterval)
	defer ticker.Stop()
	for {
		if nativeEngineSnapshot() != engine {
			return
		}
		engine.publishAutoPass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (engine *nativeEngine) publishAutoPass(ctx context.Context) {
	if engine.publishRegistry == nil {
		return
	}
	for _, status := range engine.publishRegistry.snapshot() {
		if ctx.Err() != nil {
			return
		}
		current := status.Current
		if current != "" && engine.downloader.publishProbe(ctx, current) {
			continue
		}
		if current != "" {
			engine.publishRegistry.clearDomain(status.Source)
		}
		if _, err := engine.downloader.publishRefresh(ctx, status.Source); err != nil {
			continue
		}
	}
}
