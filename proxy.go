package main

import (
	"log"
	"regexp"
)

const regFullKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const defaultBypass = "localhost;127.*;192.168.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;<local>"

var patternEnable = regexp.MustCompile(`ProxyEnable\s+REG_DWORD\s+0x(?P<value>\d+)`)
var patternServer = regexp.MustCompile(`ProxyServer\s+REG_SZ\s+(?P<value>\S+)`)
var patternBypass = regexp.MustCompile(`ProxyOverride\s+REG_SZ\s+(?P<value>\S+)`)

// 获取是否开启了代理
func getProxyEnable() bool {
	cmd := execCommand("reg", "query", regFullKey, "/v", "ProxyEnable", "/t", "REG_DWORD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error executing command:", err)
		return false
	}

	match := patternEnable.FindStringSubmatch(string(output))
	if len(match) == 0 {
		return false
	}

	return match[1] == "1"
}

// 获取代理服务器地址
func getProxyServer() string {
	cmd := execCommand("reg", "query", regFullKey, "/v", "ProxyServer", "/t", "REG_SZ")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error executing command:", err)
		return ""
	}

	match := patternServer.FindStringSubmatch(string(output))
	if len(match) == 0 {
		return ""
	}

	return match[1]
}

// 获取代理白名单
func getProxyBypass() string {
	cmd := execCommand("reg", "query", regFullKey, "/v", "ProxyOverride", "/t", "REG_SZ")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error executing command:", err)
		return ""
	}

	match := patternBypass.FindStringSubmatch(string(output))
	if len(match) == 0 {
		return ""
	}

	return match[1]
}

// 设置代理
func setProxy(enable bool, server string, bypass string) bool {
	enableValue := "0"
	if enable {
		enableValue = "1"
	}

	// Set ProxyEnable
	cmdEnable := execCommand("reg", "add", regFullKey, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", enableValue, "/f")
	if output, err := cmdEnable.CombinedOutput(); err != nil {
		log.Println("Error setting ProxyEnable:", string(output), err)
		return false
	}

	// Set ProxyServer
	if server != "" {
		cmdServer := execCommand("reg", "add", regFullKey, "/v", "ProxyServer", "/t", "REG_SZ", "/d", server, "/f")
		if output, err := cmdServer.CombinedOutput(); err != nil {
			log.Println("Error setting ProxyServer:", string(output), err)
			return false
		}
	}

	// Set ProxyOverride (Bypass list)
	if bypass != "" {
		cmdBypass := execCommand("reg", "add", regFullKey, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", bypass, "/f")
		if output, err := cmdBypass.CombinedOutput(); err != nil {
			log.Println("Error setting ProxyOverride:", string(output), err)
			return false
		}
	}

	return true
}

// 取消代理
func unsetProxy() bool {
	return setProxy(false, "", "")
}
