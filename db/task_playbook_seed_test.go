package db

import (
	"strings"
	"testing"
)

// allBuiltinTaskPlaybooks 是全部批次的内置任务模板（按批次顺序）。
func allBuiltinTaskPlaybooks() []builtinTaskPlaybook {
	out := make([]builtinTaskPlaybook, 0, len(builtinTaskPlaybooks)+len(qishaTaskPlaybooks))
	out = append(out, builtinTaskPlaybooks...)
	return append(out, qishaTaskPlaybooks...)
}

// builtinGoalCount 统计某段目标文本在库里出现几次（按内容比对，用来发现重复播种）。
func builtinGoalCount(list []*TaskTemplate, goal string) int {
	n := 0
	for _, tpl := range list {
		if tpl.Goal == goal {
			n++
		}
	}
	return n
}

// v0.4.60：内置任务 Playbook 种子——模板入库、幂等、可检索。
// 七杀 v1.0.0 起分两批（各有 settings flag），全新安装应一次播全。
func TestSeedBuiltinTaskPlaybooks(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	// 干净批次：清掉本测试的历史种子标记与同名模板（共享测试库）。
	for _, pb := range allBuiltinTaskPlaybooks() {
		_, _ = d.Exec(`DELETE FROM task_templates WHERE name=$1`, pb.name)
	}
	_, _ = d.Exec(`DELETE FROM settings WHERE key IN ('task_playbooks_v1','task_playbooks_v2')`)

	if err := d.seedBuiltinTaskPlaybooks(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	list, err := d.ListTaskTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, pb := range allBuiltinTaskPlaybooks() {
		if n := builtinGoalCount(list, pb.goal); n != 1 {
			t.Errorf("playbook %q 应恰好入库一次，实际 %d 次", pb.name, n)
		}
	}
	// 第二批的每个模板都要带「目标名称」占位符与分节结构，否则套用后目标是一段死文本。
	for _, pb := range qishaTaskPlaybooks {
		if !strings.Contains(pb.goal, "{{目标名称}}") {
			t.Errorf("playbook %q 缺少 {{目标名称}} 占位符", pb.name)
		}
		if strings.Count(pb.goal, "【") < 3 {
			t.Errorf("playbook %q 的分节不足 3 个（授权范围/工作流/验收/边界）", pb.name)
		}
	}
	// 幂等：再跑一次不新增、不报错（两个 flag 都已 done）。
	before := len(list)
	if err := d.seedBuiltinTaskPlaybooks(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	after, _ := d.ListTaskTemplates()
	if len(after) != before {
		t.Fatalf("重复种子不应新增：before=%d after=%d", before, len(after))
	}
}

// 升级路径（七杀 v1.0.0）：从上游 Pojun 升上来的实例只有第一批，且 v1 的 flag 早已置 done。
// 启动后第二批必须被补种；用户自己建的、与内置模板同名的模板不能被覆盖。
func TestSeedBuiltinTaskPlaybooksUpgradesFromFirstBatch(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	for _, pb := range qishaTaskPlaybooks {
		_, _ = d.Exec(`DELETE FROM task_templates WHERE name=$1`, pb.name)
	}
	_, _ = d.Exec(`DELETE FROM settings WHERE key IN ('task_playbooks_v1','task_playbooks_v2')`)
	// 模拟老实例：v1 已播种完毕；用户恰好自己建了一个与内置模板同名的模板。
	if err := d.SetSetting("task_playbooks_v1", "done"); err != nil {
		t.Fatal(err)
	}
	first := qishaTaskPlaybooks[0]
	const userGoal = "用户自己写的目标，不能被种子覆盖"
	if _, err := d.CreateTaskTemplate(TaskTemplateInput{Name: first.name, Description: "用户的", Goal: userGoal}); err != nil {
		t.Fatalf("预置同名模板: %v", err)
	}

	if err := d.seedBuiltinTaskPlaybooks(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	list, err := d.ListTaskTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range list {
		if tpl.Name == first.name && tpl.Goal != userGoal {
			t.Errorf("同名模板被种子覆盖了：%q", tpl.Goal)
		}
	}
	for i, pb := range qishaTaskPlaybooks {
		if i == 0 {
			continue // 同名的那一个保留用户内容
		}
		if builtinGoalCount(list, pb.goal) != 1 {
			t.Errorf("升级后第二批模板 %q 没有被补种", pb.name)
		}
	}
	if v, ok, _ := d.GetSetting("task_playbooks_v2"); !ok || v != "done" {
		t.Errorf("升级后 task_playbooks_v2 应置 done，实际 %q (ok=%v)", v, ok)
	}
}
