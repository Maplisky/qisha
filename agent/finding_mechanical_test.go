package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Maplisky/qisha/evidence"
)

// labRootShellEvidence 是 v1.0.1 靶场验收（任务 #6）里被证据闸门连拒三次的上报形态：
// 裸 TCP 拿到 root shell，证据全是命令回显，没有任何 HTTP 痕迹。闸门认不出它，执行者
// 就转而去目标网站根目录写文件凑状态码。触发串与主机名已替换，只保留证据的形态。
const labRootShellEvidence = `【目标】10.0.0.5:21/tcp FTP
【触发】发送触发串后控制连接降入 shell，同一连接直接发送命令
【权限证明（同 socket 回读原文）】
# id → uid=0(root) gid=0(root) groups=0(root),65534(nogroup)
# whoami → root
# hostname → target
# uname -a → Linux target 4.10.0-28-generic x86_64
# head -n 5 /etc/shadow → root:!:17484:0:99999:7:::
【对照】拼写差一个字母的触发串 → 502 Unknown command，仅正确的触发串静默生效，排除误报
【伤害控制】全程只读命令，未写入、未删除、未持久化；触发前后 FTP 服务均正常响应
【验证时间】2026-11-25`

func TestMechanicalEvidenceRecognizesToolOutput(t *testing.T) {
	sha1 := strings.Repeat("0f", 20)
	sha512crypt := "$6$abcdefgh$" + strings.Repeat("a", 86)
	cases := map[string]string{
		"id 回显":          "uid=0(root) gid=0(root) groups=0(root),65534(nogroup)",
		"passwd 行":       "root:x:0:0:root:/root:/bin/bash",
		"shell 提示符":      "www-data@target:/var/www $ id",
		"Windows 身份":     `whoami → nt authority\system`,
		"Windows 目录":     ` Directory of C:\inetpub\wwwroot`,
		"FTP 登录回显":       "230 Login successful.",
		"FTP 口令错误回显":     "530 Login incorrect.",
		"匿名 FTP":         "Anonymous FTP is allowed here",
		"弱口令命中":          "[22][ssh] host: 10.0.0.5   login: admin   password: admin\n1 valid password found",
		"MySQL 报错":       "ERROR 1064 (42000): You have an error in your SQL syntax; check the manual",
		"PostgreSQL 报错":  "ERROR: syntax error at or near \"'\"",
		"Oracle 报错":      "ORA-01756: quoted string not properly terminated",
		"Java 异常栈":       `Exception in thread "main" java.lang.RuntimeException`,
		"PHP 文件包含回显":     "PHP Warning:  include(data://text/plain;base64,…): failed to open stream",
		"Redis 原始回复":     "-NOAUTH Authentication required.",
		"meterpreter 会话": "[*] Meterpreter session 2 opened (10.0.0.2:4444 -> 10.0.0.5:49152)",
		"状态行":            "HTTP/1.1 200 OK",
		"curl -w 的状态":    "post-cleanup HTTP status: 404",
		"等号写法的状态码":       "status=200 OK",
		"中文的状态码":         "清理回证：状态码 404，自建对象已消失",
		"状态码带原因短语":       "200 OK，body 88 字节",
		"JSON 响应字段":      `{"error":"invalid token"}`,
		"十六进制摘要":         "sha256: " + sha1,
		"crypt 口令哈希":     "operator:" + sha512crypt + ":17484:0:99999:7:::",
		"bcrypt":         "$2y$10$" + strings.Repeat("a", 53),
		"JWT":            "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"Windows SID":    "S-1-5-21-3623811015-3361044348-30300820-1004",
		"通关凭据":           "flag{lab_placeholder}",
	}
	for name, ev := range cases {
		if !hasMechanicalVerification(ev) {
			t.Errorf("%s：没有认出工具原始输出 %q", name, ev)
		}
	}

	reject := map[string]string{
		"只有结论":      "已确认目标存在未认证命令执行，可直接取得 root 权限，危害严重。",
		"nmap 端口行":  "nmap -sV 的结果：21/tcp open ftp ProFTPD 1.3.3c",
		"服务 banner": "Server: Apache/2.4.18 (Ubuntu)",
		"SSH 版本横幅":  "SSH-2.0-OpenSSH_7.6p1 Ubuntu-4ubuntu0.3",
		"裸数字不是状态码":  "共发现 200 个端口，其中 403 个被过滤",
	}
	for name, ev := range reject {
		if hasMechanicalVerification(ev) {
			t.Errorf("%s：不该算作工具原始输出 %q", name, ev)
		}
	}
}

