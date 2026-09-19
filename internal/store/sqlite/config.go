package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"epay/internal/model"
	"epay/internal/store"
)

// 商户、支付渠道与系统设置（store.ConfigStore）。

func (s *Store) ListMerchants(ctx context.Context) ([]model.Merchant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pid, key, name, enabled, created_at, updated_at
		FROM merchants ORDER BY created_at, pid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.Merchant
	for rows.Next() {
		var m model.Merchant
		var created, updated int64
		if err := rows.Scan(&m.PID, &m.Key, &m.Name, &m.Enabled, &created, &updated); err != nil {
			return nil, err
		}
		m.CreatedAt, m.UpdatedAt = fromUnix(created), fromUnix(updated)
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) CreateMerchant(ctx context.Context, m *model.Merchant) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO merchants (pid, key, name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, m.PID, m.Key, m.Name, m.Enabled, now.Unix(), now.Unix())
	if err == nil {
		m.CreatedAt, m.UpdatedAt = now, now
	}
	return mapUnique(err)
}

func (s *Store) UpdateMerchant(ctx context.Context, m *model.Merchant) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE merchants SET key = ?, name = ?, enabled = ?, updated_at = ?
		WHERE pid = ?`, m.Key, m.Name, m.Enabled, now.Unix(), m.PID)
	if err != nil {
		return err
	}
	m.UpdatedAt = now
	return mustAffect(res)
}

func (s *Store) DeleteMerchant(ctx context.Context, pid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM merchants WHERE pid = ?`, pid)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

const channelColumns = `id, type, driver, name, enabled, options, created_at, updated_at`

func (s *Store) ListChannels(ctx context.Context) ([]model.ChannelConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelColumns+` FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.ChannelConfig
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *c)
	}
	return list, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, id int64) (*model.ChannelConfig, error) {
	c, err := scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return c, err
}

func (s *Store) CreateChannel(ctx context.Context, c *model.ChannelConfig) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO channels (type, driver, name, enabled, options, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, c.Type, c.Driver, c.Name, c.Enabled, optionsText(c.Options), now.Unix(), now.Unix())
	if err != nil {
		return mapUnique(err)
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt, c.UpdatedAt = now, now
	return nil
}

func (s *Store) UpdateChannel(ctx context.Context, c *model.ChannelConfig) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE channels SET type = ?, driver = ?, name = ?, enabled = ?, options = ?,
		updated_at = ? WHERE id = ?`, c.Type, c.Driver, c.Name, c.Enabled, optionsText(c.Options), now.Unix(), c.ID)
	if err != nil {
		return mapUnique(err)
	}
	c.UpdatedAt = now
	return mustAffect(res)
}

func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func scanChannel(r scanner) (*model.ChannelConfig, error) {
	var c model.ChannelConfig
	var options string
	var created, updated int64
	if err := r.Scan(&c.ID, &c.Type, &c.Driver, &c.Name, &c.Enabled, &options, &created, &updated); err != nil {
		return nil, err
	}
	c.Options = json.RawMessage(options)
	c.CreatedAt, c.UpdatedAt = fromUnix(created), fromUnix(updated)
	return &c, nil
}

func optionsText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

// mustAffect 更新 / 删除未命中任何行时返回 ErrNotFound。
func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}
