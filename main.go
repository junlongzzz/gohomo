package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/energye/systray"
)

const (
	// AppName 程序名称
	AppName = "Gohomo"
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
	systray.SetTooltip(fmt.Sprintf("%s %s", AppName, build))

	// 左键点击托盘时显示菜单
	systray.SetOnClick(func(menu systray.IMenu) {
		if menu != nil {
			_ = menu.ShowMenu()
		}
	})

	systray.AddMenuItem(AppName, AppName).Click(func() {
		// 点击打开主页
		_ = openBrowser("https://github.com/junlongzzz/gohomo")
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem("Mihomo", "Mihomo").Click(func() {
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
		if restartCore() {
			// 重新加载核心配置
			loadCoreConfig()
			if sysProxyItem != nil && sysProxyItem.Checked() {
				// 重新设置代理
				setCoreProxy()
			}
		} else {
			messageBoxAlert(AppName, "Failed to restart core")
		}
	})

	if coreConfig.ExternalUiAddr != "" {
		dashboardItem := systray.AddMenuItem("Core Dashboard", "Core Dashboard")
		dashboardItem.AddSubMenuItem("External UI", "External UI").Click(func() {
			_ = openBrowser(coreConfig.ExternalUiAddr)
		})
		dashboardItem.AddSubMenuItem("Official UI", "Official UI").Click(func() {
			_ = openBrowser(coreConfig.OfficialUiAddr)
		})
	}

	// 分割线
	systray.AddSeparator()

	// 打开本地工作目录
	systray.AddMenuItem("Open Work Directory", "Open Work Directory").Click(func() {
		_ = openDirectory(workDir)
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
			"Core Path: %s",
			AppName, "Wrapper for Mihomo written in Golang.", build, workDir, logDir, coreDir, corePath)
		messageBoxAlert(AppName, about)
	})

	exitItem := systray.AddMenuItem("Exit", "Exit")
	exitItem.Click(func() { systray.Quit() })
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
		if strings.HasPrefix(strings.ToLower(info.Name()), "mihomo") && strings.HasSuffix(strings.ToLower(info.Name()), ".exe") {
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

	if !isFileExist(coreDir) {
		// core目录不存在则自动创建
		err := os.Mkdir(coreDir, 0755)
		if err != nil {
			fatal("Failed to create core directory:", err)
		}
	}

	// 加载核心配置
	loadCoreConfig()

	if startCore() {
		// 设置系统代理
		setCoreProxy()
	} else {
		fatal("Failed to start core", corePath)
	}

	// 系统托盘
	systray.Run(onReady, onExit)
}
