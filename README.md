# Gohomo

![logo](./logo.png)

**Wrapper for [Mihomo](https://github.com/MetaCubeX/mihomo) written in [Golang](https://go.dev), only support Windows
now.**

## Usage

1. Download the latest release from [GitHub Releases](https://github.com/junlongzzz/gohomo/releases/latest).
2. Put [Mihomo](https://github.com/MetaCubeX/mihomo/releases) executable binary and `config.yaml` (.yml also supported)
   into the same directory as `gohomo.exe`.
3. Run `gohomo.exe` and you will see it in the system tray.
4. Enjoy!

## Configuration

> Application configuration file `gohomo.yaml` in the same directory as `gohomo.exe`

| Key                | Type          | Description                                        | Default Value                   |
|--------------------|---------------|----------------------------------------------------|---------------------------------|
| `core-log-enabled` | bool          | Enable writing core logs to a file for persistence | `false`                         |
| `proxy-by-pass`    | array(string) | Proxy bypass addresses                             | (`common private IP addresses`) |
