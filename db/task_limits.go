package db

// SetTaskLimits 更新一个任务的上限字段，只改调用方明确要求改的项：
//   - timeoutSeconds：nil=不改时间上限；非 nil=写入新值（0=不限时）。
//   - maxRounds：仅当 maxRoundsSet 为 true 时写入；此时 nil=改回沿用全局设置，0=不限。
//
// 它不触碰 first_run_at/deadline_at——已盖章的墙钟预算保持不变，新上限要等下一次
// 盖章（任务首次运行，或「继续运行」走终态复活）才生效。返回更新后的任务行。
func (d *DB) SetTaskLimits(id int64, timeoutSeconds, maxRounds *int, maxRoundsSet bool) (*Task, error) {
	task, err := scanTask(d.QueryRow(`UPDATE tasks SET
	timeout_seconds = CASE WHEN $2::boolean THEN $3::integer ELSE timeout_seconds END,
	max_rounds      = CASE WHEN $4::boolean THEN $5::integer ELSE max_rounds END
WHERE id=$1 AND deleted_at IS NULL
RETURNING `+taskCols,
		id, timeoutSeconds != nil, timeoutSeconds, maxRoundsSet, maxRounds))
	if err != nil {
		return nil, err
	}
	return task, nil
}
