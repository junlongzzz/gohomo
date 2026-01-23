package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/junlongzzz/gohomo/i18n"
	"golang.org/x/sys/windows"
)

var (
	build   string // 编译时的git提交哈希
	version string // 程序版本
	workDir string // 工作目录
	logDir  string // 日志目录

	I *i18n.I18n // i18n

	lockFileHandle windows.Handle // 锁文件句柄
)

func main() {
	// 初始化i18n
	I = i18n.New()
	if err := I.Init(); err != nil {
		fatal("Failed to init i18n:", err)
	}

	// 检查是否为单实例
	checkSingleInstance()
	defer windows.CloseHandle(lockFileHandle)

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
			fatal(panicMsg)
		}
	}()

	log.Println(fmt.Sprintf("Version: %s (%s)", version, build))
	log.Println("Work directory:", workDir)

	// 初始化应用配置
	initAppConfig()
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

// 检查是否为单实例
func checkSingleInstance() {
	lockPath := filepath.Join(os.TempDir(), "gohomo.pid")
	// 将路径转换为 UTF16
	pathPtr, _ := windows.UTF16PtrFromString(lockPath)

	// 尝试创建/打开文件
	// FILE_SHARE_READ: 允许别人读
	// 但不给 FILE_SHARE_WRITE 或 FILE_SHARE_DELETE，这样第二个进程尝试打开时就会失败
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // 0 表示不共享：第二个进程尝试打开会报 "Access is denied"
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)

	if err != nil {
		// 如果报错是“拒绝访问”或“文件被占用”，说明已有实例
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			fatal(I.TranSys("msg.error.already_running", nil))
		}
		return
	}

	// 成功获取句柄，将其存入全局变量防止被 GC 回收
	lockFileHandle = handle
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
