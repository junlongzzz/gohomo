package main

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/energye/systray"
	"golang.org/x/sys/windows"
)

//go:embed static/*
var staticFiles embed.FS // 嵌入静态文件

// 初始化系统托盘
func initSystray() {
	systray.Run(onReady, onExit)
}

func onReady() {
	sendNotification("Gohomo is running...")

	bytes, err := staticFiles.ReadFile("static/icon.ico")
	if err == nil {
		systray.SetIcon(bytes)
	}
	systray.SetTitle(AppName)
	systray.SetTooltip(AppName)

	systray.AddMenuItem(fmt.Sprintf("%s %s", AppName, version), AppName).Click(func() {
		// 点击打开主页
		_ = openBrowser(AppGitHubRepo)
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
			sendNotification("Core restarted")
			if sysProxyItem != nil && sysProxyItem.Checked() {
				// 重新设置代理
				setCoreProxy()
			}
		} else {
			messageBoxAlert(AppName, "Failed to restart core")
		}
	})

	systray.AddMenuItem("Edit Config", "Edit Config").Click(func() {
		// 打开配置文件
		_ = openBrowser(coreConfigPath)
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

	openItem := systray.AddMenuItem("Open", "Open")
	// 打开本地工作目录
	openItem.AddSubMenuItem("Work Directory", "Open Work Directory").Click(func() {
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
	openItem.AddSubMenuItem("PowerShell", "Open PowerShell").Click(func() {
		ps := "pwsh.exe"
		// 先判断 pwsh.exe 是否在环境变量内存在
		if _, err := exec.LookPath(ps); err != nil {
			// 不存在使用系统默认的 PowerShell
			ps = "powershell.exe"
		}
		openShellFn(ps)
	})
	// 打开命令行
	openItem.AddSubMenuItem("Command Prompt", "Open Command Prompt").Click(func() {
		openShellFn("cmd.exe")
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem("Check Update", "Check Update").Click(func() {
		go func() {
			resp, err := http.Get(fmt.Sprintf("%s/releases/latest/download/version.txt", AppGitHubRepo))
			if err != nil {
				messageBoxAlert(AppName, fmt.Sprintf("Failed to check update: %v", err))
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				messageBoxAlert(AppName, fmt.Sprintf("Failed to read response: %v", err))
				return
			}
			latestVersion := string(body)
			if latestVersion != "" && latestVersion != version {
				if messageBoxConfirm(AppName, fmt.Sprintf("New version available: %s\nDo you want to download it?", latestVersion)) {
					_ = openBrowser(fmt.Sprintf("%s/releases/latest", AppGitHubRepo))
				}
			} else {
				messageBoxAlert(AppName, "You are using the latest version.")
			}
		}()
	})

	systray.AddMenuItem("About", "About").Click(func() {
		about := fmt.Sprintf(`Name: %s
Description: %s
Version: %s
Build Hash: %s
Go Version: %s
---
Work Directory: %s
Log Directory: %s
Core Directory: %s
Core Path: %s
Core Version: %s
Config Path: %s`,
			AppName, "Wrapper for Mihomo written in Golang.", version, build, runtime.Version(), workDir, logDir, coreDir, corePath, getCoreVersion(), coreConfigPath)
		messageBoxAlert(AppName, about)
	})

	exitItem := systray.AddMenuItem("Exit", "Exit")
	exitItem.Click(func() { systray.Quit() })

	// 托盘点击事件处理函数
	var clickFn = func(menu systray.IMenu) {
		if menu != nil {
			go func() {
				coreItem.SetTitle(fmt.Sprintf("%s %s", CoreShowName, getCoreVersion()))
			}()
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
