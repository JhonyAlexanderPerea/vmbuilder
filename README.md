# VM Builder — Plataforma Web de Gestión de Máquinas Virtuales

## Stack tecnológico
- **Backend**: Go 1.22+ (net/http, chi router)
- **Frontend**: HTML5 + htmx + Tailwind CSS (sin build step)
- **Persistencia**: SQLite (via modernc.org/sqlite — sin CGO)
- **VirtualBox**: VBoxManage CLI via os/exec
- **SSH/Keys**: crypto/ssh (estándar Go)
- **Tiempo real**: SSE (Server-Sent Events) para estado de operaciones

## Estructura del proyecto
```
vm-platform/
├── cmd/
│   └── server/
│       └── main.go              # Punto de entrada
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── base_vm.go       # Handlers para VMs base
│   │   │   ├── disk.go          # Handlers para discos multiconexión
│   │   │   ├── user_vm.go       # Handlers para VMs de usuario
│   │   │   └── dashboard.go     # Handler del dashboard
│   │   ├── middleware/
│   │   │   └── middleware.go
│   │   └── router.go
│   ├── models/
│   │   ├── base_vm.go
│   │   ├── disk.go
│   │   └── user_vm.go
│   ├── services/
│   │   ├── vm_service.go        # Lógica de negocio VMs
│   │   ├── disk_service.go      # Lógica de discos
│   │   ├── ssh_service.go       # Gestión de llaves SSH / usuarios
│   │   └── vbox_service.go      # Wrapper VBoxManage
│   ├── repository/
│   │   ├── db.go                # Inicialización SQLite
│   │   ├── base_vm_repo.go
│   │   ├── disk_repo.go
│   │   └── user_vm_repo.go
│   └── config/
│       └── config.go
├── web/
│   ├── templates/
│   │   ├── layout.html
│   │   ├── dashboard.html
│   │   ├── base_vm_card.html
│   │   ├── disk_card.html
│   │   └── user_vm_card.html
│   └── static/
│       └── app.js
├── go.mod
└── go.sum
```

## Ejecución
```bash
go mod tidy
go run cmd/server/main.go
# Acceder en http://localhost:8080
```
