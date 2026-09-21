// Package sqlite 基于 SQLite（纯 Go 实现，无需 CGO）实现 store.OrderStore。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/store"
)

const schema = `
CREATE TABLE IF NOT EXISTS orders (
	trade_no       TEXT PRIMARY KEY,
	out_trade_no   TEXT    NOT NULL,
	pid            TEXT    NOT NULL,
	type           TEXT    NOT NULL,
	name           TEXT    NOT NULL,
	money          INTEGER NOT NULL,
	param          TEXT    NOT NULL DEFAULT '',
	notify_url     TEXT    NOT NULL,
	return_url     TEXT    NOT NULL DEFAULT '',
	client_ip      TEXT    NOT NULL DEFAULT '',
	device         TEXT    NOT NULL DEFAULT '',
	status         INTEGER NOT NULL DEFAULT 0,
	pay_kind       TEXT    NOT NULL DEFAULT '',
	pay_content    TEXT    NOT NULL DEFAULT '',
	upstream_ref   TEXT    NOT NULL DEFAULT '',
	pay_currency   TEXT    NOT NULL DEFAULT '',
	pay_amount     INTEGER NOT NULL DEFAULT 0,
	api_trade_no   TEXT    NOT NULL DEFAULT '',
	buyer          TEXT    NOT NULL DEFAULT '',
	refund_money   INTEGER NOT NULL DEFAULT 0,
	notify_status  INTEGER NOT NULL DEFAULT 0,
	notify_count   INTEGER NOT NULL DEFAULT 0,
	next_notify_at INTEGER NOT NULL DEFAULT 0,
	notify_error   TEXT    NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	expire_at      INTEGER NOT NULL DEFAULT 0,
	paid_at        INTEGER NOT NULL DEFAULT 0,
	updated_at     INTEGER NOT NULL,
	UNIQUE (pid, out_trade_no)
);
CREATE INDEX IF NOT EXISTS idx_orders_notify ON orders (notify_status, next_notify_at);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders (created_at);
CREATE INDEX IF NOT EXISTS idx_orders_paid ON orders (status, paid_at);

CREATE TABLE IF NOT EXISTS merchants (
	pid        TEXT PRIMARY KEY,
	key        TEXT    NOT NULL,
	name       TEXT    NOT NULL DEFAULT '',
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS channels (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	type       TEXT    NOT NULL UNIQUE,
	driver     TEXT    NOT NULL,
	name       TEXT    NOT NULL DEFAULT '',
	enabled    INTEGER NOT NULL DEFAULT 1,
	options    TEXT    NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// columns 与 scanOrder 的字段顺序必须保持一致。
const columns = `trade_no, out_trade_no, pid, type, name, money, param, notify_url, return_url,
	client_ip, device, status, pay_kind, pay_content, upstream_ref, pay_currency, pay_amount,
	api_trade_no, buyer, refund_money, notify_status, notify_count, next_notify_at, notify_error,
	created_at, expire_at, paid_at`

type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// Open 打开（必要时创建）数据库文件并执行建表。
//
// 数据库中保存着渠道与商户密钥的明文，因此新建的数据目录与数据库文件都只对属主可读写；
// SQLite 后续创建的 -wal / -shm 会沿用数据库文件的权限。已存在的文件不改动其权限，
// 如果是从旧版本升级，可手动执行 chmod 600 data/epay.db*。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("创建数据目录: %w", err)
		}
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Close()
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 同一时刻只允许一个写者，单连接可彻底避免 SQLITE_BUSY；
	// 网关的写入量很小，这不会成为瓶颈。
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, o *model.Order) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO orders (`+columns+`, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.TradeNo, o.OutTradeNo, o.PID, o.Type, o.Name, int64(o.Money), o.Param, o.NotifyURL, o.ReturnURL,
		o.ClientIP, o.Device, o.Status, o.PayKind, o.PayContent, o.UpstreamRef, o.PayCurrency, o.PayAmount,
		o.APITradeNo, o.Buyer, int64(o.RefundMoney), o.NotifyStatus, o.NotifyCount, unix(o.NextNotifyAt), o.NotifyError,
		unix(o.CreatedAt), unix(o.ExpireAt), unix(o.PaidAt), now)
	return mapUnique(err)
}

func (s *Store) GetByTradeNo(ctx context.Context, tradeNo string) (*model.Order, error) {
	return s.getOne(ctx, `WHERE trade_no = ?`, tradeNo)
}

func (s *Store) GetByOutTradeNo(ctx context.Context, pid, outTradeNo string) (*model.Order, error) {
	return s.getOne(ctx, `WHERE pid = ? AND out_trade_no = ?`, pid, outTradeNo)
}

func (s *Store) SavePayment(ctx context.Context, tradeNo string, p store.PaymentInfo) error {
	_, err := s.db.ExecContext(ctx, `UPDATE orders SET pay_kind = ?, pay_content = ?, upstream_ref = ?,
		pay_currency = ?, pay_amount = ?, updated_at = ? WHERE trade_no = ? AND status = ?`,
		p.Kind, p.Content, p.UpstreamRef, p.Currency, p.Amount, time.Now().Unix(), tradeNo, model.StatusPending)
	return err
}

func (s *Store) MarkPaid(ctx context.Context, tradeNo, apiTradeNo, buyer string, paidAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE orders SET status = ?, api_trade_no = ?, buyer = ?, paid_at = ?,
		notify_status = ?, notify_count = 0, next_notify_at = ?, updated_at = ?
		WHERE trade_no = ? AND status = ?`,
		model.StatusPaid, apiTradeNo, buyer, paidAt.Unix(),
		model.NotifyPending, paidAt.Unix(), time.Now().Unix(),
		tradeNo, model.StatusPending)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) DueNotifications(ctx context.Context, now time.Time, limit int) ([]*model.Order, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM orders
		WHERE notify_status = ? AND next_notify_at <= ? ORDER BY next_notify_at LIMIT ?`,
		model.NotifyPending, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

func (s *Store) UpdateNotify(ctx context.Context, tradeNo string, u store.NotifyUpdate) error {
	_, err := s.db.ExecContext(ctx, `UPDATE orders SET notify_status = ?, notify_count = ?, next_notify_at = ?,
		notify_error = ?, updated_at = ? WHERE trade_no = ?`,
		u.Status, u.Count, unix(u.NextAt), u.Error, time.Now().Unix(), tradeNo)
	return err
}

func (s *Store) ReserveRefund(ctx context.Context, tradeNo string, amount money.Cents) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE orders SET refund_money = refund_money + ?, updated_at = ?
		WHERE trade_no = ? AND status = ? AND refund_money + ? <= money`,
		int64(amount), time.Now().Unix(), tradeNo, model.StatusPaid, int64(amount))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) ReleaseRefund(ctx context.Context, tradeNo string, amount money.Cents) error {
	_, err := s.db.ExecContext(ctx, `UPDATE orders SET refund_money = MAX(refund_money - ?, 0), updated_at = ?
		WHERE trade_no = ?`, int64(amount), time.Now().Unix(), tradeNo)
	return err
}

