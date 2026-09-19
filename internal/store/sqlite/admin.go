package sqlite

import (
	"context"
	"strings"
	"time"

	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/store"
)

// 管理后台查询（store.AdminStore）。

func (s *Store) ListOrders(ctx context.Context, f store.OrderFilter) ([]*model.Order, int, error) {
	var (
		conds []string
		args  []any
	)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		conds = append(conds, `(trade_no LIKE ? ESCAPE '\' OR out_trade_no LIKE ? ESCAPE '\'
			OR api_trade_no LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	if f.Status != nil {
		conds = append(conds, "status = ?")
		args = append(args, *f.Status)
	}
	if f.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, f.Type)
	}
	if f.PID != "" {
		conds = append(conds, "pid = ?")
		args = append(args, f.PID)
	}
	if f.Notify != nil {
		conds = append(conds, "notify_status = ?")
		args = append(args, *f.Notify)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM orders `+where+
		` ORDER BY created_at DESC, trade_no DESC LIMIT ? OFFSET ?`, append(args, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, o)
	}
	return list, total, rows.Err()
}

func (s *Store) Summary(ctx context.Context, now time.Time, days int) (*store.Summary, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sum := &store.Summary{}

	err := s.db.QueryRowContext(ctx, `SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM orders WHERE created_at >= ?`, model.StatusPaid, today.Unix()).
		Scan(&sum.TodayOrders, &sum.TodayPaid)
	if err != nil {
		return nil, err
	}
	var total int64
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(money), 0) FROM orders WHERE status = ?`, model.StatusPaid).Scan(&total)
	if err != nil {
		return nil, err
	}
	sum.TotalAmount = money.Cents(total)
	err = s.db.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN notify_status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN notify_status = ? THEN 1 ELSE 0 END), 0)
		FROM orders WHERE notify_status IN (?, ?)`,
		model.NotifyPending, model.NotifyFailed, model.NotifyPending, model.NotifyFailed).
		Scan(&sum.NotifyPending, &sum.NotifyFailed)
	if err != nil {
		return nil, err
	}

	// 按天汇总在 Go 中完成，保证"天"的划分与服务器时区一致。
	start := today.AddDate(0, 0, -(days - 1))
	sum.Daily = make([]store.DailyStat, days) // 预分配，保证下面取得的元素指针不会因扩容失效
	buckets := make(map[string]*store.DailyStat, days)
	for i := range sum.Daily {
		sum.Daily[i].Date = start.AddDate(0, 0, i).Format(time.DateOnly)
		buckets[sum.Daily[i].Date] = &sum.Daily[i]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT paid_at, money FROM orders WHERE status = ? AND paid_at >= ?`,
		model.StatusPaid, start.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var paidAt, amount int64
		if err := rows.Scan(&paidAt, &amount); err != nil {
			return nil, err
		}
		if b, ok := buckets[time.Unix(paidAt, 0).In(now.Location()).Format(time.DateOnly)]; ok {
			b.Count++
			b.Amount += money.Cents(amount)
		}
	}
	if b, ok := buckets[today.Format(time.DateOnly)]; ok {
		sum.TodayAmount = b.Amount
	}
	return sum, rows.Err()
}

func (s *Store) ResetNotify(ctx context.Context, tradeNo string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE orders SET notify_status = ?, notify_count = 0, next_notify_at = ?,
		notify_error = '', updated_at = ? WHERE trade_no = ? AND status = ?`,
		model.NotifyPending, time.Now().Unix(), time.Now().Unix(), tradeNo, model.StatusPaid)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// escapeLike 转义 LIKE 通配符，避免用户输入的 % _ 被当作模式。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
