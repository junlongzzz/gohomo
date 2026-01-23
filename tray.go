package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"

	"github.com/energye/systray"
	"golang.org/x/sys/windows"
)

//go:embed static/*
var staticFiles embed.FS // 嵌入静态文件

// 匹配该应用版本号正则
var versionRegex = regexp.MustCompile(`^\d{8}$`)

// 初始化系统托盘
func initSystray() {
	systray.Run(onReady, onExit)
}

func onReady() {
	sendNotification(I.TranSys("tray.start_message", nil))

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

	sysProxyItem := systray.AddMenuItemCheckbox(I.TranSys("tray.system_proxy", nil), "", getProxyEnable())
	sysProxyItem.Click(func() {
		go func() {
			if sysProxyItem.Checked() {
				if unsetProxy() {
					sysProxyItem.Uncheck()
				}
			} else {
				if setCoreProxy() {
					sysProxyItem.Check()
				}
			}
		}()
	})

	restartCoreItem := systray.AddMenuItem(I.TranSys("tray.restart_core", nil), "")
	restartCoreItem.Click(func() {
		go func() {
			// 重新加载核心配置
			if err := loadCoreConfig(); err != nil {
				go messageBoxAlert(AppName, fmt.Sprint(err))
				return
			}
			if restartCore() {
				if sysProxyItem != nil && sysProxyItem.Checked() {
					// 重新设置代理
					setCoreProxy()
				}
			} else {
				unsetProxy()
				go messageBoxAlert(AppName, I.TranSys("msg.error.core.restart_failed", nil))
			}
		}()
	})

	systray.AddMenuItem(I.TranSys("tray.edit_config", nil), "").Click(func() {
		// 打开配置文件
		_ = openBrowser(coreConfigPath)
	})

	dashboardItem := systray.AddMenuItem(I.TranSys("tray.core_dashboard.title", nil), "")
	dashboardItem.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.local_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().ExternalUiAddr)
	})
	dashboardItem.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.official_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().OfficialUiAddr)
	})
	dashboardItem.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.yacd_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().YACDUiAddr)
	})
	dashboardItem.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.zash_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().ZashBoardUiAddr)
	})

	// 分割线
	systray.AddSeparator()

	openItem := systray.AddMenuItem(I.TranSys("tray.open.title", nil), "")
	// 打开本地工作目录
	openItem.AddSubMenuItem(I.TranSys("tray.open.options.work_dir", nil), "").Click(func() {
		_ = openDirectory(workDir)
	})

	var openShellFn = func(shell string) {
		cmd := exec.Command(shell)
		cmd.Dir = workDir
		// 设置代理环境变量
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("HTTP_PROXY=http://%s", getProxyServer()),
			fmt.Sprintf("HTTPS_PROXY=http://%s", getProxyServer()))
		cmd.SysProcAttr = &windows.SysProcAttr{
			CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP,
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			go messageBoxAlert(AppName, fmt.Sprintf("Failed to start %s: %v", shell, err))
		}
	}
	// 打开powershell
	openItem.AddSubMenuItem(I.TranSys("tray.open.options.powershell", nil), "").Click(func() {
		ps := "pwsh.exe"
		// 先判断 pwsh.exe 是否在环境变量内存在
		if _, err := exec.LookPath(ps); err != nil {
			// 不存在使用系统默认的 PowerShell
			ps = "powershell.exe"
		}
		openShellFn(ps)
	})
	// 打开命令行
	openItem.AddSubMenuItem(I.TranSys("tray.open.options.cmd", nil), "").Click(func() {
		openShellFn("cmd.exe")
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem(I.TranSys("tray.app_config", nil), "").Click(func() {
		// 打开配置文件
		_ = openBrowser(appConfigPath)
	})

	systray.AddMenuItem(I.TranSys("tray.check_update", nil), "").Click(func() {
		go func() {
			resp, err := http.Get(fmt.Sprintf("%s/releases/latest/download/version.txt", AppGitHubRepo))
			if err != nil {
				go messageBoxAlert(AppName, fmt.Sprintf("Failed to check update: %v", err))
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				go messageBoxAlert(AppName, fmt.Sprintf("Failed to read response: %v", err))
				return
			}
			latestVersion := string(body)
			if latestVersion != "" && versionRegex.MatchString(latestVersion) && latestVersion != version {
				go func() {
					if messageBoxConfirm(AppName, I.TranSys("msg.info.update_available", map[string]any{"Version": latestVersion})) {
						downloadUrl := fmt.Sprintf("%s/releases/download/%s/gohomo-%s-%s-%s.zip", AppGitHubRepo,
							latestVersion, runtime.GOOS, runtime.GOARCH, latestVersion)
						log.Println("Update package download url:", downloadUrl)
						_ = openBrowser(downloadUrl)
					}
				}()
			} else {
				go messageBoxAlert(AppName, I.TranSys("msg.info.no_update", nil))
			}
		}()
	})

	systray.AddMenuItem(I.TranSys("tray.about", nil), "").Click(func() {
		about := I.TranSys("msg.info.about", map[string]any{
			"Name":        AppName,
			"Description": "Wrapper for Mihomo written in Golang.",
			"Version":     version,
			"BuildHash":   build,
			"GoVersion":   runtime.Version(),
			"WorkDir":     workDir,
			"LogDir":      logDir,
			"ConfigPath":  coreConfigPath,
			"CoreDir":     coreDir,
			"CorePath":    corePath,
			"CoreVersion": getCoreVersion(),
		})
		go messageBoxAlert(AppName, about)
	})

	exitItem := systray.AddMenuItem(I.TranSys("tray.exit", nil), "")
	exitItem.Click(func() { systray.Quit() })

	// 托盘点击事件处理函数
	var clickFn = func(menu systray.IMenu) {
		if menu != nil {
			_ = menu.ShowMenu()

			go func() {
				coreItem.SetTitle(fmt.Sprintf("%s %s", CoreShowName, getCoreVersion()))
				// 判断是否展示外部控制面板菜单项
				if getCoreConfig().ExternalUiAddr == "" {
					dashboardItem.Hide()
				} else {
					dashboardItem.Show()
				}
			}()
		}
	}
	// 左键点击托盘时显示菜单
	systray.SetOnClick(clickFn)
	// 右键点击托盘
	systray.SetOnRClick(clickFn)
}

func onExit() {
	// 退出程序后的处理操作
	unsetProxy()
	stopCore()
	os.Exit(0)
}