func (s *Store) getOne(ctx context.Context, where string, args ...any) (*model.Order, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM orders `+where, args...)
	o, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return o, err
}

type scanner interface{ Scan(dest ...any) error }

func scanOrder(r scanner) (*model.Order, error) {
	var (
		o                                       model.Order
		orderMoney, refundMoney                 int64
		nextNotify, createdAt, expireAt, paidAt int64
	)
	err := r.Scan(&o.TradeNo, &o.OutTradeNo, &o.PID, &o.Type, &o.Name, &orderMoney, &o.Param, &o.NotifyURL, &o.ReturnURL,
		&o.ClientIP, &o.Device, &o.Status, &o.PayKind, &o.PayContent, &o.UpstreamRef, &o.PayCurrency, &o.PayAmount,
		&o.APITradeNo, &o.Buyer, &refundMoney, &o.NotifyStatus, &o.NotifyCount, &nextNotify, &o.NotifyError,
		&createdAt, &expireAt, &paidAt)
	if err != nil {
		return nil, err
	}
	o.Money = money.Cents(orderMoney)
	o.RefundMoney = money.Cents(refundMoney)
	o.NextNotifyAt = fromUnix(nextNotify)
	o.CreatedAt = fromUnix(createdAt)
	o.ExpireAt = fromUnix(expireAt)
	o.PaidAt = fromUnix(paidAt)
	return &o, nil
}

// mapUnique 把唯一约束冲突转换为 store.ErrDuplicate。
func mapUnique(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return store.ErrDuplicate
	}
	return err
}

// 时间统一以 Unix 秒存储，零值时间存为 0。
func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
