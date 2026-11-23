package db

// AttachmentCached reports whether an attachment's contents are in the cache,
// without loading them.
func (db *DB) AttachmentCached(id int64) (bool, error) {
	var present bool
	err := db.QueryRow("SELECT length(data)>0 OR size=0 FROM attachments WHERE id=?", id).Scan(&present)
	return present, err
}
