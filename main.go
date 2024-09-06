package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/energye/systray"
	"github.com/energye/systray/icon"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/sys/windows"
	"gopkg.in/yaml.v3"
)

// 捕获中断信号
var (
	build string // 编译时的git提交哈希

	workDir  string // 工作目录
	servHost = "127.0.0.1"
	servPort = 18081

	// 嵌入静态文件
	//go:embed static/*
	staticFiles embed.FS
)

// server 启动HTTP服务器
func server(host string, port int) {
	log.Println("Gohomo running at", fmt.Sprintf("http://%s:%d", servHost, servPort))

	// 静态资源处理
	var fileServer http.Handler
	staticDir := filepath.Join(workDir, "static")
	if isFileExist(staticDir) {
		// 如果本地存在 static 目录，则优先使用该目录的静态资源
		fileServer = http.FileServer(http.Dir(staticDir))
		log.Println("Using local static files:", staticDir)
	} else {
		// 使用 sub 函数创建一个文件系统，指定嵌入文件的子目录 "static"
		staticSubDir, err := fs.Sub(staticFiles, "static")
		if err != nil {
			fatal("Failed to create sub file system:", err)
		}
		fileServer = http.FileServer(http.FS(staticSubDir))
	}

	// 定义路由
	router := chi.NewRouter()
	router.Use(middleware.Compress(5, "text/html", "text/plain", "application/json"))
	router.Use(middleware.CleanPath)

	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	// 静态资源路由，捕获所有未定义路由
	router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})

	router.Route("/api", func(r chi.Router) {
		// 所有 /api/xxx 子路由
		r.Get("/info", func(w http.ResponseWriter, r *http.Request) {
			success(w, map[string]any{
				"core_running":     isCoreRunning(),
				"core_port":        coreConfig.HttpProxyPort,
				"core_ui_addr":     coreConfig.ExternalUiAddr,
				"official_ui_addr": coreConfig.OfficialUiAddr,
				"proxy_enable":     getProxyEnable(),
				"proxy_server":     getProxyServer(),
			})
		})
		// 核心操作
		r.Post("/core", func(w http.ResponseWriter, r *http.Request) {
			responseIf(w, startCore(), "failed to start core", nil)
		})
		r.Delete("/core", func(w http.ResponseWriter, r *http.Request) {
			responseIf(w, stopCore(), "failed to stop core", nil)
		})
		r.Put("/core", func(w http.ResponseWriter, r *http.Request) {
			responseIf(w, restartCore(), "failed to restart core", nil)
		})
		// 系统代理操作
		r.Post("/proxy", func(w http.ResponseWriter, r *http.Request) {
			// 设置代理
			responseIf(w, setCoreProxy(), "failed to set proxy", nil)
		})
		r.Delete("/proxy", func(w http.ResponseWriter, r *http.Request) {
			// 删除代理
			responseIf(w, unsetProxy(), "failed to unset proxy", nil)
		})
	})

	go func() {
		// 后台启用服务器
		if err := http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), router); err != nil {
			log.Println("Gohomo server start error:", err)
		}
	}()
}

// 发生错误退出程序时的提示，避免无法看到错误消息
func fatal(v ...any) {
	MessageBox("Gohomo", fmt.Sprint(v...), windows.MB_OK)
	// 退出程序
	os.Exit(0)
}

func main() {
	//log.Println("Gohomo - Wrapper for Mihomo written in Golang.", fmt.Sprintf("(build %s)", build))

	//// 尝试绑定服务端口，来判断是否有其他实例在运行或者端口被占用
	//listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", servHost, servPort))
	//if err != nil {
	//	fatal(fmt.Sprintf("Gohomo is already running or port %d is in use", servPort))
	//} else {
	//	// 关闭端口，避免后续被占用无法监听
	//	_ = listener.Close()
	//}

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
		fatal("No core found, please put it in ", coreDir)
	}
	// 加载核心配置
	loadCoreConfig()

	//// 启动HTTP服务器
	//server(servHost, servPort)

	if startCore() {
		// 设置系统代理
		setCoreProxy()
	}

	// 系统托盘
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(icon.Data)
	systray.SetTitle("Gohomo")
	systray.SetTooltip("Gohomo - Wrapper for Mihomo written in Golang.")

	systray.AddMenuItem(fmt.Sprintf("build %s", build), "").Disable()

	systray.AddMenuItem("Run Info", "Show run information").Click(func() {
		out, _ := yaml.Marshal(map[string]any{
			"core_running":     isCoreRunning(),
			"core_port":        coreConfig.HttpProxyPort,
			"core_ui_addr":     coreConfig.ExternalUiAddr,
			"official_ui_addr": coreConfig.OfficialUiAddr,
			"proxy_enable":     getProxyEnable(),
			"proxy_server":     getProxyServer(),
		})
		MessageBox("Gohomo Run Information", string(out), windows.MB_OK)
	})

	sysProxyItem := systray.AddMenuItemCheckbox("System Proxy", "Set/Unset System Proxy", getProxyEnable())
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
	restartCoreItem := systray.AddMenuItem("Restart Core", "")
	restartCoreItem.Click(func() {
		restartCoreItem.Disable()
		restartCore()
		restartCoreItem.Enable()
	})
	// 分割线
	systray.AddSeparator()
	exitItem := systray.AddMenuItem("Exit", "Exit Gohomo")
	exitItem.Click(func() {
		systray.Quit()
	})
}

func onExit() {
	// 退出程序后的处理操作
	unsetProxy()
	stopCore()
	os.Exit(0)
}
