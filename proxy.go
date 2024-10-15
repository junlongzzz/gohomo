package main

import (
	"log"

	"golang.org/x/sys/windows/registry"
)

const regKeyPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const defaultBypass = "localhost;127.*;192.168.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;<local>"

// 获取是否开启了代理
func getProxyEnable() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, regKeyPath, registry.QUERY_VALUE)
	if err != nil {
		log.Println("Error opening registry key:", err)
		return false
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil {
		return false
	}
	return value == 1
}

// 获取代理服务器地址
func getProxyServer() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, regKeyPath, registry.QUERY_VALUE)
	if err != nil {
		log.Println("Error opening registry key:", err)
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return value
}

// 获取代理白名单
func getProxyBypass() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, regKeyPath, registry.QUERY_VALUE)
	if err != nil {
		log.Println("Error opening registry key:", err)
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue("ProxyOverride")
	if err != nil {
		return ""
	}
	return value
}

// 设置代理
func setProxy(enable bool, server string, bypass string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, regKeyPath, registry.SET_VALUE)
	if err != nil {
		log.Println("Error opening registry key:", err)
		return false
	}
	defer key.Close()

	// Set ProxyEnable
	enableValue := 0
	if enable {
		enableValue = 1
	}
	if err = key.SetDWordValue("ProxyEnable", uint32(enableValue)); err != nil {
		log.Println("Error setting ProxyEnable:", err)
		return false
	}

	// Set ProxyServer
	if server != "" {
		if err = key.SetStringValue("ProxyServer", server); err != nil {
			log.Println("Error setting ProxyServer:", err)
			return false
		}
	}

	// Set ProxyOverride (Bypass list)
	if bypass != "" {
		if err = key.SetStringValue("ProxyOverride", bypass); err != nil {
			log.Println("Error setting ProxyOverride:", err)
			return false
		}
	}

	return true
}

// 取消代理
func unsetProxy() bool {
	return setProxy(false, "", "")
}
