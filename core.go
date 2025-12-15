package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/viper"
)

// CoreConfig core配置信息
type CoreConfig struct {
	// 本程序需要的一些配置字段
	Port               int
	MixedPort          int
	ExternalController string
	Secret             string
	ExternalUi         string
	ExternalUiName     string

	// 额外自定义字段，不在yaml配置文件中
	ExternalUiAddr  string // 外部ui地址
	OfficialUiAddr  string // 官方ui地址
	YACDUiAddr      string // Yet Another Clash Dashboard ui地址
	ZashBoardUiAddr string // zashboard ui地址
	HttpProxyPort   int    // http代理端口
}

var (
	coreDir           string // core工作目录
	coreName          string // core程序名称
	corePath          string // core程序路径
	coreConfigPath    string // core配置文件路径
	coreRunConfigPath string // core实际运行配置文件路径

	coreConfig      atomic.Value // core配置信息 store *CoreConfig
	coreConfigViper *viper.Viper // core配置文件解析器

	coreMutex     sync.Mutex    // 互斥锁
	coreLogWriter *SwitchWriter // 日志输出
)

// 初始化core
func initCore() {
	coreDir = filepath.Join(workDir, "core")
	if !isFileExist(coreDir) {
		// core目录不存在则自动创建
		if err := os.Mkdir(coreDir, 0755); err != nil {
			fatal("Failed to create core directory:", err)
		}
	}

	// 查找工作目录下是否存在文件名以 mihomo 开头，以 .exe 结尾的文件
	_ = filepath.WalkDir(workDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != workDir {
			// 跳过子目录
			return filepath.SkipDir
		}
		name := strings.ToLower(info.Name())
		if strings.HasPrefix(name, strings.ToLower(CoreShowName)) && strings.HasSuffix(name, ".exe") {
			corePath = path
			log.Println("Found core:", corePath)
			return fmt.Errorf("found core") // 找到文件后返回自定义错误退出遍历
		}
		return nil
	})
	if corePath == "" {
		fatal(I.TranSys("msg.error.core.not_found", map[string]any{"Dir": workDir}))
	} else {
		// 获取core文件名
		coreName = filepath.Base(corePath)
	}

	// 运行配置文件路径
	coreRunConfigPath = filepath.Join(coreDir, "config.auto-gen")
	// 配置文件搜索路径
	var configSearchPaths = []string{
		filepath.Join(workDir, "config.yaml"),
		filepath.Join(workDir, "config.yml"),
		filepath.Join(coreDir, "config.yaml"),
		filepath.Join(coreDir, "config.yml"),
	}
	for _, path := range configSearchPaths {
		if isFileExist(path) {
			coreConfigPath = path
			break
		}
	}
	if !isFileExist(coreConfigPath) {
		fatal(I.TranSys("msg.error.core.config.not_found", map[string]any{
			"Dir1": workDir,
			"Dir2": coreDir,
		}))
	}

	// 初始化配置对象
	coreConfig.Store(&CoreConfig{})

	coreConfigViper = viper.New()
	coreConfigViper.SetConfigFile(coreConfigPath)
	// 加载核心配置
	if err := loadCoreConfig(); err != nil {
		fatal(err)
	}

	// 初始化日志输出
	coreLogWriter = NewSwitchWriter(log.Writer(), getAppConfig().CoreLogEnabled)

	if startCore() {
		// 设置系统代理
		setCoreProxy()
	} else {
		fatal(I.TranSys("msg.error.core.start_failed", nil))
	}
}

