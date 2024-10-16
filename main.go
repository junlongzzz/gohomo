package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/energye/systray"
	"golang.org/x/sys/windows"
)

const (
	// AppName 程序名称
	AppName = "Gohomo"
	// CoreShowName 核心名称
	CoreShowName = "Mihomo"
)

var (
	build   string // 编译时的git提交哈希
	workDir string // 工作目录
	logDir  string // 日志目录

	//go:embed static/*
	staticFiles embed.FS // 嵌入静态文件
)

// 发生错误退出程序时的提示，避免无法看到错误消息
func fatal(v ...any) {
	log.Println(v...)
	messageBoxAlert(AppName, fmt.Sprintln(v...))
	// 退出程序
	os.Exit(0)
}

// 删除清理指定过期时长的日志文件
func delOutdatedLogs(age time.Duration) {
	_ = filepath.WalkDir(logDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != logDir {
			// 跳过子目录
			return filepath.SkipDir
		}
		// 判断文件名是否以.log结尾
		if !strings.HasSuffix(info.Name(), ".log") {
			return nil
		}
		// 获取文件创建时间
		fileInfo, err := info.Info()
		if err != nil {
			return err
		}
		fileAge := time.Now().Sub(fileInfo.ModTime())
		if fileAge > age {
			// 删除过期文件
			_ = os.Remove(path)
		}
		return nil
	})
}

// 获取pid文件路径
func getPidFilePath() string {
	// 临时目录内
	return filepath.Join(os.TempDir(), "gohomo.pid")
}

// 检查是否为单实例
func checkSingleInstance() {
	pidFilePath := getPidFilePath()
	if isFileExist(pidFilePath) {
		bytes, _ := os.ReadFile(pidFilePath)
		if bytes != nil && len(bytes) > 0 {
			// 判断pid对应进程是否还在运行
			pid, err := strconv.Atoi(string(bytes))
			if err == nil && pid > 0 && isProcessRunningByPid(pid) {
				fatal("Another instance of Gohomo is running.")
			}
		}
	}

	// 保存当前进程的pid到文件
	err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		fatal("Failed to write pid file:", err)
	}
}

