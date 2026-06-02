package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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

// 根据进程名称查找进程id
func findProcessId(processName string) int32 {
	if processName == "" {
		return 0
	}
	processes, _ := process.Processes()
	for _, p := range processes {
		name, err := p.Name()
		if err == nil && strings.EqualFold(name, processName) {
			return p.Pid
		}
	}
	return 0
}

// 根据进程id查找进程对象
func findProcessByPid(pid int32) *process.Process {
	if pid <= 0 {
		return nil
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil
	}
	return p
}

// 检查指定 pid 的进程是否还存活
func isPidAlive(pid int32) bool {
	if pid <= 0 {
		return false
	}
	exists, _ := process.PidExists(pid)
	return exists
}

// 尝试优雅的结束指定 pid 的进程，如未成功则强制结束
func killProcessGracefully(pid int32) error {
	if pid <= 0 {
		return nil
	}

	p, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}

	if err = sendCtrlBreakEvent(pid); err != nil {
		log.Printf("send CTRL_BREAK to %d failed, force kill: %v\n", pid, err)
		// 强制结束进程
		if err = p.Kill(); err != nil {
			return err
		}
	}

	// 等退出，超时则强制结束
	if err = waitForProcessExit(pid, 5*time.Second); err != nil {
		log.Printf("graceful shutdown pid %d timeout, force kill\n", pid)
		if err = p.Kill(); err != nil {
			return err
		}
		return waitForProcessExit(pid, 3*time.Second)
	}
	return nil
}

// 参考：https://github.com/GUI-for-Cores/GUI.for.Clash/blob/main/bridge/exec_windows.go
func sendCtrlBreakEvent(pid int32) error {
	if pid <= 0 {
		return errors.New("pid can not be zero")
	}
	// 先脱离当前 console；GUI 进程本身没有 console，ERROR_INVALID_HANDLE 是正常的
	ret, _, err := procFreeConsole.Call()
	if ret == 0 && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return err
	}

	// 恢复父控制台
	defer procAttachConsole.Call(uintptr(^uint32(0)))

	// 尝试附加到目标进程的控制台
	ret, _, err = procAttachConsole.Call(uintptr(pid))
	if ret == 0 {
		// ERROR_INVALID_HANDLE：目标进程没有控制台，无法发送 Ctrl 事件，需要走 force kill
		// ERROR_ACCESS_DENIED：已有控制台，可能是权限问题，仍然尝试发送
		if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return fmt.Errorf("target process %d has no console: %w", pid, windows.ERROR_INVALID_HANDLE)
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return err
		}
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

func waitForProcessExit(pid int32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if exists, _ := process.PidExists(pid); !exists {
			// 防止“刚退出又被自启动拉起”
			time.Sleep(100 * time.Millisecond)
			if exists2, _ := process.PidExists(pid); !exists2 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process %d still alive after %v", pid, timeout)
}

// 判断文件是否存在
func isFileExist(path string) bool {
	if path == "" {
		return false
	}
	// 仅当能成功 stat 时才视为存在；权限等错误不再误判为存在而走错分支
	_, err := os.Stat(path)
	return err == nil
}

// 用系统默认关联程序打开 URL / 文件 / 目录
func shellOpen(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(target)
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
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
		// CREATE_NO_WINDOW: 控制台子进程不分配可见的 conhost 窗口
		// CREATE_NEW_PROCESS_GROUP: 让子进程接收 Ctrl+Break 信号以优雅退出
		CreationFlags: windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	return cmd
}

// 发送通知
func sendNotification(message string) {
	var icon any
	if notificationIcon != nil {
		icon = notificationIcon
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
					// 新建切片承载结果，避免 append 就地写入 v 的底层数组（v 来自不可变的 CoreOverride 快照）
					target[k] = append(append([]any{}, v...), existing...)
				} else {
					// 目标无此数组时，前插退化为整体设置，否则覆写内容会被静默丢弃
					target[k] = v
				}

			// xxx+ 后插
			case strings.HasSuffix(key, "+"):
				k := trimWrap(key[:len(key)-1])
				if existing, ok := target[k].([]any); ok {
					target[k] = append(append([]any{}, existing...), v...)
				} else {
					// 目标无此数组时，后插退化为整体设置
					target[k] = v
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
