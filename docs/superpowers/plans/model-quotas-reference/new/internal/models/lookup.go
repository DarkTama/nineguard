package models

import "nineguard/internal/db"

// LookupStatus reads a model row without registering unknown ids
// (IsModelEnabled auto-registers, so it must not be used for read-side checks).
func LookupStatus(d *db.DB, id string) (found, enabled, removed bool) {
	var en int
	var rm bool
	err := d.QueryRow(`SELECT enabled, removed_at IS NOT NULL FROM models WHERE id = ? COLLATE NOCASE`, id).Scan(&en, &rm)
	if err != nil {
		return false, false, false
	}
	return true, en == 1, rm
}

func (m *Manager) Status(id string) (found, enabled, removed bool) {
	return LookupStatus(m.db, id)
}
