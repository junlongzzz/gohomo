package main

import (
	"log"
	"regexp"
)

const (
	RegKeyPath    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	DefaultBypass = "localhost;127.*;192.168.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;<local>"
)

var (
	patternEnable = regexp.MustCompile(`(?i)ProxyEnable\s+[A-Za-z_]+\s+0x(\d+)`)
	patternServer = regexp.MustCompile(`(?i)ProxyServer\s+[A-Za-z_]+\s+(\S+)`)
	patternBypass = regexp.MustCompile(`(?i)ProxyOverride\s+[A-Za-z_]+\s+(\S+)`)
)

// 获取代理开启状态
func getProxyEnable() bool {
	cmd := execCommand("reg", "query", RegKeyPath, "/v", "ProxyEnable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	match := patternEnable.FindStringSubmatch(string(output))
	if len(match) > 1 {
		return match[1] == "1"
	}

	return false
}

// 获取代理服务器地址
func getProxyServer() string {
	cmd := execCommand("reg", "query", RegKeyPath, "/v", "ProxyServer")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	match := patternServer.FindStringSubmatch(string(output))
	if len(match) > 1 {
		return match[1]
	}

	return ""
}

// 获取代理白名单
func getProxyBypass() string {
	cmd := execCommand("reg", "query", RegKeyPath, "/v", "ProxyOverride")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	match := patternBypass.FindStringSubmatch(string(output))
	if len(match) > 1 {
		return match[1]
	}

	return ""
}

// 设置代理
func setProxy(enable bool, server string, bypass string) bool {
	// Set ProxyEnable
	enableValue := "0"
	if enable {
		enableValue = "1"
	}
	cmdEnable := execCommand("reg", "add", RegKeyPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", enableValue, "/f")
	if err := cmdEnable.Run(); err != nil {
		log.Println("Error setting ProxyEnable:", err)
		return false
	}

	// Set ProxyServer
	if server != "" {
		cmdServer := execCommand("reg", "add", RegKeyPath, "/v", "ProxyServer", "/t", "REG_SZ", "/d", server, "/f")
		if err := cmdServer.Run(); err != nil {
			log.Println("Error setting ProxyServer:", err)
			return false
		}
	}

	// Set ProxyOverride (Bypass list)
	if bypass != "" {
		cmdBypass := execCommand("reg", "add", RegKeyPath, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", bypass, "/f")
		if err := cmdBypass.Run(); err != nil {
			log.Println("Error setting ProxyOverride:", err)
			return false
		}
	}

	return true
}

// 设置代理并使用默认白名单
func setProxyWithDefaultBypass(enable bool, server string) bool {
	return setProxy(enable, server, DefaultBypass)
}

// 取消代理
func unsetProxy() bool {
	return setProxy(false, "", "")
}
