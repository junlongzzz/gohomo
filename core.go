package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	coreDir    string
	coreName   string
	corePath   string
	coreConfig *CoreConfig

	mutex sync.Mutex // 互斥锁
)

// CoreConfig core配置信息
type CoreConfig struct {
	Mode               string `yaml:"mode"`
	MixedPort          int    `yaml:"mixed-port"`
	Port               int    `yaml:"port"`
	ExternalController string `yaml:"external-controller"`
	Secret             string `yaml:"secret"`
	ExternalUi         string `yaml:"external-ui"`
	ExternalUiName     string `yaml:"external-ui-name"`

	// 额外自定义字段，不在yaml配置文件中
	ExternalUiAddr string `yaml:"-"` // 外部ui地址
	OfficialUiAddr string `yaml:"-"` // 官方ui地址
	HttpProxyPort  int    `yaml:"-"` // http代理端口
}

// 加载配置文件
func loadCoreConfig() {
	configPath := filepath.Join(coreDir, "config.yaml")
	if !isFileExist(configPath) {
		fatal("config.yaml not found, please put it in", configPath)
	}

	bytes, err := os.ReadFile(configPath)
	if err != nil {
		fatal("Error reading config.yaml:", err)
	}

	coreConfig = &CoreConfig{}
	if err := yaml.Unmarshal(bytes, coreConfig); err != nil {
		fatal("Error parsing config.yaml:", err)
	}

	if coreConfig.MixedPort == 0 && coreConfig.Port == 0 {
		fatal("mixed-port and port cannot both be 0")
	}

	coreConfig.HttpProxyPort = coreConfig.MixedPort
	if coreConfig.HttpProxyPort == 0 {
		coreConfig.HttpProxyPort = coreConfig.Port
	}

	if coreConfig.ExternalController != "" {
		uiPath := "ui"
		if coreConfig.ExternalUiName != "" {
			// 去除开头/末尾的斜杠
			uiPath += "/" + strings.Trim(coreConfig.ExternalUiName, "/")
		}
		controller := strings.Split(coreConfig.ExternalController, ":")
		if controller != nil && len(controller) == 2 {
			if controller[0] == "" || controller[0] == "0.0.0.0" {
				// 形如 :9090 的格式，监听的是所有地址，管理面板就默认使用本地地址
				controller[0] = "127.0.0.1"
			}
			// 本地面板地址
			coreConfig.ExternalUiAddr = fmt.Sprintf("http://%s/%s/#/setup?hostname=%s&port=%s&secret=%s",
				strings.Join(controller, ":"), uiPath,
				controller[0], controller[1], coreConfig.Secret)
			// 官方面板地址
			coreConfig.OfficialUiAddr = fmt.Sprintf("https://metacubex.github.io/metacubexd/#/setup?http=true&hostname=%s&port=%s&secret=%s",
				controller[0], controller[1], coreConfig.Secret)
		}
	}
}

// 启动core程序
func startCore() bool {
	mutex.Lock()
	defer mutex.Unlock()

	if isCoreRunning() {
		log.Println("Core is already running")
		return true
	}

	// 启动core程序
	cmd := execCommand(corePath, "-d", coreDir)
	// 重定向输出到log
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	//cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		log.Println("Failed to start core:", err)
		return false
	}

	log.Println("Core started")
	return true
}

// 停止core程序
func stopCore() bool {
	mutex.Lock()
	defer mutex.Unlock()

	if !isCoreRunning() {
		log.Println("Core is not running")
		return true
	}

	// 结束进程
	if err := killProcessGracefully(coreName); err != nil {
		// 优雅停止失败，直接强制结束进程
		log.Println("Failed to stop core gracefully:", err)
		if err = killProcess(coreName); err != nil {
			log.Println("Failed to stop core:", err)
			return false
		}
	}

	log.Println("Core stopped")
	return true
}

// 重启core程序
func restartCore() bool {
	return stopCore() && startCore()
}

// 检查core程序是否正在运行
func isCoreRunning() bool {
	return isProcessRunning(coreName)
}

// 设置系统代理为core配置的代理
func setCoreProxy() bool {
	return setProxy(true, fmt.Sprintf("127.0.0.1:%d", coreConfig.HttpProxyPort), defaultBypass)
}
