## 🛠️ 覆写规则

`core-override` 的覆写采用**深度合并**策略。这意味着，你的自定义配置会与核心原有配置智能融合，而非粗暴替换。你可以通过特殊后缀来控制合并的具体行为。

**1. 普通值：直接覆盖**

对于字符串、数字、布尔值等简单类型，无特殊规则，直接用新值覆盖旧值。

```yaml
# 原配置
# mixed-port: 7890

# 覆写配置
core-override:
  mixed-port: 7895

# 最终效果
# mixed-port: 7895
```

**2. 对象（Map）：默认递归合并，支持强制覆盖**

* **默认行为**：递归合并。新旧都是对象时，会保留原对象中的键，仅更新或添加你指定的键。
* **强制覆盖**：在键名后加上 `!` (如 `dns!:`)，会用新对象**完全替换**原对象，不再进行递归合并。

```yaml
# 原配置
# dns:
#   enable: true
#   ipv6: true

# 覆写配置：默认合并
core-override:
  dns:
    ipv6: false       # 仅将 ipv6 改为 false，enable 依然为 true
    listen: :1053     # 新增一个 listen 字段

# 最终效果
# dns:
#   enable: true
#   ipv6: false
#   listen: :1053
```

```yaml
# 覆写配置：强制覆盖
core-override:
  dns!:
    enable: false     # 整个原 dns 对象被丢弃，完全由这个新对象替代

# 最终效果
# dns:
#   enable: false
```

**3. 数组（List）：支持覆盖、前插、后插**

数组默认是整体覆盖。如果想在原有数组基础上添加元素，需要用 `+` 前缀或后缀。

* **整体覆盖** (`key:`)：直接替换整个数组。
* **前插** (`+key:`)：将新数组元素全部插入到原数组**最前面**。
* **后插** (`key+:`)：将新数组元素全部追加到原数组**最后面**。

```yaml
# 原配置
# rules:
#   - GEOIP,CN,DIRECT
#   - MATCH,Proxy

# -------------------------------
# 示例 1: 整体覆盖
# core-override:
#   rules:
#     - DOMAIN,baidu.com,DIRECT
# 最终效果：rules 只剩这一条
#   - DOMAIN,baidu.com,DIRECT

# -------------------------------
# 示例 2: 前插
# core-override:
#   +rules:
#     - DOMAIN,baidu.com,DIRECT
# 最终效果：新规则被放在了最前面
#   - DOMAIN,baidu.com,DIRECT
#   - GEOIP,CN,DIRECT
#   - MATCH,Proxy

# -------------------------------
# 示例 3: 后插
# core-override:
#   rules+:
#     - DOMAIN-SUFFIX,google.com,Proxy
# 最终效果：新规则被追加到了最后
#   - GEOIP,CN,DIRECT
#   - MATCH,Proxy
#   - DOMAIN-SUFFIX,google.com,Proxy
```

**4. 特殊键名修饰：用 `<>` 避免歧义**

如果**原配置键名本身**就以 `+` 开头或结尾，直接书写会造成歧义。此时必须用 `<>` 包裹原键名，程序会自动去除 `<>` 进行匹配。

```yaml
# 原配置
# dns:
#   nameserver-policy:
#     +.google.cn:      # 键名本身就以 + 开头
#       - 8.8.8.8

# 覆写配置示例
core-override:
  dns:
    nameserver-policy:
      # 1. 直接覆盖原来的"+.google.cn"项
      <+.google.cn>:
        - 114.114.114.114
      # 2. 在“+.google.cn”项前插入新内容
      +<+.google.cn>:
        - 223.5.5.5
      # 3. 在“+.google.cn”项后追加新内容
      <+.google.cn>+:
        - 119.29.29.29
```

---

## ⚠️ 注意事项

- **覆写优先级**：`core-override` 的优先级低于程序直接接管的配置项（比如 TUN 的是否启用 `enable` 配置覆写无效，但 TUN
  的其余配置是完全支持的）
