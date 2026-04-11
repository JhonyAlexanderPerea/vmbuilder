package models

import "time"

// ─── Estados ─────────────────────────────────────────────────────────────────

type VMState string

const (
	VMStateStopped  VMState = "stopped"
	VMStateRunning  VMState = "running"
	VMStateStarting VMState = "starting"
	VMStateError    VMState = "error"
)

type DiskState string

const (
	DiskStateReady      DiskState = "ready"
	DiskStateAttached   DiskState = "attached"
	DiskStateDetached   DiskState = "detached"
	DiskStateNoKeys     DiskState = "no_keys"
)

// ─── BaseVM ───────────────────────────────────────────────────────────────────

type BaseVM struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	State            VMState   `json:"state"`
	HasRootKeys      bool      `json:"has_root_keys"`
	RootPubKey       string    `json:"root_pub_key"`
	VBoxUUID         string    `json:"vbox_uuid"`
	DeletionPassword string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ─── Disk (multiconexión) ─────────────────────────────────────────────────────

type Disk struct {
	ID         int64     `json:"id"`
	BaseVMID   int64     `json:"base_vm_id"`
	Name       string    `json:"name"`
	FilePath   string    `json:"file_path"`   // ruta absoluta del .vdi
	State      DiskState `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
}

// ─── UserVM ───────────────────────────────────────────────────────────────────

type UserVM struct {
	ID          int64     `json:"id"`
	DiskID      int64     `json:"disk_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Username    string    `json:"username"`     // usuario creado en el SO
	State       VMState   `json:"state"`
	HasUserKeys bool      `json:"has_user_keys"`
	VBoxUUID         string    `json:"vbox_uuid"`
	DeletionPassword string    `json:"-"` // Contraseña requerida para eliminación
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ─── SSHKeyPair ───────────────────────────────────────────────────────────────

type SSHKeyPair struct {
	PrivateKeyPEM []byte // clave privada RSA 1024 bits (PEM)
	PublicKey     string // clave pública en formato authorized_keys
}
