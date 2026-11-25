package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 收场提醒之后，删除文件的命令必须放行：模型手上可能还有没清理的自建测试对象，
// 拦下清理命令，东西就留在目标上了（v1.0.1 靶场验收里，预算提醒就这样拦过一次 rm）。
// 放行不等于放任——清理命令照常推进计数，拿它绕不开收场。
func TestIdleStopNeverBlocksCleanupCommand(t *testing.T) {
	fw := &fakeWrites{}
	hooks := wrapIdleStop(nil, fw.get, func(string) {})
	ageStopper(hooks.stop, time.Hour)

	// 先把计数推到阈值（连续零写回），进入「已提示」状态。
	for i := 0; i < idleStopRounds+1; i++ {
		if block, _, _ := hooks.PreToolUse(context.Background(), "Bash", []byte(`{}`)); !block {
			hooks.PostToolUse(context.Background(), "Bash", []byte(`{}`), []byte(`ok`), false)
		}
	}
	if !hooks.nudged {
		t.Fatal("前置条件不成立：此时应已处于提示后的状态")
	}

	cleanup := []byte(`{"command":"rm -f /var/www/html/pojun-test.txt"}`)
	if block, msg, _ := hooks.PreToolUse(context.Background(), "Bash", cleanup); block {
		t.Fatalf("清理命令被收场提醒拦下了（msg=%q）——测试对象会留在目标上", msg)
	}
	before := hooks.stop.rounds
	hooks.PostToolUse(context.Background(), "Bash", cleanup, []byte(`ok`), false)
	if hooks.stop.rounds != before+1 {
		t.Fatalf("清理命令应当照常推进计数：%d → %d", before, hooks.stop.rounds)
	}

	probe := []byte(`{"command":"nmap -sV 10.0.0.5"}`)
	block, msg, _ := hooks.PreToolUse(context.Background(), "Bash", probe)
	if !block {
		t.Fatal("提示后的新探测仍应被拦下")
	}
	if !strings.Contains(msg, "清理") {
		t.Errorf("提示后仍不收场时，追加说明应讲明清理不受限: %q", msg)
	}
}

func TestIsCleanupCommand(t *testing.T) {
	bash := func(cmd string) []byte {
		b, err := json.Marshal(map[string]string{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	yes := map[string]string{
		"rm":           "rm -f /var/www/html/pojun-test.txt",
		"经脚本下发的 rm":    "python3 - <<'EOF'\ns.sendall(b\"rm -f /var/www/html/pojun-test.txt\\n\")\nEOF",
		"find -delete": "find /tmp -maxdepth 1 -name 'pojun-*' -delete",
		"提权后删除":        "sudo rm /var/www/html/pojun-test.txt",
		"PowerShell":   `Remove-Item C:\inetpub\wwwroot\pojun-test.txt`,
	}
	for name, cmd := range yes {
		if !IsCleanupCommand("Bash", bash(cmd)) {
			t.Errorf("%s：没有认出清理命令 %q", name, cmd)
		}
	}
	no := map[string]string{
		"端口扫描":   "nmap -sV 10.0.0.5",
		"读文件":    "cat /etc/passwd",
		"Web 请求": "curl -s http://10.0.0.5/",
		"建目录":    "mkdir -p /tmp/pojun-scan",
	}
	for name, cmd := range no {
		if IsCleanupCommand("Bash", bash(cmd)) {
			t.Errorf("%s：不该算作清理命令 %q", name, cmd)
		}
	}
	if IsCleanupCommand("record_fact", bash("rm -f x")) {
		t.Error("非 Bash 工具不该按命令文本判定")
	}
	if IsCleanupCommand("Bash", []byte(`not json`)) {
		t.Error("入参不是 JSON 时不该判为清理命令")
	}
}
