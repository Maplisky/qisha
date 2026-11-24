# third_party

随仓库分发、不经过 Go 模块代理获取的第三方代码。每一项都必须写明出处、版本、许可与改动。

## norma（Agent 引擎 / harness SDK）

| | |
|---|---|
| 目录 | `norma/`（自带 `go.mod`，是一个独立的 Go 模块，不被 `./...` 匹配到） |
| 在主模块中的引用名 | `github.com/Autumn-27/norma`（`go.mod` 的 `replace` 指向本目录） |
| 取自 | `github.com/chu0119/Norma` 的标签 `v0.4.4`（Pojun 维护者对 `github.com/Autumn-27/norma` 的 fork；内容即上游 v0.4.4，模块声明保持原路径） |
| 入库时间 | 2026-11-25（七杀 v1.0.0） |
| 许可 | Apache License 2.0，全文见 `norma/LICENSE` |
| 改动 | 无。仅去掉了与引擎无关的 `cmd/` 与 `examples/` 两个目录；其余文件与发布内容逐文件一致 |

**为什么入库**：它是整个 Agent 层的地基，而它的原上游仓库曾被删除，fork 也同样是个人仓库。
放在仓库里，构建就不依赖上游仓库是否存在。

**如何升级**：用新版本的模块目录整体替换 `norma/`（保留 `LICENSE`），更新上表的「取自」与「改动」，
然后跑 `go build ./... && go vet ./... && go test -p 1 ./...`。不要直接修改 `norma/` 里的代码——
需要改动就先在上游提，或者在主模块里包一层。

入库时的比对命令（模块缓存与仓库目录逐文件比对，除被移除的目录外应无输出）：

```bash
diff -rq "$(go list -m -f '{{.Dir}}' github.com/Autumn-27/norma 2>/dev/null)" third_party/norma
```
