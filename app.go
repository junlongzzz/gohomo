package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

const (
	// AppName 程序名称
	AppName = "Gohomo"
	// AppGitHubRepo 程序GitHub仓库
	AppGitHubRepo = "https://github.com/junlongzzz/gohomo"
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
	//go:embed static/*
	appStaticFiles embed.FS // 嵌入静态文件

	appConfigPath  string       // 应用配置文件路径
	appConfig      atomic.Value // store *AppConfig
	appConfigViper *viper.Viper // 配置文件解析器
)

func initAppConfig() {
	// 设置通知展示程序名称
	beeep.AppName = AppName

	// 初始化默认配置
	appConfig.Store(defaultAppConfig())

	appConfigPath = filepath.Join(workDir, "gohomo.yaml")
	if !isFileExist(appConfigPath) {
		// 不存在，创建默认初始化配置
		if err := writeAppConfig(appConfigPath, getAppConfig()); err != nil {
			log.Println("Failed to create app config:", err)
		}
	}

	appConfigViper = viper.NewWithOptions(viper.KeyDelimiter("::"))
	appConfigViper.SetConfigFile(appConfigPath)
	if err := appConfigViper.ReadInConfig(); err != nil {
		log.Println("Failed to read app config:", err)
	} else if err = loadAppConfig(true); err != nil {
		log.Println("Failed to load app config:", err)
	}

	enabled := isAutoStartEnabled()
	wanted := getAppConfig().AutoStart
	if enabled != wanted {
		if err := setAutoStart(wanted); err != nil {
			log.Println("Failed to set auto start:", err)
		}
	}

	watchAppConfig()
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

func loadAppConfig(write bool) error {
	tempConfig := defaultAppConfig()
	// 读取本地配置进行覆盖
	if err := appConfigViper.Unmarshal(tempConfig); err != nil {
		return err
	}

	normalizeAppConfig(tempConfig)
	migrateAppConfig(tempConfig)

	appConfig.Store(tempConfig)

	if write {
		if err := writeAppConfig(appConfigPath, tempConfig); err != nil {
			log.Println("Failed to write app config:", err)
		}
	}

	log.Println("App config loaded:", appConfigPath)
	return nil
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
	return appConfig.Load().(*AppConfig)
}

func writeAppConfig(path string, config *AppConfig) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func changeAppConfig(options ...AppConfigOption) {
	if len(options) == 0 {
		return
	}

	// 获取当前配置并复制一份，避免直接修改全局正在使用的指针
	currentConfig := getAppConfig()
	newConfig := *currentConfig

	for _, option := range options {
		option(&newConfig)
	}

	// 配置持久化
	if err := writeAppConfig(appConfigPath, &newConfig); err != nil {
		log.Println("Failed to write app config:", err)
	}
}

func watchAppConfig() {
	var last time.Time

	appConfigViper.OnConfigChange(func(e fsnotify.Event) {
		if e.Op != fsnotify.Write {
			return
		}

		// 防抖
		now := time.Now()
		if now.Sub(last) < 500*time.Millisecond {
			return
		}
		last = now

		oldConfig := getAppConfig()

		if err := loadAppConfig(false); err != nil {
			log.Println("Failed to reload app config:", err)
			return
		}

		newConfig := getAppConfig()

		// 设置开机自启动
		if oldConfig.AutoStart != newConfig.AutoStart {
			if err := setAutoStart(newConfig.AutoStart); err != nil {
				go messageBoxAlert(AppName, fmt.Sprint(err))
			}
		}

		// 重载核心日志配置
		if oldConfig.CoreLogEnabled != newConfig.CoreLogEnabled {
			coreLogWriter.Switch(newConfig.CoreLogEnabled)
		}

		// 重新加载核心配置
		if oldConfig.ProxyMode != newConfig.ProxyMode ||
			oldConfig.CoreRunMode != newConfig.CoreRunMode ||
			!reflect.DeepEqual(oldConfig.ProxyByPass, newConfig.ProxyByPass) ||
			!reflect.DeepEqual(oldConfig.CoreOverride, newConfig.CoreOverride) {
			if err := loadCoreConfig(); err != nil {
				go messageBoxAlert(AppName, fmt.Sprint(err))
			} else {
				// 重启核心
				if !restartCore() {
					go messageBoxAlert(AppName, I.TranSys("msg.error.core.restart_failed", nil))
				}
			}

			if newConfig.ProxyMode == ProxyModeSystem {
				// 设置系统代理
				setCoreProxy()
			} else {
				// 关闭系统代理
				unsetProxy()
			}
		}

		updateTrayMenu(newConfig)
	})

	appConfigViper.WatchConfig()
}
