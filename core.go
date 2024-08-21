package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	cmd *exec.Cmd

	coreDir    string
	coreName   string
	coreConfig CoreConfig
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

	if err := yaml.Unmarshal(bytes, &coreConfig); err != nil {
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
			if strings.HasPrefix(coreConfig.ExternalUiName, "/") {
				uiPath += coreConfig.ExternalUiName
			} else {
				uiPath += "/" + coreConfig.ExternalUiName
			}
			// 去除末尾的斜杠
			uiPath = strings.TrimSuffix(uiPath, "/")
		}
		controller := strings.Split(coreConfig.ExternalController, ":")
		if controller != nil && len(controller) == 2 {
			// 本地面板地址
			coreConfig.ExternalUiAddr = fmt.Sprintf("http://%s/%s/#/setup?hostname=%s&port=%s&secret=%s",
				coreConfig.ExternalController, uiPath,
				controller[0], controller[1], coreConfig.Secret)
			// 官方面板地址
			coreConfig.OfficialUiAddr = fmt.Sprintf("https://metacubex.github.io/metacubexd/#/setup?http=true&hostname=%s&port=%s&secret=%s",
				controller[0], controller[1], coreConfig.Secret)
		}
	}
}

// 启动core程序
func startCore() bool {
	if isCoreRunning() {
		log.Println("Core is already running")
		return true
	}

	// 启动core程序
	cmd = exec.Command(filepath.Join(coreDir, coreName), "-d", coreDir)
	// 获取标准输出管道
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Println("Error creating StdoutPipe:", err)
		return false
	}
	// 获取标准错误管道
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Println("Error creating StderrPipe:", err)
		return false
	}
	if err := cmd.Start(); err != nil {
		log.Println("Failed to start core:", err)
		return false
	}

	go func() {
		// 创建一个 io.MultiReader 来合并 stdout 和 stderr
		reader := io.MultiReader(stdout, stderr)
		// 逐行读取输出并打印到控制台
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Println("Error reading core output:", err)
		}
		// 等待命令执行完成
		//if err := cmd.Wait(); err != nil {
		//	log.Println("Core exited with error:", err)
		//}
	}()

	log.Println("Core started")
	return true
}

// 停止core程序
func stopCore() bool {
	if !isCoreRunning() {
		log.Println("Core is not running")
		return true
	}

	// 进程已经不存在或退出，通过进程名杀死
	if err := killProcess(coreName); err != nil {
		log.Println("Failed to kill core:", err)
		return false
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
