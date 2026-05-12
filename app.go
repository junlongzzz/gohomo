package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	ProxyByPass    []string       `yaml:"proxy-by-pass" mapstructure:"proxy-by-pass"`       // 代理白名单地址
	ProxyMode      string         `yaml:"proxy-mode" mapstructure:"proxy-mode"`             // 代理模式 close-关闭 system-系统 tun-tun(需要管理员权限运行)
	CoreLogEnabled bool           `yaml:"core-log-enabled" mapstructure:"core-log-enabled"` // 是否启用记录核心日志
	CoreRunMode    string         `yaml:"core-run-mode" mapstructure:"core-run-mode"`       // 运行模式 rule-规则 global-全局 direct-直连
	CoreOverride   map[string]any `yaml:"core-override" mapstructure:"core-override"`       // 核心覆盖配置
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
	appIconBytes   []byte       // 程序图标数据
)

func initAppConfig() {
	// 设置通知展示程序名称
	beeep.AppName = AppName

	// 读取程序图标数据
	iconBytes, err := appStaticFiles.ReadFile("static/icon.ico")
	if err != nil {
		log.Println("Failed to read app icon:", err)
	} else {
		appIconBytes = iconBytes
	}

	// 初始化默认配置
	appConfig.Store(defaultAppConfig())

	appConfigPath = filepath.Join(workDir, "gohomo.yaml")
	if !isFileExist(appConfigPath) {
		// 不存在，创建默认初始化配置
		if err := writeAppConfig(appConfigPath); err != nil {
			log.Println("Failed to create app config:", err)
		}
	}

	appConfigViper = viper.New()
	appConfigViper.SetConfigFile(appConfigPath)
	if err := appConfigViper.ReadInConfig(); err != nil {
		log.Println("Failed to read app config:", err)
	} else if err = loadAppConfig(true); err != nil {
		log.Println("Failed to load app config:", err)
	}
	watchAppConfig()
}

// 应用默认配置
func defaultAppConfig() *AppConfig {
	return &AppConfig{
		Schema:         AppGitHubRepo,
		ProxyByPass:    defaultBypassHosts,
		ProxyMode:      ProxyModeSystem,
		CoreLogEnabled: false,
		CoreRunMode:    CoreRunModeRule,
		CoreOverride:   make(map[string]any),
	}
}

func loadAppConfig(write bool) error {
	// 拷贝内存中配置
	tempConfig := defaultAppConfig()
	// 读取本地配置进行覆盖
	if err := appConfigViper.Unmarshal(tempConfig); err != nil {
		return err
	}
	// 转为小写
	tempConfig.ProxyMode = strings.ToLower(tempConfig.ProxyMode)
	tempConfig.CoreRunMode = strings.ToLower(tempConfig.CoreRunMode)
	appConfig.Store(tempConfig)

	if write {
		if err := writeAppConfig(appConfigPath); err != nil {
			log.Println("Failed to write app config:", err)
		}
	}

	log.Println("App config loaded:", appConfigPath)
	return nil
}

func getAppConfig() *AppConfig {
	return appConfig.Load().(*AppConfig)
}

func writeAppConfig(path string) error {
	out, err := yaml.Marshal(getAppConfig())
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func changeAppConfig(proxyMode string, runMode string) {
	config := getAppConfig()

	if proxyMode != "" {
		config.ProxyMode = proxyMode
	}

	if runMode != "" {
		config.CoreRunMode = runMode
	}

	// 配置持久化
	_ = writeAppConfig(appConfigPath)
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

		if err := loadAppConfig(false); err != nil {
			log.Println("Failed to reload app config:", err)
			return
		}

		config := getAppConfig()

		// 重载核心日志配置
		coreLogWriter.Switch(config.CoreLogEnabled)

		// 重新加载核心配置
		if err := loadCoreConfig(); err != nil {
			go messageBoxAlert(AppName, fmt.Sprint(err))
		} else {
			// 重启核心
			if !restartCore() {
				go messageBoxAlert(AppName, I.TranSys("msg.error.core.restart_failed", nil))
			}
		}

		// 更改托盘代理模式选项
		proxyModeCloseItem.Uncheck()
		proxyModeSystemItem.Uncheck()
		proxyModeTunItem.Uncheck()
		if config.ProxyMode == ProxyModeSystem {
			// 设置系统代理
			setCoreProxy()
			proxyModeSystemItem.Check()
		} else {
			// 关闭系统代理
			unsetProxy()
			if config.ProxyMode == ProxyModeTun {
				proxyModeTunItem.Check()
			} else if config.ProxyMode == ProxyModeClose {
				proxyModeCloseItem.Check()
			}
		}

		// 更改托盘运行模式选项
		runModeRuleItem.Uncheck()
		runModeGlobalItem.Uncheck()
		runModeDirectItem.Uncheck()
		switch config.CoreRunMode {
		case CoreRunModeRule:
			runModeRuleItem.Check()
			break
		case CoreRunModeGlobal:
			runModeGlobalItem.Check()
			break
		case CoreRunModeDirect:
			runModeDirectItem.Check()
			break
		}

		updateTrayTitle(config)
	})

	appConfigViper.WatchConfig()
}
