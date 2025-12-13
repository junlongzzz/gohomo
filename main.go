package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/junlongzzz/gohomo/i18n"
)

const (
	// AppName 程序名称
	AppName = "Gohomo"
	// CoreShowName 核心名称
	CoreShowName = "Mihomo"
	// AppGitHubRepo 程序GitHub仓库
	AppGitHubRepo = "https://github.com/junlongzzz/gohomo"
)

var (
	build   string // 编译时的git提交哈希
	version string // 程序版本
	workDir string // 工作目录
	logDir  string // 日志目录

	I *i18n.I18n // i18n
)

func main() {
	// 初始化i18n
	I = i18n.New()
	if err := I.Init(); err != nil {
		fatal("Failed to init i18n:", err)
	}

	// 检查是否为单实例
	checkSingleInstance()
	// 获取当前程序的执行所在目录
	executable, err := os.Executable()
	if err != nil {
		fatal("Failed to get executable path:", err)
	}
	workDir = filepath.Dir(executable)

	logDir = filepath.Join(workDir, "logs")
	if !isFileExist(logDir) {
		// 日志目录不存在则自动创建
		if err := os.Mkdir(logDir, 0755); err != nil {
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

	log.Println(fmt.Sprintf("Version: %s (%s)", version, build))
	log.Println("Work directory:", workDir)

	// 初始化核心
	initCore()
	// 系统托盘
	initSystray()
}

// 发生错误退出程序时的提示，避免无法看到错误消息
func fatal(v ...any) {
	log.Println(v...)
	messageBoxAlert(AppName, fmt.Sprintln(v...))
	// 退出程序
	//os.Exit(0)
	onExit()
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
				fatal(I.TranSys("msg.error.already_running", nil))
			}
		}
	}

	// 保存当前进程的pid到文件
	err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		fatal(I.TranSys("msg.error.write_pid_file", map[string]any{"Error": err}))
	}
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
