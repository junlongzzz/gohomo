package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gen2brain/beeep"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

type AppConfig struct {
	Schema         string         `yaml:"$schema" mapstructure:"$schema"`                   // 配置文件模板
	AutoStart      bool           `yaml:"auto-start" mapstructure:"auto-start"`             // 是否开机自动启动
	ProxyByPass    []string       `yaml:"proxy-by-pass" mapstructure:"proxy-by-pass"`       // 代理白名单地址
	ProxyMode      string         `yaml:"proxy-mode" mapstructure:"proxy-mode"`             // 代理模式 close-关闭 system-系统 tun-tun(需要管理员权限运行)
	CoreLogEnabled bool           `yaml:"core-log-enabled" mapstructure:"core-log-enabled"` // 是否启用记录核心日志
	CoreRunMode    string         `yaml:"core-run-mode" mapstructure:"core-run-mode"`       // 运行模式 rule-规则 global-全局 direct-直连
	CoreOverride   map[string]any `yaml:"core-override" mapstructure:"core-override"`       // 核心覆盖配置
}

type AppConfigOption func(*AppConfig)

func WithProxyMode(proxyMode string) AppConfigOption {
	return func(config *AppConfig) {
		config.ProxyMode = proxyMode
	}
}

func WithCoreRunMode(coreRunMode string) AppConfigOption {
	return func(config *AppConfig) {
		config.CoreRunMode = coreRunMode
	}
}

func WithCoreLogEnabled(coreLogEnabled bool) AppConfigOption {
	return func(config *AppConfig) {
		config.CoreLogEnabled = coreLogEnabled
	}
}

