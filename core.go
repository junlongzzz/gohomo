package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cast"
	"go.yaml.in/yaml/v3"
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
	ApiEnabled      bool   // 是否启用外部控制api
	ExternalUiAddr  string // 外部ui地址
	OfficialUiAddr  string // 官方ui地址
	YACDUiAddr      string // Yet Another Clash Dashboard ui地址
	ZashBoardUiAddr string // zashboard ui地址
	HttpProxyPort   int    // http代理端口
}

// coreVersionCache 核心版本缓存。
// 用 mtime+size 双重比对探测核心文件是否被替换；自带 mu 防止并发场景下重复 fork。
type coreVersionCache struct {
	mu      sync.Mutex
	Version string
	MTime   time.Time
	Size    int64
}

var (
	coreDir           string // core工作目录
	coreName          string // core程序名称
	corePath          string // core程序路径
	coreConfigPath    string // core配置文件路径
	coreRunConfigPath string // core实际运行配置文件路径

	coreConfig atomic.Pointer[CoreConfig] // core配置信息

	coreMutex     sync.Mutex    // 互斥锁，保护核心进程操作 + 配置读取
	coreLogWriter *SwitchWriter // 日志输出

	coreActivePid atomic.Int32     // 当前活动的核心进程 PID，0 表示未运行
	coreVersion   coreVersionCache // 核心版本缓存（含自身互斥锁）

	proxyEnvVars = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} // 代理环境变量
)

// 初始化core
func initCore() {
	coreDir = filepath.Join(workDir, "core")
	if !isFileExist(coreDir) {
		// core目录不存在则自动创建
		if err := os.MkdirAll(coreDir, 0755); err != nil {
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
			return filepath.SkipAll
		}
		return nil
	})
	if corePath == "" {
		if messageBoxConfirm(AppName, I.TranSys("msg.error.core.confirm_download", nil)) {
			_ = shellOpen(fmt.Sprintf("%s/releases/latest", CoreGitHubRepo))
			// 用户主动去下载核心：核心尚未启动、系统代理也未设置，只需释放单实例锁再退出
			releaseSingleInstanceLock()
			os.Exit(0)
		}
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
		// 没有核心配置时，创建一个最小化的初始配置
		log.Println("Can't find core config, create a initial config file")
		coreConfigPath = configSearchPaths[0]
		if err := os.WriteFile(coreConfigPath, []byte(`mixed-port: 7890`), 0644); err != nil {
			// 创建初始配置失败时直接抛错提示
			fatal(I.TranSys("msg.error.core.config.not_found", map[string]any{
				"Dir1": workDir,
				"Dir2": coreDir,
			}))
		}
	}

	// 初始化配置对象
	coreConfig.Store(&CoreConfig{})

	// 加载核心配置
	if err := loadCoreConfig(); err != nil {
		fatal(err)
	}

	// 初始化日志输出
	coreLogWriter = NewSwitchWriter(log.Writer(), getAppConfig().CoreLogEnabled)

	if startCore() {
		if getAppConfig().ProxyMode == ProxyModeSystem {
			// 设置系统代理
			setCoreProxy()
		}
	} else {
		fatal(I.TranSys("msg.error.core.start_failed", nil))
	}
}

