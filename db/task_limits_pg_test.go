package db

import "testing"

// 任务级规划轮数上限的落库往返：创建时写入、统一读取路径带出、只改指定项的上限更新。
func TestTaskLimitsRoundTrip(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	iptr := func(n int) *int { return &n }

	inherit, err := d.CreateTaskWithOptions("limits: inherit", "goal", TaskCreateOptions{TimeoutSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(inherit.ID)
	if got, _ := d.GetTask(inherit.ID); got == nil || got.MaxRounds != nil || got.TimeoutSeconds != 3600 {
		t.Fatalf("默认应沿用全局(MaxRounds=nil)且时间上限=3600: %+v", got)
	}

	task, err := d.CreateTaskWithOptions("limits: explicit", "goal", TaskCreateOptions{TimeoutSeconds: 1800, MaxRounds: iptr(40)})
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	got, err := d.GetTask(task.ID)
	if err != nil || got == nil || got.MaxRounds == nil || *got.MaxRounds != 40 || got.TimeoutSeconds != 1800 {
		t.Fatalf("创建后应读到 MaxRounds=40 TimeoutSeconds=1800: %+v err=%v", got, err)
	}
	// 列表路径走的是同一份列清单，也要带出任务级上限。
	listed, err := d.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed {
		if item.ID == task.ID {
			found = true
			if item.MaxRounds == nil || *item.MaxRounds != 40 {
				t.Fatalf("ListTasks 未带出 MaxRounds: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("ListTasks 未包含新建任务")
	}

	// 只改轮数上限：时间上限必须原样保留。
	if _, err := d.SetTaskLimits(task.ID, nil, iptr(0), true); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTask(task.ID)
	if got.MaxRounds == nil || *got.MaxRounds != 0 || got.TimeoutSeconds != 1800 {
		t.Fatalf("改轮数为 0 后应 MaxRounds=0 TimeoutSeconds=1800: %+v", got)
	}
	// 只改时间上限：轮数上限必须原样保留。
	if _, err := d.SetTaskLimits(task.ID, iptr(7200), nil, false); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTask(task.ID)
	if got.TimeoutSeconds != 7200 || got.MaxRounds == nil || *got.MaxRounds != 0 {
		t.Fatalf("改时间为 7200 后应 TimeoutSeconds=7200 MaxRounds=0: %+v", got)
	}
	// 显式 null：改回沿用全局设置，时间上限不动。
	if _, err := d.SetTaskLimits(task.ID, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTask(task.ID)
	if got.MaxRounds != nil || got.TimeoutSeconds != 7200 {
		t.Fatalf("显式 null 后应 MaxRounds=nil TimeoutSeconds=7200: %+v", got)
	}
	// 什么都没指定：两个字段都不动。
	if _, err := d.SetTaskLimits(task.ID, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTask(task.ID)
	if got.MaxRounds != nil || got.TimeoutSeconds != 7200 {
		t.Fatalf("空更新不应改变任何上限: %+v", got)
	}
}
