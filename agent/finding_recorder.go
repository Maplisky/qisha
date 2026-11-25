package agent

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Maplisky/qisha/db"
)

// validateFindingEvidence 高严重度证据门槛（v0.4.5，借鉴 Shannon 的 L3 门槛）：
// critical/high 必须有达到最低篇幅的可核验证据，挡住"一句话上报严重漏洞"。
// medium/low 不设门槛——低危经常只有一句说明，过度校验会阻碍正常上报。
// 返回空串表示通过，否则是回灌给模型的中文纠正提示（模型可在同会话补证据或降级）。
func validateFindingEvidence(severity, evidence string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(evidence))
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		if n < 200 {
			return "证据不足以支撑 critical 级别：evidence 至少需要 200 字的可核验原始证据（请求/响应原文、命令输出、回显片段）。请补全证据后重新调用 report_finding；若实际只达到 L2（证明输入影响了后端逻辑但未拿到真实影响），请把 severity 降为 medium 并写明利用条件。"
		}
	case "high":
		if n < 120 {
			return "证据不足以支撑 high 级别：evidence 至少需要 120 字的可核验原始证据（请求/响应原文、命令输出或回显片段）。请补全后重新调用 report_finding；若只是间接推断，请把 severity 降为 medium 并注明。"
		}
	}
	return ""
}

// ═══ v0.4.52：机械验证凭据（借鉴 raptor 认识论闸门 + CVEngine triage 三重夹逼）═══

// EvidenceGrade 是证据的确定性等级（floor：由工具输出决定，AI 不能高于它）。
type EvidenceGrade int

const (
	EvidenceNone     EvidenceGrade = iota // 没有机械验证凭据
	EvidenceVersion                       // 只有版本/banner 匹配（不可靠：发行版 backport 不改版本号）
	EvidenceBehavior                      // 有行为级验证（nuclei 命中/HTTP 200+特征/CVSS 触发）
	EvidenceReplay                        // 有可复现回放（curl 命令+响应、截图、流量 ID）
)

// mechanicalSignals 是只有工具原样输出才会出现的字面片段（按小写比较），命中任意一条
// 即视为带有可机械复现的验证凭据。
//
// 清单按「这段文字是不是工具吐出来的」归类，不按协议。v1.0.1 的靶场验收里，裸 TCP 的
// root 回显（uid=0(root)）因为这里只有 HTTP 形态的信号被连拒三次，执行者为了凑出
// 「HTTP 状态码」转而去目标网站根目录做写测试——闸门把人往哪种证据上逼，人就会去做
// 哪种操作。
//
// 刻意不收的：nmap 端口行、服务 banner、版本号。它们是侦察结论，不是行为级验证，
// 收进来「纯版本匹配不通过」就名存实亡。
var mechanicalSignals = []string{
	// HTTP 原始响应与 Web 工具输出
	"http/1.", "http/2",
	"curl -", "curl ",
	"sqlmap", "nuclei",
	"location:", "set-cookie:", "content-type:",
	// JSON 响应字段
	`"code":`, `"status":`, `"error":`, `"message":`, `"success":`, `"token":`, `"password":`,
	// 非 HTTP 服务的认证结果与原始报错
	"login successful", "login incorrect", "anonymous access granted", "anonymous ftp", "anonymous login ok",
	"valid password found", "account found:", "discovered credentials", "(pwn3d!)",
	"invalid user", "permission denied (publickey", "access is denied",
	"+pong", "-noauth", "-wrongpass", "-err ",
	"you have an error in your sql syntax", "syntax error at or near", "unclosed quotation mark",
	"incorrect syntax near", "sqlite3.operationalerror",
	// 程序异常栈（反序列化 / 命令执行 / 文件包含 / 内存破坏类漏洞的回显）
	"exception in thread", "at java.", "traceback (most recent call last)",
	"fatal error: uncaught", "failed opening", "failed to open stream", "server error in ",
	"segmentation fault", "stack smashing detected", "buffer overflow detected",
	// 漏洞利用框架的会话回显
	"meterpreter", "session opened", "command shell session",
	// Windows 身份与配置回显
	`nt authority\system`, `builtin\administrators`, "microsoft windows [version", "cpassword=",
}

// mechanicalPatterns 是同一判定里字面片段表达不了的结构化形态，都对小写文本匹配。
var mechanicalPatterns = []*regexp.Regexp{
	// HTTP 状态行的各种写法：原始状态行、curl -w 的 "HTTP status: 404"、工具回显的 "status=200"
	regexp.MustCompile(`\bhttp(?:/\d(?:\.\d)?)?\s+[1-5]\d\d\b|\bstatus\s*[:=]\s*[1-5]\d\d\b|状态码\s*[:：=]?\s*[1-5]\d\d\b` +
		`|\b[1-5]\d\d (?:ok|created|accepted|no content|moved permanently|found|see other|not modified|bad request|unauthorized|forbidden|not found|method not allowed|request timeout|conflict|gone|internal server error|bad gateway|service unavailable|gateway timeout)\b`),
	regexp.MustCompile(`\b[ug]id=\d+\b`),                                        // id 的输出
	regexp.MustCompile(`\broot:[^:\s]*:0:0:`),                                   // 读到的 passwd 行
	regexp.MustCompile(`\b[a-z_][a-z0-9_-]*@[a-z0-9][a-z0-9.-]*:[^\s]*\s*[$#]`), // shell 提示符 user@host:path$
	regexp.MustCompile(`directory of [a-z]:\\`),                                 // Windows 目录回显
	regexp.MustCompile(`\bora-\d{5}\b`),                                         // Oracle 报错码
	regexp.MustCompile(`\bs-1-5-\d+(?:-\d+)+`),                                  // Windows SID
	regexp.MustCompile(`\b[0-9a-f]{32,}\b`),                                     // MD5 / SHA 摘要
	regexp.MustCompile(`\$[0-9a-z]{1,4}\$(?:rounds=\d+\$)?[./0-9a-z]{1,20}\$[./0-9a-z]{19,}` +
		`|\$2[abxy]?\$\d\d\$[./0-9a-z]{50,}`), // crypt 口令哈希（含 bcrypt）
	regexp.MustCompile(`\beyj[0-9a-z_-]{6,}\.[0-9a-z_-]{6,}\.[0-9a-z_-]*`), // JWT
	regexp.MustCompile(`[a-z0-9_]{2,}\{[^\s{}]{4,}\}`),                     // flag{…} 形态的通关凭据
}

