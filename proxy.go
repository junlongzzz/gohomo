package main

import (
	"strings"

	"github.com/xishang0128/sysproxy-go/sysproxy"
)

// 默认代理白名单
var defaultBypassHosts = []string{
	"localhost",
	"127.*",
	"192.168.*",
	"10.*",
	"172.16.*",
	"172.17.*",
	"172.18.*",
	"172.19.*",
	"172.20.*",
	"172.21.*",
	"172.22.*",
	"172.23.*",
	"172.24.*",
	"172.25.*",
	"172.26.*",
	"172.27.*",
	"172.28.*",
	"172.29.*",
	"172.30.*",
	"172.31.*",
	"<local>",
}

// 获取代理开启状态
func getProxyEnable() bool {
	proxyConfig, err := sysproxy.QueryProxySettings("", false)
	if err != nil {
		return false
	}
	return proxyConfig.Proxy.Enable
}

// 获取代理服务器地址
func getProxyServer() string {
	proxyConfig, err := sysproxy.QueryProxySettings("", false)
	if err != nil {
		return ""
	}
	return proxyConfig.Proxy.Servers["http_server"]
}

// 获取代理白名单
func getProxyBypass() string {
	proxyConfig, err := sysproxy.QueryProxySettings("", false)
	if err != nil {
		return ""
	}
	return proxyConfig.Proxy.Bypass
}

// 设置代理
func setProxy(enable bool, host, port, bypass string) bool {
	var err error
	if enable {
		if bypass == "" {
			// 使用默认白名单
			bypass = strings.Join(defaultBypassHosts, ";")
		}
		err = sysproxy.SetProxy(sysproxy.FormatServer(host, port), bypass, "", false)
	} else {
		err = sysproxy.DisableProxy("", false)
	}
	return err == nil
}

// 取消代理
func unsetProxy() bool {
	return setProxy(false, "", "", "")
}
