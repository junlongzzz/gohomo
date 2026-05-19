package main

import (
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

// TrayMenu 需要动态控制的系统托盘菜单项
type TrayMenu struct {
	Core *systray.MenuItem

	ProxyMode struct {
		Menu   *systray.MenuItem
		Close  *systray.MenuItem
		System *systray.MenuItem
		Tun    *systray.MenuItem
	}

	RunMode struct {
		Menu   *systray.MenuItem
		Rule   *systray.MenuItem
		Global *systray.MenuItem
		Direct *systray.MenuItem
	}

	RestartCore *systray.MenuItem

	Dashboard struct {
		Menu  *systray.MenuItem
		Local *systray.MenuItem
	}

	More struct {
		AutoStart   *systray.MenuItem
		CoreLog     *systray.MenuItem
		CheckUpdate *systray.MenuItem
	}
}

var (
	trayMenu     TrayMenu
	versionRegex = regexp.MustCompile(`^\d{8}$`) // 匹配该应用版本号正则
)

// 初始化系统托盘
func initSystray() {
	systray.Run(onReady, onExit)
}

func onReady() {
	sendNotification(I.TranSys("tray.start_message", nil))

	iconBytes, _ := appStaticFiles.ReadFile("static/icon.ico")
	if iconBytes != nil {
		systray.SetIcon(iconBytes)
	}
	systray.SetTitle(AppName)
	systray.SetTooltip(AppName)

	systray.AddMenuItem(fmt.Sprintf("%s %s", AppName, version), AppName).Click(func() {
		// 点击打开主页
		_ = openBrowser(AppGitHubRepo)
	})

	// 分割线
	systray.AddSeparator()

	trayMenu.Core = systray.AddMenuItem(CoreShowName, CoreShowName)
	trayMenu.Core.Click(func() {
		// 点击打开主页
		_ = openBrowser(CoreGitHubRepo)
	})

	trayMenu.ProxyMode.Menu = systray.AddMenuItem(I.TranSys("tray.proxy_mode.title", nil), "")
	trayMenu.ProxyMode.Close = trayMenu.ProxyMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.proxy_mode.options.close", nil), "", false)
	trayMenu.ProxyMode.System = trayMenu.ProxyMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.proxy_mode.options.system", nil), "", false)
	trayMenu.ProxyMode.Tun = trayMenu.ProxyMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.proxy_mode.options.tun", nil), "", false)
	trayMenu.ProxyMode.Close.Click(func() {
		go changeAppConfig(WithProxyMode(ProxyModeClose))
	})
	trayMenu.ProxyMode.System.Click(func() {
		go changeAppConfig(WithProxyMode(ProxyModeSystem))
	})
	trayMenu.ProxyMode.Tun.Click(func() {
		go changeAppConfig(WithProxyMode(ProxyModeTun))
	})

	trayMenu.RunMode.Menu = systray.AddMenuItem(I.TranSys("tray.run_mode.title", nil), "")
	trayMenu.RunMode.Rule = trayMenu.RunMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.run_mode.options.rule", nil), "", false)
	trayMenu.RunMode.Global = trayMenu.RunMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.run_mode.options.global", nil), "", false)
	trayMenu.RunMode.Direct = trayMenu.RunMode.Menu.AddSubMenuItemCheckbox(I.TranSys("tray.run_mode.options.direct", nil), "", false)
	trayMenu.RunMode.Rule.Click(func() {
		go changeAppConfig(WithCoreRunMode(CoreRunModeRule))
	})
	trayMenu.RunMode.Global.Click(func() {
		go changeAppConfig(WithCoreRunMode(CoreRunModeGlobal))
	})
	trayMenu.RunMode.Direct.Click(func() {
		go changeAppConfig(WithCoreRunMode(CoreRunModeDirect))
	})

	trayMenu.RestartCore = systray.AddMenuItem(I.TranSys("tray.restart_core", nil), "")
	trayMenu.RestartCore.Click(func() {
		go func() {
			trayMenu.RestartCore.Disable()
			defer trayMenu.RestartCore.Enable()
			// 重新加载核心配置
			if err := loadCoreConfig(); err != nil {
				go messageBoxAlert(AppName, fmt.Sprint(err))
				return
			}
			if restartCore() {
				if getAppConfig().ProxyMode == ProxyModeSystem {
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

	trayMenu.Dashboard.Menu = systray.AddMenuItem(I.TranSys("tray.core_dashboard.title", nil), "")
	trayMenu.Dashboard.Local = trayMenu.Dashboard.Menu.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.local_ui", nil), "")
	trayMenu.Dashboard.Local.Click(func() {
		_ = openBrowser(getCoreConfig().ExternalUiAddr)
	})
	trayMenu.Dashboard.Menu.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.official_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().OfficialUiAddr)
	})
	trayMenu.Dashboard.Menu.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.yacd_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().YACDUiAddr)
	})
	trayMenu.Dashboard.Menu.AddSubMenuItem(I.TranSys("tray.core_dashboard.options.zash_ui", nil), "").Click(func() {
		_ = openBrowser(getCoreConfig().ZashBoardUiAddr)
	})

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem(I.TranSys("tray.app_config", nil), "").Click(func() {
		// 打开配置文件
		_ = openBrowser(appConfigPath)
	})

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

	moreItem := systray.AddMenuItem(I.TranSys("tray.more.title", nil), "")

	trayMenu.More.AutoStart = moreItem.AddSubMenuItemCheckbox(I.TranSys("tray.more.options.auto_start", nil), "", false)
	trayMenu.More.AutoStart.Click(func() {
		go changeAppConfig(WithAutoStart(!trayMenu.More.AutoStart.Checked()))
	})

	trayMenu.More.CoreLog = moreItem.AddSubMenuItemCheckbox(I.TranSys("tray.more.options.core_log", nil), "", false)
	trayMenu.More.CoreLog.Click(func() {
		go changeAppConfig(WithCoreLogEnabled(!trayMenu.More.CoreLog.Checked()))
	})

	trayMenu.More.CheckUpdate = moreItem.AddSubMenuItem(I.TranSys("tray.more.options.check_update", nil), "")
	trayMenu.More.CheckUpdate.Click(func() {
		go func() {
			trayMenu.More.CheckUpdate.Disable()
			defer trayMenu.More.CheckUpdate.Enable()
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

	moreItem.AddSubMenuItem(I.TranSys("tray.more.options.about", nil), "").Click(func() {
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

	// 分割线
	systray.AddSeparator()

	systray.AddMenuItem(I.TranSys("tray.exit", nil), "").Click(func() { systray.Quit() })

	updateTrayMenu(getAppConfig())

	// 托盘点击事件处理函数
	var trayClickFn = func(menu systray.IMenu) {
		if menu == nil {
			return
		}

		// 展示核心版本
		trayMenu.Core.SetTitle(fmt.Sprintf("%s %s", CoreShowName, getCoreVersion()))

		var statusText string
		if isCoreRunning() {
			statusText = I.TranSys("tray.core_status.running", nil)
		} else {
			statusText = I.TranSys("tray.core_status.stopped", nil)
		}
		trayMenu.RestartCore.SetTitle(fmt.Sprintf(
			"%s [%s]",
			I.TranSys("tray.restart_core", nil),
			statusText,
		))

		// 判断是否展示外部控制面板菜单项
		tempConfig := getCoreConfig()
		if tempConfig.ApiEnabled {
			trayMenu.Dashboard.Menu.Show()
			// 判断是否展示本地控制面板菜单项
			if tempConfig.ExternalUiAddr != "" {
				trayMenu.Dashboard.Local.Show()
			} else {
				trayMenu.Dashboard.Local.Hide()
			}
		} else {
			trayMenu.Dashboard.Menu.Hide()
		}

		if err := menu.ShowMenu(); err != nil {
			log.Println("Failed to show menu:", err)
		}
	}
	// 左键点击托盘时显示菜单
	systray.SetOnClick(trayClickFn)
	// 右键点击托盘
	systray.SetOnRClick(trayClickFn)
}

func onExit() {
	// 退出程序后的处理操作
	unsetProxy()
	stopCore()
	os.Exit(0)
}

func updateTrayMenu(appConfig *AppConfig) {
	trayMenu.ProxyMode.Menu.SetTitle(fmt.Sprintf(
		"%s [%s]",
		I.TranSys("tray.proxy_mode.title", nil),
		I.TranSys(fmt.Sprintf("tray.proxy_mode.options.%s", appConfig.ProxyMode), nil),
	))

	trayMenu.RunMode.Menu.SetTitle(fmt.Sprintf(
		"%s [%s]",
		I.TranSys("tray.run_mode.title", nil),
		I.TranSys(fmt.Sprintf("tray.run_mode.options.%s", appConfig.CoreRunMode), nil),
	))

	// 更改托盘代理模式选项
	trayMenu.ProxyMode.Close.Uncheck()
	trayMenu.ProxyMode.System.Uncheck()
	trayMenu.ProxyMode.Tun.Uncheck()
	switch appConfig.ProxyMode {
	case ProxyModeClose:
		trayMenu.ProxyMode.Close.Check()
	case ProxyModeSystem:
		trayMenu.ProxyMode.System.Check()
	case ProxyModeTun:
		trayMenu.ProxyMode.Tun.Check()
	}

	// 更改托盘运行模式选项
	trayMenu.RunMode.Rule.Uncheck()
	trayMenu.RunMode.Global.Uncheck()
	trayMenu.RunMode.Direct.Uncheck()
	switch appConfig.CoreRunMode {
	case CoreRunModeRule:
		trayMenu.RunMode.Rule.Check()
	case CoreRunModeGlobal:
		trayMenu.RunMode.Global.Check()
	case CoreRunModeDirect:
		trayMenu.RunMode.Direct.Check()
	}

	if appConfig.AutoStart {
		trayMenu.More.AutoStart.Check()
	} else {
		trayMenu.More.AutoStart.Uncheck()
	}

	if appConfig.CoreLogEnabled {
		trayMenu.More.CoreLog.Check()
	} else {
		trayMenu.More.CoreLog.Uncheck()
	}
}
