package main

import (
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

type AppConfig struct {
	CoreLogEnabled bool     `yaml:"core-log-enabled" mapstructure:"core-log-enabled"` // 是否启用记录核心日志
	ProxyByPass    []string `yaml:"proxy-by-pass" mapstructure:"proxy-by-pass"`       // 代理白名单地址
}

const (
	// AppName 程序名称
	AppName = "Gohomo"
	// CoreShowName 核心名称
	CoreShowName = "Mihomo"
	// AppGitHubRepo 程序GitHub仓库
	AppGitHubRepo = "https://github.com/junlongzzz/gohomo"
)

var (
	appConfigPath  string       // 应用配置文件路径
	appConfig      atomic.Value // store *AppConfig
	appConfigViper *viper.Viper // 配置文件解析器
)

func initAppConfig() {
	// 初始化默认配置
	appConfig.Store(&AppConfig{
		CoreLogEnabled: false,
		ProxyByPass:    defaultBypassHosts,
	})

	appConfigPath = filepath.Join(workDir, "gohomo.yaml")
	if !isFileExist(appConfigPath) {
		// 不存在，创建默认初始化配置
		if err := writeDefaultAppConfig(appConfigPath); err != nil {
			log.Println("Failed to create app config:", err)
		}
	}

	appConfigViper = viper.New()
	appConfigViper.SetConfigFile(appConfigPath)
	if err := appConfigViper.ReadInConfig(); err != nil {
		log.Println("Failed to read app config:", err)
	} else if err = loadAppConfig(); err != nil {
		log.Println("Failed to load app config:", err)
	}
	watchAppConfig()
}

func loadAppConfig() error {
	tempConfig := new(AppConfig)
	if err := appConfigViper.Unmarshal(tempConfig); err != nil {
		return err
	}

	appConfig.Store(tempConfig)
	log.Println("App config loaded:", appConfigPath)
	return nil
}

func getAppConfig() *AppConfig {
	return appConfig.Load().(*AppConfig)
}

func writeDefaultAppConfig(path string) error {
	out, err := yaml.Marshal(getAppConfig())
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
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

		if err := loadAppConfig(); err != nil {
			log.Println("Failed to reload app config:", err)
			return
		}

		// 重载核心日志配置
		coreLogWriter.Switch(getAppConfig().CoreLogEnabled)

		// 重载代理配置
		if getProxyEnable() {
			setCoreProxy()
		}
	})

	appConfigViper.WatchConfig()
}