// 加载配置文件
func loadCoreConfig() error {
	coreMutex.Lock()
	defer coreMutex.Unlock()

	// 直接读取原始配置文件并解析为 map，保留 key 的大小写。
	// 不走 viper.AllSettings()：viper 大小写不敏感会把 proxy-providers / rule-providers 等
	// 自定义名称小写化，而 proxy-groups 的 use、rules 里的引用是原样字符串，会导致 provider 引用断链。
	raw, err := os.ReadFile(coreConfigPath)
	if err != nil {
		return errors.New(I.TranSys("msg.error.core.config.read_failed", map[string]any{"Error": err}))
	}
	baseConfig := make(map[string]any)
	if err = yaml.Unmarshal(raw, &baseConfig); err != nil {
		return errors.New(I.TranSys("msg.error.core.config.read_failed", map[string]any{"Error": err}))
	}

	// 开始合并应用配置的覆写配置
	tempAppConfig := getAppConfig()
	mergedConfig := deepMerge(baseConfig, tempAppConfig.CoreOverride)
	// 覆写运行模式
	mergedConfig["mode"] = tempAppConfig.CoreRunMode
	// 覆写tun配置
	tun, ok := mergedConfig["tun"].(map[string]any)
	if !ok {
		// 不存在 or 类型不对 → 新建
		tun = make(map[string]any)
	}
	// 设置是否启用tun
	tunEnabled := tempAppConfig.ProxyMode == ProxyModeTun
	tun["enable"] = tunEnabled
	mergedConfig["tun"] = tun

	if tunEnabled && !isRunAsAdmin() {
		// 启用tun时判断是否有管理员权限，否则核心无法创建tun网卡
		return errors.New(I.TranSys("msg.error.core.config.tun_without_admin", nil))
	}

	// 读取配置到临时配置对象
	tempConfig := &CoreConfig{}

	if mixedPort := cast.ToInt(mergedConfig["mixed-port"]); mixedPort != 0 {
		tempConfig.MixedPort = mixedPort
		tempConfig.HttpProxyPort = mixedPort
	} else if port := cast.ToInt(mergedConfig["port"]); port != 0 {
		tempConfig.Port = port
		tempConfig.HttpProxyPort = port
	}
	// TUN 模式不依赖 http/mixed 代理端口；仅在需要 http 代理（system/close）时缺端口才视为错误
	if tempConfig.HttpProxyPort == 0 && !tunEnabled {
		return errors.New(I.TranSys("msg.error.core.config.missing_port", nil))
	}

	tempConfig.ExternalController = cast.ToString(mergedConfig["external-controller"])
	tempConfig.Secret = cast.ToString(mergedConfig["secret"])
	tempConfig.ExternalUi = cast.ToString(mergedConfig["external-ui"])
	tempConfig.ExternalUiName = cast.ToString(mergedConfig["external-ui-name"])

	if host, port, err := net.SplitHostPort(tempConfig.ExternalController); err == nil {
		// 需要配置了外部控制器API时才能使用控制面板
		tempConfig.ApiEnabled = true

		if host == "" || host == "0.0.0.0" || host == "::" {
			// 形如 :9090 的格式，监听的是所有地址，管理面板就默认使用本地地址
			host = "127.0.0.1"
		}

		// secret 可能含 &/#/+ 等 query 关键字符，必须编码
		escapedSecret := url.QueryEscape(tempConfig.Secret)
		escapedHost := url.QueryEscape(host)
		escapedPort := url.QueryEscape(port)

		if tempConfig.ExternalUi != "" {
			// 配置了本地外部用户UI
			uiUrlPath := "/ui"
			if tempConfig.ExternalUiName != "" {
				// 去除开头/末尾的斜杠
				uiUrlPath += "/" + strings.Trim(tempConfig.ExternalUiName, "/")
			}
			// 本地面板地址
			tempConfig.ExternalUiAddr = fmt.Sprintf("http://%s%s/#/setup?http=true&hostname=%s&port=%s&secret=%s",
				net.JoinHostPort(host, port), uiUrlPath, escapedHost, escapedPort, escapedSecret)
		}

		// 官方面板地址
		tempConfig.OfficialUiAddr = fmt.Sprintf("https://metacubex.github.io/metacubexd/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			escapedHost, escapedPort, escapedSecret)
		// Yet Another Clash Dashboard
		tempConfig.YACDUiAddr = fmt.Sprintf("https://yacd.metacubex.one/?hostname=%s&port=%s&secret=%s",
			escapedHost, escapedPort, escapedSecret)
		// zashboard
		tempConfig.ZashBoardUiAddr = fmt.Sprintf("https://board.zash.run.place/#/setup?http=true&hostname=%s&port=%s&secret=%s",
			escapedHost, escapedPort, escapedSecret)
	}

	// 保存到运行配置文件
	if err := func() error {
		out, err := yaml.Marshal(mergedConfig)
		if err != nil {
			return err
		}
		// 头部声明此文件是程序生成产物，避免用户误编辑后下次 reload 被静默覆盖
		header := fmt.Sprintf("# AUTO-GENERATED — DO NOT EDIT\n# Generated from: %s\n# Manual edits will be silently overwritten on next reload.\n\n", coreConfigPath)
		return os.WriteFile(coreRunConfigPath, append([]byte(header), out...), 0644)
	}(); err != nil {
		return errors.New(I.TranSys("msg.error.core.config.write_running_failed", map[string]any{"Error": err}))
	}

	// 配置解析校验成功，临时配置提交给正式配置
	coreConfig.Store(tempConfig)
	log.Println("Core config loaded:", coreConfigPath)
	return nil
}

// startCore 启动核心进程
func startCore() bool {
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

	pid := int32(cmd.Process.Pid)
	coreActivePid.Store(pid)

	// 同时异步等待进程退出，在退出时清空 PID，避免后续操作误用已退出的 PID
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		coreActivePid.CompareAndSwap(pid, 0)
		exited <- err
	}()
	select {
	case err := <-exited:
		log.Println("Core exited during startup:", err)
		return false
	case <-time.After(500 * time.Millisecond):
		log.Println("Core started, pid:", pid)
		return true
	}
}

