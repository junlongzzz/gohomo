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

// CoreConfig core配置信息
type CoreConfig struct {
	Port               int
	MixedPort          int
	ExternalController string
	Secret             string
	ExternalUi         string
	ExternalUiName     string

	// 额外自定义字段，不在yaml配置文件中
	ExternalUiAddr string // 外部ui地址
	OfficialUiAddr string // 官方ui地址
	YACDUiAddr     string // Yet Another Clash Dashboard ui地址
	HttpProxyPort  int    // http代理端口
	RunConfigPath  string // 运行配置文件路径
}

var (
	coreDir  string
	coreName string
	corePath string

	coreConfig = &CoreConfig{
		// 配置文件中不存在时需要赋予默认值的选项
		ExternalController: "127.0.0.1:9090",
		Secret:             "123456", // not secure!
		ExternalUi:         "ui",
	}

	mutex sync.Mutex // 互斥锁
)

// 加载配置文件
func loadCoreConfig() error {
	configPath := filepath.Join(coreDir, "config.yaml")
	if !isFileExist(configPath) {
		return fmt.Errorf("config file not found: %s", configPath)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// 解析yaml至map以获取到所有配置
	var configMap = map[string]any{}
	if err = yaml.Unmarshal(configBytes, &configMap); err != nil {
		return fmt.Errorf("failed to unmarshal config file: %v", err)
	}

	// 开始从map中获取配置项
	if configMap["port"] != nil {
		coreConfig.Port = configMap["port"].(int)
		coreConfig.HttpProxyPort = coreConfig.Port
	}

	if configMap["mixed-port"] != nil {
		coreConfig.MixedPort = configMap["mixed-port"].(int)
		coreConfig.HttpProxyPort = coreConfig.MixedPort
	}

	if configMap["external-controller"] != nil && configMap["external-controller"] != "" {
		coreConfig.ExternalController = configMap["external-controller"].(string)
	} else {
		// 赋默认值
		configMap["external-controller"] = coreConfig.ExternalController
	}

	if configMap["secret"] != nil && configMap["secret"] != "" {
		coreConfig.Secret = configMap["secret"].(string)
	} else {
		// 赋默认值
		configMap["secret"] = coreConfig.Secret
	}

	if configMap["external-ui"] != nil && configMap["external-ui"] != "" {
		coreConfig.ExternalUi = configMap["external-ui"].(string)
	} else {
		// 赋默认值
		configMap["external-ui"] = coreConfig.ExternalUi
	}

	if configMap["external-ui-name"] != nil {
		coreConfig.ExternalUiName = configMap["external-ui-name"].(string)
	}

	if coreConfig.HttpProxyPort == 0 {
		return fmt.Errorf("http proxy port not set")
	}

	uiUrlPath := "/ui"
	if coreConfig.ExternalUiName != "" {
		// 去除开头/末尾的斜杠
		uiUrlPath += "/" + strings.Trim(coreConfig.ExternalUiName, "/")
	}
	controller := strings.Split(coreConfig.ExternalController, ":")
	if controller != nil && len(controller) == 2 {
		if controller[0] == "" || controller[0] == "0.0.0.0" {
			// 形如 :9090 的格式，监听的是所有地址，管理面板就默认使用本地地址
			controller[0] = "127.0.0.1"
		}
		// 本地面板地址
		coreConfig.ExternalUiAddr = fmt.Sprintf("http://%s%s/#/setup?hostname=%s&port=%s&secret=%s",
			strings.Join(controller, ":"), uiUrlPath,
			controller[0], controller[1], coreConfig.Secret)
		// 官方面板地址
		coreConfig.OfficialUiAddr = fmt.Sprintf("https://metacubex.github.io/metacubexd/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			controller[0], controller[1], coreConfig.Secret)
		// Yet Another Clash Dashboard
		coreConfig.YACDUiAddr = fmt.Sprintf("https://yacd.metacubex.one/?hostname=%s&port=%s&secret=%s",
			controller[0], controller[1], coreConfig.Secret)
	}

	var out []byte
	if out, err = yaml.Marshal(&configMap); err != nil {
		return fmt.Errorf("failed to marshal config file: %v", err)
	}
	if coreConfig.RunConfigPath == "" {
		coreConfig.RunConfigPath = filepath.Join(coreDir, "config.auto-gen")
	}
	// 保存到运行配置文件
	if err = os.WriteFile(coreConfig.RunConfigPath, out, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
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
	cmd := execCommand(corePath, "-d", coreDir, "-f", coreConfig.RunConfigPath)
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

// 获取core版本号
func getCoreVersion() string {
	if output, err := execCommand(corePath, "-v").CombinedOutput(); err == nil {
		split := strings.Split(string(output), " ")
		if len(split) > 2 && split[0] == CoreShowName {
			return split[2]
		}
	}
	return ""
}
