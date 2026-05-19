package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/gen2brain/beeep"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	RegKeyAutoStart = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
)

var (
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procFreeConsole              = modKernel32.NewProc("FreeConsole")
	procAttachConsole            = modKernel32.NewProc("AttachConsole")
	procSetConsoleCtrlHandler    = modKernel32.NewProc("SetConsoleCtrlHandler")
	procGenerateConsoleCtrlEvent = modKernel32.NewProc("GenerateConsoleCtrlEvent")
)

// 查找指定名称的所有进程
func findProcessesByName(processName string) ([]*process.Process, error) {
	if processName == "" {
		return nil, fmt.Errorf("process name is empty")
	}

	processes, err := process.Processes()
	if err != nil {
		return nil, err
	}

	var result []*process.Process
	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			continue
		}

		if strings.EqualFold(name, processName) {
			result = append(result, p)
		}
	}
	return result, nil
}

// 检查进程是否运行
func isProcessRunning(processName string) bool {
	processes, _ := process.Processes()
	for _, p := range processes {
		name, err := p.Name()
		if err == nil && strings.EqualFold(name, processName) {
			return true
		}
	}
	return false
}

// 检查进程是否运行（根据进程id）
func isProcessRunningByPid(pid int32) bool {
	if pid <= 0 {
		return false
	}

	exists, err := process.PidExists(pid)
	if err != nil {
		return false
	}
	return exists
}

// 根据进程名称查找进程id
func findProcessId(processName string) int32 {
	processes, _ := process.Processes()
	for _, p := range processes {
		name, err := p.Name()
		if err == nil && strings.EqualFold(name, processName) {
			return p.Pid
		}
	}
	return 0
}

// 根据进程名称查找进程
func findProcess(processName string) (*process.Process, error) {
	processes, _ := process.Processes()
	for _, p := range processes {
		name, err := p.Name()
		if err == nil && strings.EqualFold(name, processName) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("process %s not found", processName)
}

// 根据进程名称强制结束进程
func killProcess(processName string) error {
	processes, err := findProcessesByName(processName)
	if err != nil {
		return err
	}

	if len(processes) == 0 {
		// 进程本就不存在，视作成功
		return nil
	}

	var lastErr error
	for _, p := range processes {
		if err = p.Kill(); err != nil {
			lastErr = err
			log.Printf("force kill process %d failed: %v\n", p.Pid, err)
		}
	}
	return lastErr
}

// 根据进程id强制结束进程
func killProcessByPid(pid int32) error {
	if pid <= 0 {
		return fmt.Errorf("pid is invalid")
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// 尝试优雅的结束进程，如未成功则强制结束
func killProcessGracefully(processName string) error {
	pid := findProcessId(processName)
	if pid == 0 {
		// 进程不存在，视作成功
		return nil
	}

	p, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}

	if err = sendCtrlBreakEvent(pid); err != nil {
		log.Printf("send CTRL_BREAK to %d failed, force kill: %v\n", pid, err)
		return p.Kill() // 强制结束
	}

	// 等待进程退出，超时后强制结束
	timeout := time.After(5 * time.Second)
	done := make(chan error, 1)
	go func() {
		_, waitErr := p.Wait()
		done <- waitErr
	}()

	select {
	case <-timeout:
		log.Printf("graceful shutdown pid %d timeout, force kill\n", pid)
		return p.Kill() // 超时未退出，强制结束
	case waitErr := <-done:
		return waitErr
	}
}

// 参考：https://github.com/GUI-for-Cores/GUI.for.Clash/blob/main/bridge/exec_windows.go
func sendCtrlBreakEvent(pid int32) error {
	// 先脱离当前 console
	ret, _, err := procFreeConsole.Call()
	if ret == 0 && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return err
	}

	// 恢复父控制台
	defer procAttachConsole.Call(uintptr(^uint32(0)))

	// 尝试附加到控制台
	ret, _, err = procAttachConsole.Call(uintptr(pid))
	if ret == 0 && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}

	// 尝试设置控制台处理程序，让当前进程忽略 Ctrl 事件（防止误伤自己）
	ret, _, err = procSetConsoleCtrlHandler.Call(0, 1)
	if ret == 0 {
		return err
	}
	// 恢复当前进程对 Ctrl 事件的响应
	defer procSetConsoleCtrlHandler.Call(0, 0)

	// 发送 CTRL_BREAK_EVENT 事件，因为 windows 不支持信号
	ret, _, err = procGenerateConsoleCtrlEvent.Call(windows.CTRL_BREAK_EVENT, uintptr(pid))
	if ret == 0 {
		return err
	}

	return nil
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
	var icon any
	iconBytes, _ := appStaticFiles.ReadFile("static/icon.png")
	if iconBytes != nil {
		icon = iconBytes
	} else {
		icon = ""
	}
	if err := beeep.Notify(AppName, message, icon); err != nil {
		log.Printf("Failed to send notification: %v\n", err)
	}
}

// 递归合并两个 map
func deepMerge(target map[string]any, other map[string]any) map[string]any {
	for key, value := range other {

		switch v := value.(type) {

		// ===== 对象 =====
		case map[string]any:
			if strings.HasSuffix(key, "!") {
				// 后缀带 ! 的直接覆盖
				k := trimWrap(key[:len(key)-1])
				target[k] = v
				continue
			}

			k := trimWrap(key)

			child, ok := target[k].(map[string]any)
			if !ok {
				child = make(map[string]any)
				target[k] = child
			}

			deepMerge(child, v)

		// ===== 数组 =====
		case []any:
			switch {
			// +xxx 前插
			case strings.HasPrefix(key, "+"):
				k := trimWrap(key[1:])
				if existing, ok := target[k].([]any); ok {
					target[k] = append(v, existing...)
				}

			// xxx+ 后插
			case strings.HasSuffix(key, "+"):
				k := trimWrap(key[:len(key)-1])
				if existing, ok := target[k].([]any); ok {
					target[k] = append(existing, v...)
				}

			// xxx 覆盖
			default:
				k := trimWrap(key)
				target[k] = v
			}

		// ===== 普通值 =====
		default:
			k := trimWrap(key)
			target[k] = v
		}
	}

	return target
}

// 去掉字符串首尾的 <>
func trimWrap(str string) string {
	if strings.HasPrefix(str, "<") && strings.HasSuffix(str, ">") {
		return str[1 : len(str)-1]
	}
	return str
}

// 判断当前进程是否以管理员权限运行
func isRunAsAdmin() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	var elevated uint32
	var outLen uint32

	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevated)),
		uint32(unsafe.Sizeof(elevated)),
		&outLen,
	)
	if err != nil {
		return false
	}

	return elevated != 0
}

func setAutoStart(enable bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, RegKeyAutoStart, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if enable {
		if err = key.SetStringValue(AppName, fmt.Sprintf(`"%s"`, exePath)); err != nil {
			return err
		}
	} else {
		if err = key.DeleteValue(AppName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}

	return nil
}

func isAutoStartEnabled() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, RegKeyAutoStart, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	val, _, err := key.GetStringValue(AppName)
	if err != nil {
		return false
	}

	expected := fmt.Sprintf(`"%s"`, exePath)

	return strings.EqualFold(val, expected)
}
