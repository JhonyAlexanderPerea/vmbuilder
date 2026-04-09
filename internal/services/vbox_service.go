package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// VBoxService encapsula todas las llamadas a VBoxManage.
// Cada método devuelve error con contexto claro para el handler.
type VBoxService struct {
	vboxManage string // ruta al ejecutable, default "VBoxManage"
}

func NewVBoxService() *VBoxService {
	return &VBoxService{vboxManage: "VBoxManage"}
}

// run ejecuta un comando VBoxManage con timeout y captura stdout/stderr.
func (v *VBoxService) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, v.vboxManage, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("VBoxManage %s: %w — %s", strings.Join(args, " "), err, errBuf.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// ─── VM lifecycle ─────────────────────────────────────────────────────────────

// StartVM arranca una VM por nombre o UUID.
func (v *VBoxService) StartVM(nameOrUUID string) error {
	_, err := v.run("startvm", nameOrUUID, "--type", "headless")
	return err
}

// StopVM apaga la VM limpiamente (acpi).
func (v *VBoxService) StopVM(nameOrUUID string) error {
	_, err := v.run("controlvm", nameOrUUID, "acpipowerbutton")
	return err
}

// PowerOffVM fuerza el apagado.
func (v *VBoxService) PowerOffVM(nameOrUUID string) error {
	_, err := v.run("controlvm", nameOrUUID, "poweroff")
	return err
}

// DeleteVM elimina la VM y libera sus medios.
func (v *VBoxService) DeleteVM(nameOrUUID string) error {
	_, err := v.run("unregistervm", nameOrUUID, "--delete")
	return err
}

// VMState devuelve el estado actual: "running", "stopped", etc.
func (v *VBoxService) VMState(nameOrUUID string) (string, error) {
	out, err := v.run("showvminfo", nameOrUUID, "--machinereadable")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "VMState=") {
			state := strings.TrimPrefix(line, "VMState=")
			return strings.Trim(state, `"`), nil
		}
	}
	return "unknown", nil
}

// GetVMIP intenta obtener la IP de la VM vía GuestProperty (requiere VirtualBox Guest Additions).
func (v *VBoxService) GetVMIP(nameOrUUID string) (string, error) {
	out, err := v.run("guestproperty", "get", nameOrUUID,
		"/VirtualBox/GuestInfo/Net/0/V4/IP")
	if err != nil {
		return "", err
	}
	// Formato: "Value: 192.168.x.x"
	parts := strings.SplitN(out, ": ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1]), nil
	}
	return "", fmt.Errorf("IP no disponible aún")
}

// ─── VM creation from multiattach disk ───────────────────────────────────────

// CreateUserVM registra y configura una nueva VM usando un disco multiconexión.
// Devuelve el UUID asignado por VirtualBox.
func (v *VBoxService) CreateUserVM(name, osType, diskPath string, ramMB, cpus int) (string, error) {
	// 1. Crear la VM
	if _, err := v.run("createvm", "--name", name, "--ostype", osType, "--register"); err != nil {
		return "", fmt.Errorf("createvm: %w", err)
	}

	// 2. Configurar RAM y CPUs
	if _, err := v.run("modifyvm", name,
		"--memory", fmt.Sprint(ramMB),
		"--cpus", fmt.Sprint(cpus),
		"--nic1", "nat",
		"--natpf1", "ssh,tcp,,0,,22", // port forwarding SSH (VirtualBox asigna puerto libre)
		"--audio", "none",
	); err != nil {
		return "", fmt.Errorf("modifyvm: %w", err)
	}

	// 3. Agregar controlador de almacenamiento SATA
	if _, err := v.run("storagectl", name, "--name", "SATA", "--add", "sata"); err != nil {
		return "", fmt.Errorf("storagectl: %w", err)
	}

	// 4. Conectar el disco multiconexión (multiattach)
	if _, err := v.run("storageattach", name,
		"--storagectl", "SATA",
		"--port", "0",
		"--device", "0",
		"--type", "hdd",
		"--medium", diskPath,
	); err != nil {
		return "", fmt.Errorf("storageattach: %w", err)
	}

	// 5. Obtener UUID
	uuid, err := v.GetVMUUID(name)
	if err != nil {
		return "", err
	}
	return uuid, nil
}

// GetVMUUID retorna el UUID de una VM por nombre.
func (v *VBoxService) GetVMUUID(name string) (string, error) {
	out, err := v.run("showvminfo", name, "--machinereadable")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UUID=") {
			uuid := strings.TrimPrefix(line, "UUID=")
			return strings.Trim(uuid, `"`), nil
		}
	}
	return "", fmt.Errorf("UUID no encontrado para VM '%s'", name)
}

// ─── Disk operations ──────────────────────────────────────────────────────────

// CreateMultiattachDisk crea un disco .vdi en modo multiattach.
// sizeMB es el tamaño del disco.
func (v *VBoxService) CreateMultiattachDisk(filePath string, sizeMB int) error {
	_, err := v.run("createmedium", "disk",
		"--filename", filePath,
		"--size", fmt.Sprint(sizeMB),
		"--variant", "Standard",
	)
	if err != nil {
		return err
	}
	// Cambiar el tipo a multiattach
	_, err = v.run("modifymedium", filePath, "--type", "multiattach")
	return err
}

// DetachDisk desconecta un disco de una VM.
func (v *VBoxService) DetachDisk(vmNameOrUUID, storagectl string, port, device int) error {
	_, err := v.run("storageattach", vmNameOrUUID,
		"--storagectl", storagectl,
		"--port", fmt.Sprint(port),
		"--device", fmt.Sprint(device),
		"--medium", "none",
	)
	return err
}

// DeleteDisk elimina un medio (archivo VDI) de VirtualBox.
func (v *VBoxService) DeleteDisk(filePath string) error {
	_, err := v.run("closemedium", "disk", filePath, "--delete")
	return err
}

// GetNATSSHPort obtiene el puerto NAT mapeado al SSH de la VM (si se configuró).
func (v *VBoxService) GetNATSSHPort(vmName string) (int, error) {
	out, err := v.run("showvminfo", vmName, "--machinereadable")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, `"ssh"`) && strings.Contains(line, "tcp") {
			// Formato: Forwarding(0)="ssh,tcp,,PORT,,22"
			parts := strings.Split(line, ",")
			if len(parts) >= 4 {
				var port int
				fmt.Sscanf(parts[3], "%d", &port)
				if port > 0 {
					return port, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("puerto SSH NAT no encontrado")
}