func TestValidateMechanicalEvidence(t *testing.T) {
	// medium/low 与空证据不归这道闸门管（空证据由 validateFindingEvidence 处理）。
	for _, sev := range []string{"medium", "low", ""} {
		if got := validateMechanicalEvidence(sev, "只有结论"); got != "" {
			t.Errorf("%s 级别不该被机械凭据闸门拦下: %s", sev, got)
		}
	}
	if got := validateMechanicalEvidence("critical", "  \n"); got != "" {
		t.Errorf("空证据不该在这里打回: %s", got)
	}

	// 回归：验收任务 #6 的第一次上报就该通过。
	for _, sev := range []string{"critical", "HIGH"} {
		if got := validateMechanicalEvidence(sev, labRootShellEvidence); got != "" {
			t.Fatalf("%s 的命令回显证据被打回: %s", sev, got)
		}
	}

	if got := validateMechanicalEvidence("critical", "Server: Apache/2.4.18 (Ubuntu)，该版本存在已知远程代码执行漏洞。"); !strings.Contains(got, "版本匹配 ≠ 漏洞") {
		t.Fatalf("纯版本匹配不该放行: %q", got)
	}
	if got := validateMechanicalEvidence("high", "已确认目标存在未认证命令执行，可直接取得 root 权限。"); got == "" {
		t.Fatal("没有任何原始输出的 high 上报不该放行")
	}
}

// 纠正提示会原样回给模型。它只能要求「把已经拿到的输出贴上来」，不能读起来像
// 「再去目标上做点什么」——旧文案点名要 HTTP 状态码，执行者照字面去目标上写了测试文件。
func TestMechanicalEvidenceCorrectionDoesNotSteerToNewActions(t *testing.T) {
	for name, msg := range map[string]string{
		"纯版本匹配":  validateMechanicalEvidence("critical", "Server: Apache/2.4.18 (Ubuntu)，该版本存在已知远程代码执行漏洞。"),
		"没有原始输出": validateMechanicalEvidence("critical", "已确认目标存在未认证命令执行，可直接取得 root 权限。"),
	} {
		if strings.Contains(msg, "http_probe") || strings.Contains(msg, "HTTP 状态码/") {
			t.Errorf("%s：纠正提示仍在把人往 HTTP 证据上引: %s", name, msg)
		}
	}
	msg := validateMechanicalEvidence("critical", "已确认目标存在未认证命令执行，可直接取得 root 权限。")
	for _, want := range []string{"命令执行的回显", "不要为这条校验再去对目标做新的操作"} {
		if !strings.Contains(msg, want) {
			t.Errorf("纠正提示缺少「%s」: %s", want, msg)
		}
	}
}

func TestReportFindingAcceptsCommandEchoEvidence(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	task, err := d.CreateTask("command echo evidence", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	ts := NewToolSet(nil, "")
	ts.ts, ts.taskID = d.Exploration(task.ExplorationID), task.ID
	ts.SetFindingRecorder(evidence.New(d, nil, t.TempDir()))
	ts.notifyFinding = func(int64, string) {}
	call := func(evidenceText string) string {
		t.Helper()
		body, err := json.Marshal(map[string]string{
			"vulnclass": "RCE", "name": "fixture backdoor RCE", "severity": "critical",
			"summary": "unauthenticated root command execution", "evidence": evidenceText,
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := ts.addFinding().Call(t.Context(), body, nil)
		if err != nil {
			t.Fatal(err)
		}
		return res.Flatten()
	}
	if got := call(labRootShellEvidence); !strings.HasPrefix(got, "finding recorded: ") {
		t.Fatalf("命令回显证据的 critical 上报被打回: %s", got)
	}
	conclusionOnly := "已确认目标存在未认证命令执行，可直接取得 root 权限，危害严重。" + strings.Repeat("结论属实，但这里没有任何原始输出。", 15)
	if got := call(conclusionOnly); !strings.Contains(got, "可机械复现的验证凭据") {
		t.Fatalf("没有任何原始输出的 critical 上报不该入库: %s", got)
	}
}
