# Gohomo

<img src="./logo.png" width="128" alt="logo">

**一个使用 [Go](https://go.dev) 编写的 Mihomo 封装托盘工具，目前仅支持 Windows 平台。**

---

<img src="./docs/screenshot.png" width="320" alt="screenshot">

---

## 🚀 使用方法

1. 从 [Releases](https://github.com/junlongzzz/gohomo/releases/latest) 页面下载最新版本。

2. 下载 [Mihomo](https://github.com/MetaCubeX/mihomo/releases) 可执行文件，并准备其配置文件 `config.yaml` (也支持 `.yml`
   格式)。

   > 配置文件可选，不存在时程序会默认创建可运行的最小配置文件，后续通过程序内的「编辑配置」功能再进行自定义编辑即可。

3. 将以上文件放在 `gohomo.exe` 同一目录下。

4. 运行 `gohomo.exe`，程序会在系统托盘中启动。

5. 开始使用 🎉

---

## ⚙️ 应用配置

应用配置储存在 `gohomo.yaml` 文件内，和 `gohomo.exe` 位于同一目录，可通过程序内的「应用配置」功能进行编辑。

| 配置项                | 类型            | 说明                                                               | 默认值       |
|--------------------|---------------|------------------------------------------------------------------|-----------|
| `proxy-by-pass`    | array(string) | 不走代理（直连）的地址列表，仅在系统代理模式下生效                                        | 常见私有 IP 段 |
| `proxy-mode`       | string        | 代理模式：`close` 关闭、`system` 系统代理、`tun` TUN 模式 [**推荐在托盘界面中修改**]      | `system`  |
| `core-log-enabled` | bool          | 是否将核心的运行日志写入文件                                                   | `false`   |
| `core-run-mode`    | string        | 核心运行模式：`rule` 规则、`global` 全局、`direct` 直连 [**推荐在托盘界面中修改**]        | `rule`    |
| `core-override`    | map           | 核心配置覆写，可自定义或覆盖 `Mihomo` 任意配置项，详见 [覆写规则](./docs/core-override.md) | `{}`      |

> 应用配置修改后会立即生效，无需重启程序或核心。

> Mihomo 核心配置文件修改后，需要手动「重启核心」。

---

## ⚠️ 注意事项

- 启用 **TUN 模式** 需要管理员权限运行程序。

---

Back to [README](./README.md)
