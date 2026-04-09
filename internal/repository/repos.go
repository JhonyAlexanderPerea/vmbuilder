package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/uq/vm-platform/internal/models"
)

// ─── BaseVM Repository ────────────────────────────────────────────────────────

type BaseVMRepo struct{ db *DB }

func NewBaseVMRepo(db *DB) *BaseVMRepo { return &BaseVMRepo{db} }

func (r *BaseVMRepo) Create(name, description string) (*models.BaseVM, error) {
	res, err := r.db.Exec(
		`INSERT INTO base_vms (name, description) VALUES (?, ?)`,
		name, description,
	)
	if err != nil {
		return nil, fmt.Errorf("crear base_vm: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

func (r *BaseVMRepo) FindByID(id int64) (*models.BaseVM, error) {
	row := r.db.QueryRow(`SELECT id, name, description, state, has_root_keys, vbox_uuid, created_at, updated_at FROM base_vms WHERE id = ?`, id)
	return scanBaseVM(row)
}

func (r *BaseVMRepo) FindAll() ([]*models.BaseVM, error) {
	rows, err := r.db.Query(`SELECT id, name, description, state, has_root_keys, vbox_uuid, created_at, updated_at FROM base_vms ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBaseVMs(rows)
}

func (r *BaseVMRepo) SetRootKeys(id int64, privKey []byte, pubKey string) error {
	_, err := r.db.Exec(
		`UPDATE base_vms SET has_root_keys=1, root_priv_key=?, root_pub_key=?, updated_at=? WHERE id=?`,
		privKey, pubKey, time.Now(), id,
	)
	return err
}

func (r *BaseVMRepo) GetRootKeys(id int64) (privKey []byte, pubKey string, err error) {
	row := r.db.QueryRow(`SELECT root_priv_key, root_pub_key FROM base_vms WHERE id=?`, id)
	err = row.Scan(&privKey, &pubKey)
	return
}

func (r *BaseVMRepo) SetState(id int64, state models.VMState) error {
	_, err := r.db.Exec(`UPDATE base_vms SET state=?, updated_at=? WHERE id=?`, state, time.Now(), id)
	return err
}

func (r *BaseVMRepo) SetVBoxUUID(id int64, uuid string) error {
	_, err := r.db.Exec(`UPDATE base_vms SET vbox_uuid=?, updated_at=? WHERE id=?`, uuid, time.Now(), id)
	return err
}

func (r *BaseVMRepo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM base_vms WHERE id=?`, id)
	return err
}

func scanBaseVM(row *sql.Row) (*models.BaseVM, error) {
	v := &models.BaseVM{}
	var vboxUUID sql.NullString
	err := row.Scan(&v.ID, &v.Name, &v.Description, &v.State, &v.HasRootKeys, &vboxUUID, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	v.VBoxUUID = vboxUUID.String
	return v, nil
}

func scanBaseVMs(rows *sql.Rows) ([]*models.BaseVM, error) {
	var vms []*models.BaseVM
	for rows.Next() {
		v := &models.BaseVM{}
		var vboxUUID sql.NullString
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.State, &v.HasRootKeys, &vboxUUID, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.VBoxUUID = vboxUUID.String
		vms = append(vms, v)
	}
	return vms, rows.Err()
}

// ─── Disk Repository ──────────────────────────────────────────────────────────

type DiskRepo struct{ db *DB }

func NewDiskRepo(db *DB) *DiskRepo { return &DiskRepo{db} }

func (r *DiskRepo) Create(baseVMID int64, name, filePath string) (*models.Disk, error) {
	res, err := r.db.Exec(
		`INSERT INTO disks (base_vm_id, name, file_path, state) VALUES (?, ?, ?, 'no_keys')`,
		baseVMID, name, filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("crear disk: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

func (r *DiskRepo) FindByID(id int64) (*models.Disk, error) {
	row := r.db.QueryRow(`SELECT id, base_vm_id, name, file_path, state, created_at FROM disks WHERE id=?`, id)
	d := &models.Disk{}
	err := row.Scan(&d.ID, &d.BaseVMID, &d.Name, &d.FilePath, &d.State, &d.CreatedAt)
	return d, err
}

func (r *DiskRepo) FindByBaseVM(baseVMID int64) ([]*models.Disk, error) {
	rows, err := r.db.Query(
		`SELECT id, base_vm_id, name, file_path, state, created_at FROM disks WHERE base_vm_id=? ORDER BY created_at DESC`,
		baseVMID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var disks []*models.Disk
	for rows.Next() {
		d := &models.Disk{}
		if err := rows.Scan(&d.ID, &d.BaseVMID, &d.Name, &d.FilePath, &d.State, &d.CreatedAt); err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, rows.Err()
}

func (r *DiskRepo) FindAll() ([]*models.Disk, error) {
	rows, err := r.db.Query(`SELECT id, base_vm_id, name, file_path, state, created_at FROM disks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var disks []*models.Disk
	for rows.Next() {
		d := &models.Disk{}
		if err := rows.Scan(&d.ID, &d.BaseVMID, &d.Name, &d.FilePath, &d.State, &d.CreatedAt); err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, rows.Err()
}

func (r *DiskRepo) SetState(id int64, state models.DiskState) error {
	_, err := r.db.Exec(`UPDATE disks SET state=? WHERE id=?`, state, id)
	return err
}

func (r *DiskRepo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM disks WHERE id=?`, id)
	return err
}

// ─── UserVM Repository ────────────────────────────────────────────────────────

type UserVMRepo struct{ db *DB }

func NewUserVMRepo(db *DB) *UserVMRepo { return &UserVMRepo{db} }

func (r *UserVMRepo) Create(diskID int64, name, description string) (*models.UserVM, error) {
	res, err := r.db.Exec(
		`INSERT INTO user_vms (disk_id, name, description) VALUES (?, ?, ?)`,
		diskID, name, description,
	)
	if err != nil {
		return nil, fmt.Errorf("crear user_vm: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

func (r *UserVMRepo) FindByID(id int64) (*models.UserVM, error) {
	row := r.db.QueryRow(
		`SELECT id, disk_id, name, description, username, state, has_user_keys, vbox_uuid, ssh_port, created_at, updated_at FROM user_vms WHERE id=?`,
		id,
	)
	return scanUserVM(row)
}

func (r *UserVMRepo) FindAll() ([]*models.UserVM, error) {
	rows, err := r.db.Query(
		`SELECT id, disk_id, name, description, username, state, has_user_keys, vbox_uuid, ssh_port, created_at, updated_at FROM user_vms ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vms []*models.UserVM
	for rows.Next() {
		if uv, err := scanUserVMRow(rows); err == nil {
			vms = append(vms, uv)
		}
	}
	return vms, rows.Err()
}

func (r *UserVMRepo) SetUserKeys(id int64, username string, privKey []byte, pubKey string) error {
	_, err := r.db.Exec(
		`UPDATE user_vms SET has_user_keys=1, username=?, user_priv_key=?, user_pub_key=?, updated_at=? WHERE id=?`,
		username, privKey, pubKey, time.Now(), id,
	)
	return err
}

func (r *UserVMRepo) GetUserKeys(id int64) (privKey []byte, pubKey string, err error) {
	row := r.db.QueryRow(`SELECT user_priv_key, user_pub_key FROM user_vms WHERE id=?`, id)
	err = row.Scan(&privKey, &pubKey)
	return
}

func (r *UserVMRepo) SetState(id int64, state models.VMState) error {
	_, err := r.db.Exec(`UPDATE user_vms SET state=?, updated_at=? WHERE id=?`, state, time.Now(), id)
	return err
}

func (r *UserVMRepo) SetVBoxUUID(id int64, uuid string, sshPort int) error {
	_, err := r.db.Exec(
		`UPDATE user_vms SET vbox_uuid=?, ssh_port=?, updated_at=? WHERE id=?`,
		uuid, sshPort, time.Now(), id,
	)
	return err
}

func (r *UserVMRepo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM user_vms WHERE id=?`, id)
	return err
}

func scanUserVM(row *sql.Row) (*models.UserVM, error) {
	uv := &models.UserVM{}
	var vboxUUID sql.NullString
	var sshPort sql.NullInt64
	err := row.Scan(&uv.ID, &uv.DiskID, &uv.Name, &uv.Description, &uv.Username,
		&uv.State, &uv.HasUserKeys, &vboxUUID, &sshPort, &uv.CreatedAt, &uv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	uv.VBoxUUID = vboxUUID.String
	return uv, nil
}

func scanUserVMRow(rows *sql.Rows) (*models.UserVM, error) {
	uv := &models.UserVM{}
	var vboxUUID sql.NullString
	var sshPort sql.NullInt64
	err := rows.Scan(&uv.ID, &uv.DiskID, &uv.Name, &uv.Description, &uv.Username,
		&uv.State, &uv.HasUserKeys, &vboxUUID, &sshPort, &uv.CreatedAt, &uv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	uv.VBoxUUID = vboxUUID.String
	return uv, nil
}
