package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	shCore                     = windows.NewLazySystemDLL("SHCore.dll")
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

// 根据 pid 杀进程
func killProcessByPid(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("pid is invalid")
	}
	cmd := execCommand("taskkill", "/PID", fmt.Sprintf("%d", pid), "/F")
	return cmd.Run()
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

// 浏览器打开指定地址
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return &os.PathError{Op: "open", Path: url, Err: os.ErrNotExist}
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

// 创建命令并设置 CREATE_NO_WINDOW
func execCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	return cmd
}
