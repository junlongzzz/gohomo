package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// 使用 tasklist 命令检查进程是否正在运行
func isProcessRunning(processName string) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error check running:", string(output), err)
		return false
	}
	return strings.Contains(string(output), processName)
}

// 使用 taskkill 命令杀进程，支持 * 号模糊匹配
func killProcess(processName string) error {
	cmd := exec.Command("taskkill", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName), "/F")
	return cmd.Run()
}

// 根据 pid 杀进程
func killProcessByPid(pid int) error {
	cmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/F")
	return cmd.Run()
}

// 判断文件是否存在
func isFileExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}
