package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-toast/toast"
	"golang.org/x/sys/windows"
)

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	shCore   = windows.NewLazySystemDLL("SHCore.dll")

	attachConsole              = kernel32.NewProc("AttachConsole")
	setConsoleCtrlHandler      = kernel32.NewProc("SetConsoleCtrlHandler")
	generateConsoleCtrlEvent   = kernel32.NewProc("GenerateConsoleCtrlEvent")
	procSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwareness = shCore.NewProc("SetProcessDpiAwareness")
)

// 使用 tasklist 命令检查进程是否正在运行
func isProcessRunning(processName string) bool {
	if processName == "" {
		return false
	}
	cmd := execCommand("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error check running:", string(output), err)
		return false
	}
	return strings.Contains(string(output), processName)
}

// 根据 pid 检查进程是否正在运行
func isProcessRunningByPid(pid int) bool {
	if pid == 0 {
		return false
	}
	cmd := execCommand("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error check running:", string(output), err)
		return false
	}
	return strings.Contains(string(output), fmt.Sprintf("%d", pid))
}

// 使用 taskkill 命令杀进程，支持 * 号模糊匹配
func killProcess(processName string) error {
	if processName == "" {
		return fmt.Errorf("process name is empty")
	}
	cmd := execCommand("taskkill", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName), "/F")
	return cmd.Run()
}

// 优雅的退出进程
// https://github.com/GUI-for-Cores/GUI.for.Clash/blob/main/bridge/exec_windows.go#L21
func killProcessGracefully(processName string) error {
	process, err := findProcess(processName)
	if err != nil {
		return err
	}
	// 尝试附加到控制台
	call, _, err := attachConsole.Call(uintptr(process.Pid))
	if call == 0 && !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return err
	}
	// 尝试设置控制台处理程序
	call, _, err = setConsoleCtrlHandler.Call(0, 1)
	if call == 0 {
		return err
	}
	// 发送 CTRL_BREAK_EVENT 信号，因为 windows 不支持信号
	call, _, err = generateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(process.Pid))
	if call == 0 {
		return err
	}
	// 等待进程退出
	_, _ = process.Wait()
	return nil
}

// 根据 pid 杀进程
func killProcessByPid(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("pid is invalid")
	}
	cmd := execCommand("taskkill", "/PID", fmt.Sprintf("%d", pid), "/F")
	return cmd.Run()
}

// 根据进程名称查找进程ID
func findProcessId(processName string) int {
	cmd := execCommand("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.ToLower(fields[0]) == strings.ToLower(processName) {
			pid, err := strconv.Atoi(fields[1])
			if err == nil {
				return pid
			}
		}
	}
	return 0
}

// 根据名称查找进程
func findProcess(processName string) (*os.Process, error) {
	processId := findProcessId(processName)
	if processId == 0 {
		return nil, fmt.Errorf("process %s not found", processName)
	}
	return os.FindProcess(processId)
}

// 判断文件是否存在
func isFileExist(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

// 使用默认程序打开指定地址/文件/文件夹/程序等
func openBrowser(uri string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// 处理 uri 特殊字符
		uri = strings.ReplaceAll(uri, "&", "^&")
		cmd = exec.Command("cmd", "/c", "start", "", uri)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	case "darwin":
		cmd = exec.Command("open", uri)
	case "linux":
		cmd = exec.Command("xdg-open", uri)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

// 打开目录浏览
func openDirectory(dir string) error {
	var cmd *exec.Cmd

	// 判断当前系统类型
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir) // Windows
	case "darwin":
		cmd = exec.Command("open", dir) // macOS
	case "linux":
		cmd = exec.Command("xdg-open", dir) // Linux (需要安装 xdg-utils)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

// 返回值对应不同的按钮，flags表示展示MB_xx哪些操作按钮
// 展示的时候会阻塞当前线程，直到用户点击按钮
func messageBox(title, content string, flags uint32) int {
	captionPtr, _ := windows.UTF16PtrFromString(title)
	textPtr, _ := windows.UTF16PtrFromString(content)
	ret, _ := windows.MessageBox(0, textPtr, captionPtr, flags)
	return int(ret)
}

// 显示带确认按钮的消息框
func messageBoxAlert(title, content string) int {
	return messageBox(title, content, windows.MB_OK|windows.MB_ICONINFORMATION)
}

// 显示带确认和取消按钮的消息框
// 返回值为true表示用户点击了确认按钮，否则为取消按钮
func messageBoxConfirm(title, content string) bool {
	return messageBox(title, content, windows.MB_OKCANCEL|windows.MB_ICONQUESTION) == 1
}

// 启用高 DPI 感知
func setDPIAware() {
	if procSetProcessDpiAwareness.Find() == nil {
		// 在 Windows 10 及以上系统启用高 DPI 感知
		// 2 表示 DPI_AWARENESS_PER_MONITOR_AWARE，适应不同显示器的 DPI
		ret, _, err := procSetProcessDpiAwareness.Call(uintptr(2))
		if ret != 0 {
			log.Println("SetProcessDpiAwareness call failed:", err)
		}
	} else {
		// 在 Windows 8.1 及以下系统启用高 DPI 感知
		ret, _, err := procSetProcessDPIAware.Call()
		if ret == 0 {
			log.Println("SetProcessDPIAware call failed:", err)
		}
	}
}

// 创建控制台命令，不显示窗口
func execCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		// 设置控制台字符集和新进程组
		CreationFlags: windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP,
		// 隐藏窗口
		HideWindow: true,
	}
	return cmd
}

// 发送通知
func sendNotification(message string) {
	notification := toast.Notification{
		AppID:   AppName,
		Message: message,
	}
	if err := notification.Push(); err != nil {
		log.Printf("Failed to send notification: %v\n", err)
	}
}
