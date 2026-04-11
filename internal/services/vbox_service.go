package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		"--natpf1", "ssh,tcp,,2223,,22", // Volvemos al formato universal ,,2223,,22
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

// GetVMDiskPath intenta encontrar la ruta del primer disco rígido (HDD) de la VM.
func (v *VBoxService) GetVMDiskPath(nameOrUUID string) (string, error) {
	out, err := v.run("showvminfo", nameOrUUID, "--machinereadable")
	if err != nil {
		return "", err
	}
	
	lines := strings.Split(out, "\n")
	var cfgFile string
	for _, line := range lines {
		if strings.HasPrefix(line, "CfgFile=") {
			cfgFile = strings.Trim(strings.SplitN(line, "=", 2)[1], "\" \r\n")
		}
	}

	for _, line := range lines {
		lineLower := strings.ToLower(line)
		if (strings.Contains(lineLower, ".vdi") || strings.Contains(lineLower, ".vmdk")) {
			val := ""
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				val = parts[1]
			}
			if val != "" {
				path := strings.Trim(val, "\" \r\n")
				// Si es una snapshot, intentamos adivinar el base en la misma carpeta del .vbox
				if strings.Contains(path, "Snapshots") && cfgFile != "" {
					baseDir := filepath.Dir(cfgFile)
					vmName := ""
					for _, l := range lines {
						if strings.HasPrefix(l, "name=") {
							vmName = strings.Trim(strings.SplitN(l, "=", 2)[1], "\" \r\n")
						}
					}
					// Intentamos buscar NombreVM.vdi en la carpeta base
					basePath := filepath.Join(baseDir, vmName+".vdi")
					if _, err := os.Stat(basePath); err == nil {
						fmt.Printf("[DEBUG] Snapshot detectada. Usando base: %s\n", basePath)
						return basePath, nil
					}
				}
				
				if !strings.Contains(path, "Snapshots") {
					fmt.Printf("[DEBUG] Disco encontrado: %s\n", path)
					return path, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no se encontró el disco principal (base)")
}

// CreateMultiattachDisk crea un disco .vdi en modo multiattach.
// Si sourcePath no está vacío, clona ese disco en lugar de crear uno vacío.
func (v *VBoxService) CreateMultiattachDisk(vmName, filePath string) error {
	// 0. VERIFICACIÓN CRÍTICA: La máquina virtual DEBE estar apagada ("poweroff" o "aborted")
	state, err := v.VMState(vmName)
	if err == nil && state == "running" {
		return fmt.Errorf("la máquina '%s' está encendida. Para configurar el disco multiconexión, primero debés apagar la máquina virtual completamente", vmName)
	}

	// 1. Intentamos detectar si el disco está conectado para saber cómo volver a conectarlo
	out, _ := v.run("showvminfo", vmName, "--machinereadable")
	var controller, port, device string
	lines := strings.Split(out, "\n")
	
	for _, line := range lines {
		// Buscamos la conexión del hdd. Ej: "SATA-0-0"="C:\..."
		if strings.Contains(line, filePath) {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) > 0 {
				key := strings.Trim(parts[0], "\"") // SATA-0-0
				keyParts := strings.Split(key, "-")
				if len(keyParts) == 3 {
					controller = keyParts[0]
					port = keyParts[1]
					device = keyParts[2]
				}
			}
		}
	}

	// 2. Si lo encontramos conectado, lo desconectamos temporalmente (VBox no deja cambiar el modo de un disco atado)
	if controller != "" {
		fmt.Printf("[NINJA] Desconectando disco de %s (%s %s:%s)...\n", vmName, controller, port, device)
		_, _ = v.run("storageattach", vmName, "--storagectl", controller, "--port", port, "--device", device, "--medium", "none")
	}

	// 3. Cambiamos el modo a multiattach
	fmt.Printf("[NINJA] Cambiando modo a multiattach...\n")
	if _, err := v.run("modifymedium", filePath, "--type", "multiattach"); err != nil {
		// Si falla, intentamos reconectar antes de salir para no dejar la VM rota
		if controller != "" {
			_, _ = v.run("storageattach", vmName, "--storagectl", controller, "--port", port, "--device", device, "--type", "hdd", "--medium", filePath)
		}
		// Si el error contiene "locked", damos una sugerencia
		if strings.Contains(err.Error(), "locked") {
			return fmt.Errorf("el archivo del disco está bloqueado por VirtualBox. Por favor, cerrá la interfaz de VirtualBox o cualquier otra tarea que use la máquina '%s' e intentalo de nuevo", vmName)
		}
		return err
	}

	// 4. Si estaba conectado, lo volvemos a conectar
	if controller != "" {
		fmt.Printf("[NINJA] Reconectando disco a %s...\n", vmName)
		_, _ = v.run("storageattach", vmName, "--storagectl", controller, "--port", port, "--device", device, "--type", "hdd", "--medium", filePath)
	}

	return nil
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