func WithAutoStart(autoStart bool) AppConfigOption {
	return func(config *AppConfig) {
		config.AutoStart = autoStart
	}
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

const (
	// AppName 程序名称
	AppName = "Gohomo"
	// AppGitHubRepo 程序GitHub仓库
	AppGitHubRepo = "https://github.com/junlongzzz/gohomo"
	// AppGitHubRepoApi 程序GitHub仓库API接口地址前缀
	AppGitHubRepoApi = "https://api.github.com/repos/junlongzzz/gohomo"
	// CoreShowName 核心名称
	CoreShowName = "Mihomo"
	// CoreGitHubRepo 核心GitHub仓库
	CoreGitHubRepo = "https://github.com/MetaCubeX/mihomo"

	ProxyModeClose  = "close"
	ProxyModeSystem = "system"
	ProxyModeTun    = "tun"

	CoreRunModeRule   = "rule"
	CoreRunModeGlobal = "global"
	CoreRunModeDirect = "direct"
)

var (
	//go:embed static/icon.ico
	trayIcon []byte // 托盘图标
	//go:embed static/icon.png
	notificationIcon []byte // 通知图标

	appConfigPath    string                    // 应用配置文件路径
	appConfig        atomic.Pointer[AppConfig] // 当前生效的应用配置
	appConfigViper   *viper.Viper              // 配置文件解析器
	appUpdateRunning atomic.Bool               // 防止并发检查更新
	appConfigMutex   sync.Mutex                // 串行化配置变更（changeAppConfig / reloadFromDisk / 手动重启核心）
)

func initAppConfig() {
	// 设置通知展示程序名称
	beeep.AppName = AppName

	appConfigPath = filepath.Join(workDir, "gohomo.yaml")
	if !isFileExist(appConfigPath) {
		// 不存在则写入一份默认配置作为模板，方便用户照着改
		if err := writeAppConfig(appConfigPath, defaultAppConfig()); err != nil {
			log.Println("Failed to create app config:", err)
		}
	}

	appConfigViper = viper.NewWithOptions(viper.KeyDelimiter("::"))
	appConfigViper.SetConfigFile(appConfigPath)

	cfg, err := parseAppConfig()
	if err != nil {
		log.Println("Failed to load app config, falling back to default:", err)
		cfg = defaultAppConfig()
	} else if needsConfigRewrite() {
		// 解析配置成功后，再判断是否有配置文件 key 与程序不一致时才写回磁盘
		if err = writeAppConfig(appConfigPath, cfg); err != nil {
			log.Println("Failed to write app config:", err)
		}
	}
	appConfig.Store(cfg)

	// 同步开机自启动注册表项
	if isAutoStartEnabled() != cfg.AutoStart {
		if err = setAutoStart(cfg.AutoStart); err != nil {
			log.Println("Failed to set auto start:", err)
		}
	}
}

// 应用默认配置
func defaultAppConfig() *AppConfig {
	return &AppConfig{
		Schema:         AppGitHubRepo,
		AutoStart:      false,
		ProxyByPass:    defaultBypassHosts,
		ProxyMode:      ProxyModeSystem,
		CoreLogEnabled: false,
		CoreRunMode:    CoreRunModeRule,
		CoreOverride:   make(map[string]any),
	}
}

// parseAppConfig 从磁盘读取并规范化应用配置；不更新内存、不写盘
func parseAppConfig() (*AppConfig, error) {
	if err := appConfigViper.ReadInConfig(); err != nil {
		return nil, err
	}
	cfg := defaultAppConfig()
	if err := appConfigViper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	normalizeAppConfig(cfg)
	migrateAppConfig(cfg)

	log.Println("App config loaded:", appConfigPath)
	return cfg, nil
}

func normalizeAppConfig(config *AppConfig) {
	config.ProxyMode = strings.ToLower(strings.TrimSpace(config.ProxyMode))
	config.CoreRunMode = strings.ToLower(strings.TrimSpace(config.CoreRunMode))
}

func migrateAppConfig(config *AppConfig) {
	// 修复非法值
	switch config.ProxyMode {
	case ProxyModeClose,
		ProxyModeSystem,
		ProxyModeTun:
	default:
		config.ProxyMode = ProxyModeSystem
	}

	switch config.CoreRunMode {
	case CoreRunModeRule,
		CoreRunModeGlobal,
		CoreRunModeDirect:
	default:
		config.CoreRunMode = CoreRunModeRule
	}
}

func getAppConfig() *AppConfig {
	return appConfig.Load()
}

func writeAppConfig(path string, config *AppConfig) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// changeAppConfig 唯一的"主动修改配置"入口（托盘点击等 UI 触发）
// 流程：拷贝当前配置 → 应用 option → 写盘 → applyAppConfig（store + 应用生效 + 刷新菜单）
// 写盘失败则不更新内存，避免内存/磁盘分裂；写盘成功后 fsnotify 反弹会被 reloadFromDisk 用 DeepEqual 跳过
func changeAppConfig(options ...AppConfigOption) {
	if len(options) == 0 {
		return
	}

	appConfigMutex.Lock()
	defer appConfigMutex.Unlock()

	cfg := *getAppConfig()
	for _, opt := range options {
		opt(&cfg)
	}

	if err := writeAppConfig(appConfigPath, &cfg); err != nil {
		log.Println("Failed to write app config:", err)
		return
	}
	applyAppConfig(&cfg)
}

// applyAppConfig 把 newCfg store 到内存，按字段 diff 应用生效，最后刷新托盘菜单
// changeAppConfig（主动修改）和 reloadFromDisk（用户编辑 yaml）共用同一入口
func applyAppConfig(newCfg *AppConfig) {
	old := appConfig.Swap(newCfg)
	if old != nil {
		diffApply(old, newCfg)
	}
	updateTrayMenu(newCfg)
}

// diffApply 按字段差异应用生效
// 关键优化：close↔system 切换不重启核心，只调系统代理；
// 仅在 CoreRunMode / CoreOverride 变化、或切换 TUN 模式时才必须重启核心
func diffApply(old, new *AppConfig) {
	if old.AutoStart != new.AutoStart {
		if err := setAutoStart(new.AutoStart); err != nil {
			go messageBoxAlert(AppName, fmt.Sprint(err))
		}
	}
	if old.CoreLogEnabled != new.CoreLogEnabled {
		coreLogWriter.Switch(new.CoreLogEnabled)
	}

	// CoreRunMode/CoreOverride 改了 → 必须重启核心；TUN 是核心的 feature，所以切换 TUN 也要重启
	coreRestart := old.CoreRunMode != new.CoreRunMode ||
		!reflect.DeepEqual(old.CoreOverride, new.CoreOverride) ||
		(old.ProxyMode == ProxyModeTun) != (new.ProxyMode == ProxyModeTun)
	if coreRestart {
		applyCoreConfigChange(new)
		return
	}

	// 只是 close/system 切换或 ProxyByPass 改了，不重启核心，只刷系统代理
	proxyRefresh := old.ProxyMode != new.ProxyMode ||
		!reflect.DeepEqual(old.ProxyByPass, new.ProxyByPass)
	if proxyRefresh {
		refreshSystemProxy(new)
	}
}

// applyCoreConfigChange 重新加载核心配置 + 重启核心 + 按 ProxyMode 调系统代理
// 既用于 diffApply 的"必须重启核心"分支，也给托盘 重启核心 手动触发复用
func applyCoreConfigChange(cfg *AppConfig) {
	if err := loadCoreConfig(); err != nil {
		// 加载失败：旧核心仍在跑旧配置，代理保持原状不动
		go messageBoxAlert(AppName, fmt.Sprint(err))
		return
	}
	if !restartCore() {
		unsetCoreProxy()
		go messageBoxAlert(AppName, I.TranSys("msg.error.core.restart_failed", nil))
		return
	}
	refreshSystemProxy(cfg)
}

// restartCoreManually 供托盘"重启核心"手动触发：持 appConfigMutex 与配置变更串行，避免与连点 / 外部编辑并发
func restartCoreManually() {
	appConfigMutex.Lock()
	defer appConfigMutex.Unlock()
	applyCoreConfigChange(getAppConfig())
}

// refreshSystemProxy 按 ProxyMode 设置/清理系统代理。close 和 tun 模式都不开系统代理
func refreshSystemProxy(cfg *AppConfig) {
	if cfg.ProxyMode == ProxyModeSystem {
		setCoreProxy()
	} else {
		unsetCoreProxy()
	}
}

func watchAppConfig() {
	var debounceTimer *time.Timer

	// OnConfigChange 由 viper 内部单一 fsnotify goroutine 串行调用
	appConfigViper.OnConfigChange(func(e fsnotify.Event) {
		if e.Op != fsnotify.Write {
			return
		}
		// 尾随防抖：每次事件都重置 timer，500ms 内无新事件才真正执行
		// 避免 Windows 上 fsnotify 对一次写入触发两次事件时丢掉最终状态
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(500*time.Millisecond, reloadFromDisk)
	})

	appConfigViper.WatchConfig()
}

// reloadFromDisk fsnotify 防抖触发：重读 yaml 跟内存对比，不同就走 applyAppConfig
// 程序自己写盘（changeAppConfig）也会触发 fsnotify，但 reload 后跟内存一致会被 DeepEqual 跳过
// 所以本函数只服务"用户外部编辑 yaml"的场景
func reloadFromDisk() {
	appConfigMutex.Lock()
	defer appConfigMutex.Unlock()

	cfg, err := parseAppConfig()
	if err != nil {
		log.Println("Failed to reload app config:", err)
		return
	}
	if reflect.DeepEqual(getAppConfig(), cfg) {
		return // 程序自己写盘的反弹，跳过
	}
	applyAppConfig(cfg)
}

func checkAppUpdate() {
	if !appUpdateRunning.CompareAndSwap(false, true) {
		return // 已有更新检查在进行中
	}
	defer appUpdateRunning.Store(false)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/releases/latest", AppGitHubRepoApi), nil)
	if err != nil {
		go messageBoxAlert(AppName, fmt.Sprintf("Failed to check update: %v", err))
		return
	}
	// GitHub API 拒绝无 UA 的请求
	req.Header.Set("User-Agent", AppName)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		go messageBoxAlert(AppName, fmt.Sprintf("Failed to check update: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		go messageBoxAlert(AppName, fmt.Sprintf("Update check failed: HTTP %d", resp.StatusCode))
		return
	}

	var release GitHubRelease
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		go messageBoxAlert(AppName, fmt.Sprintf("Failed to read response: %v", err))
		return
	}

	latestVersion := release.TagName
	if latestVersion != "" && version != "" && latestVersion != version {
		if messageBoxConfirm(AppName, I.TranSys("msg.info.update_available", map[string]any{"Version": latestVersion})) {
			downloadUrl := fmt.Sprintf("%s/releases/download/%s/gohomo-%s-%s-%s.zip", AppGitHubRepo, latestVersion, runtime.GOOS, runtime.GOARCH, latestVersion)
			log.Println("Update package download url:", downloadUrl)
			_ = shellOpen(downloadUrl)
		}
	} else {
		go messageBoxAlert(AppName, I.TranSys("msg.info.no_update", nil))
	}
}

// needsConfigRewrite 检查配置文件的 key 是否与 AppConfig 结构体一致
// 返回 true 表示存在不一致，需要回写配置文件
func needsConfigRewrite() bool {
	// 用 viper 已解析的结果，避免重复读文件
	fileKeys := appConfigViper.AllSettings()

	// 获取结构体 mapstructure tag 作为合法 key 集合
	structFields := make(map[string]struct{})
	t := reflect.TypeOf(AppConfig{})
	for i := range t.NumField() {
		if tag, ok := t.Field(i).Tag.Lookup("mapstructure"); ok && tag != "" {
			structFields[tag] = struct{}{}
		}
	}

	// 检查结构体字段是否全部存在于文件中
	for k := range structFields {
		if _, ok := fileKeys[k]; !ok {
			return true
		}
	}
	// 文件有结构体不认识的 key（拼写错误/废弃字段）
	for k := range fileKeys {
		if _, ok := structFields[k]; !ok {
			return true
		}
	}

	return false
}