func onReady() {
	bytes, err := staticFiles.ReadFile("static/icon.ico")
	if err == nil {
		systray.SetIcon(bytes)
	}
	systray.SetTitle(AppName)
	systray.SetTooltip(AppName)

	systray.AddMenuItem(fmt.Sprintf("%s %s", AppName, build), AppName).Click(func() {
		// 点击打开主页
		_ = openBrowser("https://github.com/junlongzzz/gohomo")
	})

	// 分割线
	systray.AddSeparator()

	coreItem := systray.AddMenuItem(CoreShowName, CoreShowName)
	coreItem.Click(func() {
		// 点击打开主页
		_ = openBrowser("https://github.com/MetaCubeX/mihomo")
	})

	sysProxyItem := systray.AddMenuItemCheckbox("System Proxy", "Set or Unset", getProxyEnable())
	sysProxyItem.Click(func() {
		if sysProxyItem.Checked() {
			if unsetProxy() {
				sysProxyItem.Uncheck()
			}
		} else {
			if setCoreProxy() {
				sysProxyItem.Check()
			}
		}
	})

	restartCoreItem := systray.AddMenuItem("Restart Core", "Restart Core")
	restartCoreItem.Click(func() {
		// 重新加载核心配置
		if err := loadCoreConfig(); err != nil {
			messageBoxAlert(AppName, fmt.Sprint(err))
			return
		}
		if restartCore() {
			if sysProxyItem != nil && sysProxyItem.Checked() {
				// 重新设置代理
				setCoreProxy()
			}
		} else {
			messageBoxAlert(AppName, "Failed to restart core")
		}
	})

	dashboardItem := systray.AddMenuItem("Core Dashboard", "Core Dashboard")
	dashboardItem.AddSubMenuItem("External UI", "External UI").Click(func() {
		_ = openBrowser(coreConfig.ExternalUiAddr)
	})
	dashboardItem.AddSubMenuItem("Official UI", "Official UI").Click(func() {
		_ = openBrowser(coreConfig.OfficialUiAddr)
	})
	dashboardItem.AddSubMenuItem("YACD UI", "YACD UI").Click(func() {
		_ = openBrowser(coreConfig.YACDUiAddr)
	})

	// 分割线
	systray.AddSeparator()

	// 打开本地工作目录
	systray.AddMenuItem("Open Work Directory", "Open Work Directory").Click(func() {
		_ = openDirectory(workDir)
	})

	var openShellFn = func(shell string) {
		cmd := exec.Command(shell)
		cmd.Dir = workDir
		// 设置代理环境变量
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("HTTP_PROXY=http://127.0.0.1:%d", coreConfig.HttpProxyPort),
			fmt.Sprintf("HTTPS_PROXY=http://127.0.0.1:%d", coreConfig.HttpProxyPort))
		cmd.SysProcAttr = &windows.SysProcAttr{
			CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP,
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			messageBoxAlert(AppName, fmt.Sprintf("Failed to start %s: %v", shell, err))
		}
	}
	// 打开powershell
	systray.AddMenuItem("Open PowerShell", "Open PowerShell").Click(func() {
		ps := "pwsh.exe"
		// 先判断 pwsh.exe 是否在环境变量内存在
		if _, err := exec.LookPath(ps); err != nil {
			// 不存在使用系统默认的 PowerShell
			ps = "powershell.exe"
		}
		openShellFn(ps)
	})
	// 打开命令行
	systray.AddMenuItem("Open Command Prompt", "Open Command Prompt").Click(func() {
		openShellFn("cmd.exe")
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem("About", "About").Click(func() {
		about := fmt.Sprintf("Name: %s\n"+
			"Description: %s\n"+
			"Build Hash: %s\n"+
			"---\n"+
			"Work Directory: %s\n"+
			"Log Directory: %s\n"+
			"Core Directory: %s\n"+
			"Core Path: %s\n"+
			"Core Version: %s",
			AppName, "Wrapper for Mihomo written in Golang.", build, workDir, logDir, coreDir, corePath, getCoreVersion())
		messageBoxAlert(AppName, about)
	})

	exitItem := systray.AddMenuItem("Exit", "Exit")
	exitItem.Click(func() { systray.Quit() })

	// 托盘点击事件处理函数
	var clickFn = func(menu systray.IMenu) {
		if menu != nil {
			coreItem.SetTitle(fmt.Sprintf("%s %s", CoreShowName, getCoreVersion()))
			_ = menu.ShowMenu()
		}
	}
	// 左键点击托盘时显示菜单
	systray.SetOnClick(clickFn)
	// 右键点击托盘
	systray.SetOnRClick(clickFn)
}

func onExit() {
	// 退出程序后的处理操作
	// 清理pid文件，写入-1
	_ = os.WriteFile(getPidFilePath(), []byte("-1"), 0644)
	unsetProxy()
	stopCore()
	os.Exit(0)
}

func main() {
	// 设置高DPI感知，避免界面模糊
	setDPIAware()
	// 检查是否为单实例
	checkSingleInstance()

	// 获取当前程序的执行所在目录
	executable, err := os.Executable()
	if err != nil {
		fatal("Failed to get executable path:", err)
	}
	workDir = filepath.Dir(executable)
	coreDir = filepath.Join(workDir, "core")
	logDir = filepath.Join(workDir, "logs")

	if !isFileExist(coreDir) {
		// core目录不存在则自动创建
		err := os.Mkdir(coreDir, 0755)
		if err != nil {
			fatal("Failed to create core directory:", err)
		}
	}

	if !isFileExist(logDir) {
		// 日志目录不存在则自动创建
		err := os.Mkdir(logDir, 0755)
		if err != nil {
			fatal("Failed to create log directory:", err)
		}
	}

	// 删除7天前的日志文件
	go delOutdatedLogs(7 * 24 * time.Hour)
	// 使用当天日期作为日志文件名
	logFilePath := filepath.Join(logDir, fmt.Sprintf("%s.log", time.Now().Format("2006-01-02")))
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		fatal("Failed to open log file:", err)
	}
	defer logFile.Close()
	// 将日志输出重定向到文件
	log.SetOutput(logFile)

	defer func() {
		// 捕获panic
		if r := recover(); r != nil {
			panicMsg := fmt.Sprintf("Panic: %v", r)
			log.Println(panicMsg)
			log.Println("Stack trace:\n", string(debug.Stack()))
			messageBoxAlert(AppName, panicMsg)
		}
	}()

	log.Println("Build hash:", build)
	log.Println("Work directory:", workDir)

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
		fatal("No core found, please put it in", workDir)
	} else {
		// 获取core文件名
		coreName = filepath.Base(corePath)
	}

	// 加载核心配置
	if err := loadCoreConfig(); err != nil {
		fatal(err)
	}

	if startCore() {
		// 设置系统代理
		setCoreProxy()
	} else {
		fatal("Failed to start core:", corePath)
	}

	// 系统托盘
	systray.Run(onReady, onExit)
}
