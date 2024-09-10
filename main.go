package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/energye/systray"
	"golang.org/x/sys/windows"
)

const (
	// 程序名称
	appName = "Gohomo"
	// pid文件名称
	pidFile = "gohomo.pid"
)

var (
	build string // 编译时的git提交哈希

	workDir string // 工作目录

	//go:embed static/*
	staticFiles embed.FS // 嵌入静态文件
)

func main() {
	// 设置高DPI感知，避免界面模糊
	SetDPIAware()

	checkSingleInstance()

	// 获取当前程序的执行所在目录
	executable, err := os.Executable()
	if err != nil {
		fatal("Failed to get executable path:", err)
	}
	workDir = filepath.Dir(executable)
	coreDir = filepath.Join(workDir, "core")
	log.Println("Working directory:", workDir)
	log.Println("Core directory:", coreDir)
	// 查找core目录下知否存在mihomo开头的可执行文件
	_ = filepath.Walk(coreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasPrefix(strings.ToLower(info.Name()), "mihomo") {
			// 判断是否是可执行的文件
			if (runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(info.Name()), ".exe")) ||
				info.Mode()&0111 != 0 { // linux&unix下的判断
				// 可执行
				coreName = info.Name()
				log.Println("Found core:", coreName)
				return fmt.Errorf("found core") // 找到文件后返回自定义错误退出遍历
			} else {
				log.Println("Found core:", info.Name(), "but it is not executable")
			}
		}
		return nil
	})
	if coreName == "" {
		fatal("No core found, please put it in", coreDir)
	}
	// 加载核心配置
	loadCoreConfig()

	if startCore() {
		// 设置系统代理
		setCoreProxy()
	}

	// 系统托盘
	systray.Run(onReady, onExit)
}

// 发生错误退出程序时的提示，避免无法看到错误消息
func fatal(v ...any) {
	MessageBox(appName, fmt.Sprintln(v...), windows.MB_OK)
	// 退出程序
	os.Exit(0)
}

// 检查是否为单实例
func checkSingleInstance() {
	pidFilePath := filepath.Join(os.TempDir(), pidFile)
	if isFileExist(pidFilePath) {
		bytes, err := os.ReadFile(pidFilePath)
		if err == nil {
			// 判断pid对应进程是否还在运行
			pid, err := strconv.Atoi(string(bytes))
			if err == nil && isProcessRunningByPid(pid) {
				fatal("Another instance of Gohomo is running.")
			}
		}
	}

	// 当前进程的pid
	pid := os.Getpid()
	// 保存pid文件
	err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(pid)), 0644)
	if err != nil {
		fatal("Failed to write pid file:", err)
	}
}

func onReady() {
	bytes, err := staticFiles.ReadFile("static/icon.ico")
	if err == nil {
		systray.SetIcon(bytes)
	}
	systray.SetTitle(appName)
	systray.SetTooltip(appName)

	// 左键点击托盘时显示菜单
	systray.SetOnClick(func(menu systray.IMenu) {
		if menu != nil {
			_ = menu.ShowMenu()
		}
	})

	systray.AddMenuItem(appName, appName).Click(func() {
		// 点击打开主页
		_ = openBrowser("https://github.com/junlongzzz/gohomo")
	})
	systray.AddMenuItem("Mihomo", "Mihomo").Click(func() {
		// 点击打开主页
		_ = openBrowser("https://github.com/MetaCubeX/mihomo")
	})

	// 分割线
	systray.AddSeparator()

	sysProxyItem := systray.AddMenuItemCheckbox("System proxy", "Set/Unset system proxy", getProxyEnable())
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

	restartCoreItem := systray.AddMenuItem("Restart core", "Restart core")
	restartCoreItem.Click(func() {
		if !restartCore() {
			MessageBox(appName, "Failed to restart core", windows.MB_OK)
		}
	})

	if coreConfig.ExternalUiAddr != "" {
		dashboardItem := systray.AddMenuItem("Core dashboard", "Core dashboard")
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
	systray.AddMenuItem("Open work directory", "Open work directory").Click(func() {
		_ = openDirectory(workDir)
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem("About", "About").Click(func() {
		about := fmt.Sprintf("Name: %s\nDescription: %s\nBuild hash: %s\n---\nWork directory: %s\nCore directory: %s\nCore name: %s",
			appName, "Wrapper for Mihomo written in Golang.", build, workDir, coreDir, coreName)
		MessageBox(appName, about, windows.MB_OK)
	})

	exitItem := systray.AddMenuItem("Exit", "Exit")
	exitItem.Click(func() { systray.Quit() })
}

func onExit() {
	// 退出程序后的处理操作
	unsetProxy()
	stopCore()
	os.Exit(0)
}