// stopCore 停止核心进程
func stopCore() bool {
	pid := coreActivePid.Load()

	// 通过 PID 查找进程对象，验证是否为核心进程
	if p := findProcessByPid(pid); p != nil {
		if name, _ := p.Name(); !strings.EqualFold(name, coreName) {
			pid = 0 // PID 存活但不是核心进程，按名称兜底查找
		}
	} else {
		pid = 0 // PID 已失效，按名称兜底查找
	}

	// PID 无效或不匹配，按程序名称兜底查找
	if pid == 0 {
		pid = findProcessId(coreName)
	}
	if pid == 0 {
		log.Println("Core is not running")
		return true
	}

	// 结束进程
	if err := killProcessGracefully(pid); err != nil {
		log.Println("Failed to stop core gracefully:", err)
		return false
	}
	coreActivePid.Store(0)
	log.Println("Core stopped, pid:", pid)
	return true
}

// restartCore 重启核心进程
func restartCore() bool {
	coreMutex.Lock()
	defer coreMutex.Unlock()
	return stopCore() && startCore()
}

// 检查core程序是否正在运行：先走 PID 快路径，未命中按 exe 名兜底（处理核心自重启场景）
func isCoreRunning() bool {
	if pid := coreActivePid.Load(); pid > 0 && isPidAlive(pid) {
		return true
	}
	if found := findProcessId(coreName); found > 0 {
		coreActivePid.Store(found)
		return true
	}
	coreActivePid.Store(0)
	return false
}

func setProxyEnv(proxyUrl string) {
	for _, k := range proxyEnvVars {
		_ = os.Setenv(k, proxyUrl)
	}
}

func unsetProxyEnv() {
	for _, k := range proxyEnvVars {
		_ = os.Unsetenv(k)
	}
}

// 设置系统代理为core配置的代理
func setCoreProxy() bool {
	host := "127.0.0.1"
	port := fmt.Sprintf("%d", getCoreConfig().HttpProxyPort)
	set := setProxy(true, host, port, strings.Join(getAppConfig().ProxyByPass, ";"))
	if set {
		// host/port 已知，直接拼装代理环境变量，省去一次系统代理回查
		setProxyEnv(fmt.Sprintf("http://%s:%s", host, port))
	} else {
		unsetProxyEnv()
	}
	return set
}

// 取消core代理
func unsetCoreProxy() bool {
	unsetProxyEnv()
	return unsetProxy()
}

// getCoreVersion 返回核心版本号
// 文件未变直接返回缓存；第一次调用或文件被替换时同步 fork 核心 -v 重新探测（约百毫秒级阻塞）
func getCoreVersion() string {
	if corePath == "" {
		return ""
	}

	coreVersion.mu.Lock()
	defer coreVersion.mu.Unlock()

	info, err := os.Stat(corePath)
	if err != nil {
		// 取不到文件信息就返回上次的缓存
		return coreVersion.Version
	}
	mtime, size := info.ModTime(), info.Size()
	if coreVersion.MTime.Equal(mtime) && coreVersion.Size == size {
		return coreVersion.Version
	}

	// 文件变了或没缓存，同步探测
	var v string
	if output, err := execCommand(corePath, "-v").Output(); err == nil {
		fields := strings.Fields(string(output))
		if len(fields) >= 3 && strings.EqualFold(fields[0], CoreShowName) {
			v = fields[2]
		}
	}
	coreVersion.Version = v
	coreVersion.MTime = mtime
	coreVersion.Size = size
	return v
}

// 获取core配置信息
func getCoreConfig() *CoreConfig {
	return coreConfig.Load()
}