// 加载配置文件
func loadCoreConfig() error {
	if err := coreConfigViper.ReadInConfig(); err != nil {
		return fmt.Errorf(I.TranSys("msg.error.core.config.read_failed", map[string]any{"Error": err}))
	}

	// 读取配置到临时配置对象
	tempConfig := new(CoreConfig)

	if mixedPort := coreConfigViper.GetInt("mixed-port"); mixedPort != 0 {
		tempConfig.MixedPort = mixedPort
		tempConfig.HttpProxyPort = mixedPort
	} else if port := coreConfigViper.GetInt("port"); port != 0 {
		tempConfig.Port = port
		tempConfig.HttpProxyPort = port
	}
	if tempConfig.HttpProxyPort == 0 {
		return fmt.Errorf(I.TranSys("msg.error.core.config.missing_port", nil))
	}

	tempConfig.ExternalController = coreConfigViper.GetString("external-controller")
	tempConfig.Secret = coreConfigViper.GetString("secret")
	tempConfig.ExternalUi = coreConfigViper.GetString("external-ui")
	tempConfig.ExternalUiName = coreConfigViper.GetString("external-ui-name")

	if host, port, err := net.SplitHostPort(tempConfig.ExternalController); err == nil && tempConfig.ExternalUi != "" {
		// 需要配置了外部控制器API和外部用户UI时才能使用控制面板
		uiUrlPath := "/ui"
		if tempConfig.ExternalUiName != "" {
			// 去除开头/末尾的斜杠
			uiUrlPath += "/" + strings.Trim(tempConfig.ExternalUiName, "/")
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			// 形如 :9090 的格式，监听的是所有地址，管理面板就默认使用本地地址
			host = "127.0.0.1"
		}
		// 本地面板地址
		tempConfig.ExternalUiAddr = fmt.Sprintf("http://%s%s/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			net.JoinHostPort(host, port), uiUrlPath, host, port, tempConfig.Secret)
		// 官方面板地址
		tempConfig.OfficialUiAddr = fmt.Sprintf("https://metacubex.github.io/metacubexd/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			host, port, tempConfig.Secret)
		// Yet Another Clash Dashboard
		tempConfig.YACDUiAddr = fmt.Sprintf("https://yacd.metacubex.one/?hostname=%s&port=%s&secret=%s",
			host, port, tempConfig.Secret)
		// zashboard
		tempConfig.ZashBoardUiAddr = fmt.Sprintf("https://board.zash.run.place/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			host, port, tempConfig.Secret)
	}

	// 保存到运行配置文件
	if err := func() error {
		f, err := os.OpenFile(coreRunConfigPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
		defer f.Close()

		if err = coreConfigViper.WriteConfigTo(f); err != nil {
			return err
		}
		return f.Sync()
	}(); err != nil {
		return fmt.Errorf(I.TranSys("msg.error.core.config.write_running_failed", map[string]any{"Error": err}))
	}

	// 配置解析校验成功，临时配置提交给正式配置
	coreConfig.Store(tempConfig)
	log.Println("Core config loaded:", coreConfigPath)
	return nil
}

// 启动core程序
func startCore() bool {
	coreMutex.Lock()
	defer coreMutex.Unlock()

	if isCoreRunning() {
		log.Println("Core is already running")
		return true
	}

	// 启动core程序
	cmd := execCommand(corePath, "-d", coreDir, "-f", coreRunConfigPath)
	// 重定向输出到log
	cmd.Stdout = coreLogWriter
	cmd.Stderr = coreLogWriter
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
	coreMutex.Lock()
	defer coreMutex.Unlock()

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
	set := setProxy(true, "127.0.0.1", fmt.Sprintf("%d", getCoreConfig().HttpProxyPort), strings.Join(getAppConfig().ProxyByPass, ";"))
	if set {
		proxyUrl := fmt.Sprintf("http://%s", getProxyServer())
		// 设置环境变量
		_ = os.Setenv("HTTP_PROXY", proxyUrl)
		_ = os.Setenv("HTTPS_PROXY", proxyUrl)
	} else {
		// 恢复环境变量
		_ = os.Unsetenv("HTTP_PROXY")
		_ = os.Unsetenv("HTTPS_PROXY")
	}
	return set
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

// 获取core配置信息
func getCoreConfig() *CoreConfig {
	return coreConfig.Load().(*CoreConfig)
}