// hasMechanicalVerification 检查 evidence 是否包含可机械复现的验证凭据。
// 凭据定义（借鉴 raptor [GATES] 的 "tool output is the verdict"）——工具实际返回的
// 原始输出：
//  1. HTTP 状态行与响应头、curl / sqlmap / nuclei 的输出
//  2. 命令执行的身份与权限回显（id、shell 提示符、读到的 passwd 行、Windows 身份）
//  3. 非 HTTP 服务的认证结果、数据库与程序的原始报错
//  4. 结构化数据（JSON 响应字段、十六进制摘要、crypt 口令哈希、JWT、SID）
//
// 这些是确定性信号——LLM 编造 evidence 时很难同时伪造所有形态。
func hasMechanicalVerification(evidence string) bool {
	lower := strings.ToLower(evidence)
	if hasAny(lower, mechanicalSignals...) {
		return true
	}
	for _, re := range mechanicalPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

// hasVersionOnlyEvidence 检查是否只有版本匹配（借鉴 CVEngine match_policy 的
// "version ≠ vulnerability" 规则——发行版 backport 不改版本号）。
func hasVersionOnlyEvidence(evidence string) bool {
	lower := strings.ToLower(evidence)
	versionSignals := []string{"version", "banner", "server:", "x-powered-by", "powered-by"}
	hasVersion := false
	hasExploit := false
	for _, sig := range versionSignals {
		if strings.Contains(lower, sig) {
			hasVersion = true
			break
		}
	}
	exploitSignals := []string{"curl -", "200", "403", "http/1.", "response", "output"}
	for _, sig := range exploitSignals {
		if strings.Contains(lower, sig) {
			hasExploit = true
			break
		}
	}
	return hasVersion && !hasExploit
}

// validateMechanicalEvidence 机械验证凭据校验（v0.4.52，借鉴 raptor 认识论闸门）：
// critical/high 漏洞必须附可机械复现的验证凭据（HTTP 响应/curl 输出/工具输出）。
// 纯版本匹配不能作为漏洞证据（CVEngine match_policy：发行版 backport 不改版本号）。
// 返回空串表示通过，否则是回灌给模型的纠正提示。
func validateMechanicalEvidence(severity, evidence string) string {
	if strings.TrimSpace(evidence) == "" {
		return "" // 空 evidence 由 validateFindingEvidence 处理
	}
	lower := strings.ToLower(severity)
	if lower != "critical" && lower != "high" {
		return "" // medium/low 不做机械验证校验
	}
	if hasMechanicalVerification(evidence) {
		return "" // 有机械验证凭据，通过
	}
	if hasVersionOnlyEvidence(evidence) {
		return "证据只有版本/banner 匹配，没有行为级验证——发行版 backport 补丁不改版本号，版本匹配 ≠ 漏洞。" +
			"请用工具对目标实际验证（发送触发请求或执行验证命令），把返回的原始内容（HTTP 状态行与响应特征、命令回显等）原样写进 evidence。" +
			"如果只是版本匹配没有验证，请降为 medium 并注明「版本匹配，行为未验证」。"
	}
	// 纠正提示只要求把已经拿到的输出贴上来，不能读起来像「再去目标上做点什么」：
	// v1.0.1 靶场验收里，旧文案点名要 HTTP 状态码，执行者照字面去目标上写了测试文件。
	return "critical/high 证据缺少可机械复现的验证凭据：要有工具实际返回的原始输出——HTTP 状态行与响应头、curl 回显、" +
		"命令执行的回显（id/whoami/读到的文件内容等）、认证结果、数据转储的原始行或扫描工具的命中结果。" +
		"「可能存在」不是 finding，「实际拿到」才是。" +
		"把已经执行过的验证命令和它的原样输出贴进 evidence 即可，不要为这条校验再去对目标做新的操作（尤其是写入或改动目标数据）。" +
		"确实只做了间接推断的，请降级为 medium。"
}

// FindingRecorder is injected by the host; agents never synthesize or copy
// evidence bodies themselves. Its implementation owns the atomic write.
type FindingRecorder interface {
	Record(context.Context, db.RecordFindingInput, []db.TrafficRef) (*db.RecordedFinding, error)
}

func (t *ToolSet) SetFindingRecorder(r FindingRecorder)   { t.findingRecorder = r }
func (w *Worker) SetFindingRecorder(r FindingRecorder)    { w.findingRecorder = r }
func (p *Planner) SetFindingRecorder(r FindingRecorder)   { p.findingRecorder = r }
func (m *MainAgent) SetFindingRecorder(r FindingRecorder) { m.findingRecorder = r }
