package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/uq/vm-platform/internal/models"
	"github.com/uq/vm-platform/internal/repository"
	"github.com/uq/vm-platform/internal/services"
)

// ─── Deps container ───────────────────────────────────────────────────────────

type Handler struct {
	BaseVMRepo *repository.BaseVMRepo
	DiskRepo   *repository.DiskRepo
	UserVMRepo *repository.UserVMRepo
	VBox       *services.VBoxService
	SSH        *services.SSHService
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func respondErr(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}

func paramInt(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, key), 10, 64)
}

// ─── Dashboard ────────────────────────────────────────────────────────────────

// GetDashboard devuelve el estado completo para el dashboard.
// Además, intenta sincronizar el estado real de las VMs con la base de datos.
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	baseVMs, err := h.BaseVMRepo.FindAll()
	if err != nil {
		respondErr(w, 500, "Error cargando VMs base: "+err.Error())
		return
	}
	// Sincronizar Base VMs
	for _, vm := range baseVMs {
		liveState, _ := h.syncBaseVMState(vm)
		if liveState != "" {
			vm.State = models.VMState(liveState)
		}
	}

	disks, err := h.DiskRepo.FindAll()
	if err != nil {
		respondErr(w, 500, "Error cargando discos: "+err.Error())
		return
	}

	userVMs, err := h.UserVMRepo.FindAll()
	if err != nil {
		respondErr(w, 500, "Error cargando VMs de usuario: "+err.Error())
		return
	}
	// Sincronizar User VMs
	for _, vm := range userVMs {
		liveState, _ := h.syncUserVMState(vm)
		if liveState != "" {
			vm.State = models.VMState(liveState)
		}
	}

	respond(w, 200, map[string]any{
		"base_vms": baseVMs,
		"disks":    disks,
		"user_vms": userVMs,
	})
}

func (h *Handler) syncBaseVMState(vm *models.BaseVM) (string, error) {
	idOrName := vm.VBoxUUID
	if idOrName == "" {
		idOrName = vm.Name
	}

	state, err := h.VBox.VMState(idOrName)
	if err != nil {
		return "", err
	}

	// Si no teníamos UUID, lo guardamos ahora para la posteridad
	if vm.VBoxUUID == "" {
		uuid, _ := h.VBox.GetVMUUID(vm.Name)
		if uuid != "" {
			_ = h.BaseVMRepo.SetVBoxUUID(vm.ID, uuid)
			vm.VBoxUUID = uuid
		}
	}

	if string(vm.State) != state {
		_ = h.BaseVMRepo.SetState(vm.ID, models.VMState(state))
	}
	return state, nil
}

func (h *Handler) syncUserVMState(vm *models.UserVM) (string, error) {
	idOrName := vm.VBoxUUID
	if idOrName == "" {
		idOrName = vm.Name
	}

	state, err := h.VBox.VMState(idOrName)
	if err != nil {
		return "", err
	}

	if string(vm.State) != state {
		_ = h.UserVMRepo.SetState(vm.ID, models.VMState(state))
	}
	return state, nil
}

// ─── Base VM handlers ─────────────────────────────────────────────────────────

// ListBaseVMs retorna todas las VMs base.
func (h *Handler) ListBaseVMs(w http.ResponseWriter, r *http.Request) {
	vms, err := h.BaseVMRepo.FindAll()
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, 200, vms)
}

// CreateBaseVM agrega una nueva VM base al sistema.
func (h *Handler) CreateBaseVM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "JSON inválido")
		return
	}
	if req.Name == "" || req.Password == "" {
		respondErr(w, 400, "El nombre y la contraseña son obligatorios")
		return
	}
	vm, err := h.BaseVMRepo.Create(req.Name, req.Description, req.Password)
	if err != nil {
		respondErr(w, 500, "No se pudo crear la VM base: "+err.Error())
		return
	}
	respond(w, 201, vm)
}

// DeleteBaseVM elimina una VM base y todo lo asociado (discos y clones).
func (h *Handler) DeleteBaseVM(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	vm, err := h.BaseVMRepo.FindByID(id)
	if err != nil {
		respondErr(w, 404, "VM base no encontrada")
		return
	}

	// Verificar contraseña
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Printf("DEBUG: Error decodificando body DELETE: %v\n", err)
	}

	fmt.Printf("DEBUG: Intento de borrado BaseVM ID %d. Recibido: '%s', En BD: '%s'\n", id, req.Password, vm.DeletionPassword)

	if req.Password == "" || vm.DeletionPassword != req.Password {
		respondErr(w, 403, "La contraseña de la máquina es incorrecta")
		return
	}

	// Eliminar de VirtualBox si tiene UUID
	if vm.VBoxUUID != "" {
		_ = h.VBox.PowerOffVM(vm.VBoxUUID)
		_ = h.VBox.DeleteVM(vm.VBoxUUID)
	}

	if err := h.BaseVMRepo.Delete(id); err != nil {
		respondErr(w, 500, "Error eliminando de BD: "+err.Error())
		return
	}

	respond(w, 200, map[string]string{"message": "VM base y recursos asociados eliminados"})
}

// CreateRootKeys genera el par de llaves RSA para root en la VM base especificada.
// Devuelve la clave pública; la privada se almacena en BD y se puede descargar aparte.
func (h *Handler) CreateRootKeys(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	vm, err := h.BaseVMRepo.FindByID(id)
	if err != nil {
		respondErr(w, 404, "VM base no encontrada")
		return
	}
	if vm.HasRootKeys {
		respondErr(w, 409, "Esta VM ya tiene llaves de root generadas")
		return
	}

	keyPair, err := h.SSH.GenerateRSAKeyPair()
	if err != nil {
		respondErr(w, 500, "Error generando llaves: "+err.Error())
		return
	}

	if err := h.BaseVMRepo.SetRootKeys(id, keyPair.PrivateKeyPEM, keyPair.PublicKey); err != nil {
		respondErr(w, 500, "Error guardando llaves: "+err.Error())
		return
	}

	respond(w, 200, map[string]string{
		"message":    "Llaves de root generadas correctamente",
		"public_key": keyPair.PublicKey,
	})
}

// InstallRootKey instala la llave pública en la VM usando la contraseña de root.
func (h *Handler) InstallRootKey(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "JSON inválido")
		return
	}

	_, pubKey, err := h.BaseVMRepo.GetRootKeys(id)
	if err != nil {
		respondErr(w, 404, "Llaves no encontradas")
		return
	}

	if err := h.SSH.InstallPublicKeyWithPassword(req.Host, req.Port, "root", req.Password, pubKey); err != nil {
		// Logueamos el error real para debugging
		fmt.Printf("Error instalando llave: %v\n", err)
		
		// Respondemos con un mensaje bien resumido y claro para el usuario
		respondErr(w, http.StatusUnauthorized, "No se pudo conectar. Verifique que la VM esté encendida y que el usuario 'root' admita conexión con contraseña.")
		return
	}

	respond(w, 200, map[string]string{"message": "¡Llave instalada con éxito!"})
}

// DownloadRootKey descarga la clave privada de root como archivo PEM.
func (h *Handler) DownloadRootKey(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	privKey, _, err := h.BaseVMRepo.GetRootKeys(id)
	if err != nil || len(privKey) == 0 {
		respondErr(w, 404, "Llaves no encontradas — genéralas primero")
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="root_key_%d.pem"`, id))
	w.Write(privKey)
}

// ─── Disk handlers ────────────────────────────────────────────────────────────

// CreateDisk crea un disco multiconexión para una VM base.
// Solo se permite si la VM ya tiene llaves de root configuradas.
func (h *Handler) CreateDisk(w http.ResponseWriter, r *http.Request) {
	vmID, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	vm, err := h.BaseVMRepo.FindByID(vmID)
	if err != nil {
		respondErr(w, 404, "VM base no encontrada")
		return
	}
	if !vm.HasRootKeys {
		respondErr(w, 422, "Debes crear las llaves de root antes de crear un disco")
		return
	}

	var req struct {
		Name   string `json:"name"`
		SizeMB int    `json:"size_mb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "JSON inválido")
		return
	}
	if req.Name == "" {
		respondErr(w, 400, "El nombre es obligatorio")
		return
	}
	if req.SizeMB <= 0 {
		req.SizeMB = 20480 // 20 GB por defecto
	}

	// BUSCAMOS EL DISCO DE LA VM BASE (No creamos nada nuevo, usamos el original)
	sourceDisk, _ := h.VBox.GetVMDiskPath(vm.VBoxUUID)
	if sourceDisk == "" {
		sourceDisk, _ = h.VBox.GetVMDiskPath(vm.Name)
	}

	if sourceDisk == "" {
		respondErr(w, 404, "No pudimos encontrar el disco rígido de tu VM base. Asegurate de que tenga un disco .vdi conectado en VirtualBox.")
		return
	}

	// Ponemos el disco original en modo multiattach (ahora con lógica Ninja anti-bloqueo)
	if err := h.VBox.CreateMultiattachDisk(vm.Name, sourceDisk); err != nil {
		respondErr(w, 500, "Error configurando disco multiconexión: "+err.Error())
		return
	}

	// Registramos este disco en la BD como disponible para crear VMs de usuario
	disk, err := h.DiskRepo.Create(vmID, req.Name, sourceDisk)
	if err != nil {
		respondErr(w, 500, "Error registrando disco en BD: "+err.Error())
		return
	}

	// Marcar como listo (llaves ya existen en la VM base)
	_ = h.DiskRepo.SetState(disk.ID, models.DiskStateReady)
	disk.State = models.DiskStateReady

	respond(w, 201, disk)
}

// DeleteDisk elimina el disco de VirtualBox y de la BD.
func (h *Handler) DeleteDisk(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	disk, err := h.DiskRepo.FindByID(id)
	if err != nil {
		respondErr(w, 404, "Disco no encontrado")
		return
	}
	if disk.State == models.DiskStateAttached {
		respondErr(w, 422, "No se puede eliminar un disco conectado a una VM")
		return
	}

	if err := h.VBox.DeleteDisk(disk.FilePath); err != nil {
		respondErr(w, 500, "Error eliminando disco de VirtualBox: "+err.Error())
		return
	}
	if err := h.DiskRepo.Delete(id); err != nil {
		respondErr(w, 500, "Error eliminando disco de BD: "+err.Error())
		return
	}
	respond(w, 200, map[string]string{"message": "Disco eliminado"})
}

// ─── User VM handlers ─────────────────────────────────────────────────────────

// CreateUserVM crea una VM de usuario a partir de un disco multiconexión.
func (h *Handler) CreateUserVM(w http.ResponseWriter, r *http.Request) {
	diskID, err := paramInt(r, "diskId")
	if err != nil {
		respondErr(w, 400, "diskId inválido")
		return
	}

	disk, err := h.DiskRepo.FindByID(diskID)
	if err != nil {
		respondErr(w, 404, "Disco no encontrado")
		return
	}
	if disk.State == models.DiskStateNoKeys {
		respondErr(w, 422, "El disco no tiene llaves configuradas")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		RamMB       int    `json:"ram_mb"`
		CPUs        int    `json:"cpus"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "JSON inválido")
		return
	}
	if req.Name == "" || req.Password == "" {
		respondErr(w, 400, "El nombre y la contraseña son obligatorios")
		return
	}
	if req.RamMB <= 0 {
		req.RamMB = 1024
	}
	if req.CPUs <= 0 {
		req.CPUs = 1
	}

	// Obtener la info del OS de la VM base para pasarla a VirtualBox
	baseVM, err := h.BaseVMRepo.FindByID(disk.BaseVMID)
	if err != nil {
		respondErr(w, 500, "VM base no encontrada")
		return
	}

	uuid, err := h.VBox.CreateUserVM(req.Name, "Debian_64", disk.FilePath, req.RamMB, req.CPUs)
	if err != nil {
		respondErr(w, 500, "Error creando VM en VirtualBox: "+err.Error())
		return
	}
	_ = baseVM // usado para logging/trazabilidad

	userVM, err := h.UserVMRepo.Create(diskID, req.Name, req.Description, req.Password)
	if err != nil {
		respondErr(w, 500, "Error registrando VM en BD: "+err.Error())
		return
	}

	_ = h.UserVMRepo.SetVBoxUUID(userVM.ID, uuid, 0)
	userVM.VBoxUUID = uuid

	respond(w, 201, userVM)
}

// CreateUserAccount crea un usuario en el SO de la VM y genera sus llaves SSH.
func (h *Handler) CreateUserAccount(w http.ResponseWriter, r *http.Request) {
	vmID, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	var req struct {
		Username string `json:"username"`
		VMHost   string `json:"vm_host"` // IP o localhost si es NAT
		VMPort   int    `json:"vm_port"` // puerto SSH (NAT forwarding)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "JSON inválido")
		return
	}
	if req.Username == "" || req.VMHost == "" {
		respondErr(w, 400, "username y vm_host son obligatorios")
		return
	}
	if req.VMPort == 0 {
		req.VMPort = 22
	}

	userVM, err := h.UserVMRepo.FindByID(vmID)
	if err != nil {
		respondErr(w, 404, "VM de usuario no encontrada")
		return
	}

	// Obtener llave root de la VM base
	disk, err := h.DiskRepo.FindByID(userVM.DiskID)
	if err != nil {
		respondErr(w, 500, "Disco no encontrado")
		return
	}
	rootPrivKey, _, err := h.BaseVMRepo.GetRootKeys(disk.BaseVMID)
	if err != nil {
		respondErr(w, 500, "Llaves root no encontradas")
		return
	}

	// Generar par de llaves para el usuario
	userKeyPair, err := h.SSH.GenerateRSAKeyPair()
	if err != nil {
		respondErr(w, 500, "Error generando llaves de usuario: "+err.Error())
		return
	}

	// Crear usuario en el SO remoto
	if err := h.SSH.CreateUser(req.VMHost, req.VMPort, rootPrivKey, req.Username, userKeyPair.PublicKey); err != nil {
		respondErr(w, 500, "Error creando usuario en VM: "+err.Error())
		return
	}

	if err := h.UserVMRepo.SetUserKeys(vmID, req.Username, userKeyPair.PrivateKeyPEM, userKeyPair.PublicKey); err != nil {
		respondErr(w, 500, "Error guardando llaves en BD: "+err.Error())
		return
	}

	respond(w, 200, map[string]string{
		"message":    fmt.Sprintf("Usuario '%s' creado correctamente", req.Username),
		"public_key": userKeyPair.PublicKey,
	})
}

// DownloadUserKey descarga la clave privada del usuario como archivo PEM.
func (h *Handler) DownloadUserKey(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	privKey, _, err := h.UserVMRepo.GetUserKeys(id)
	if err != nil || len(privKey) == 0 {
		respondErr(w, 404, "Llaves no encontradas — crea el usuario primero")
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="user_key_%d.pem"`, id))
	w.Write(privKey)
}

// DeleteUserVM elimina la VM de usuario de VirtualBox y de la BD.
func (h *Handler) DeleteUserVM(w http.ResponseWriter, r *http.Request) {
	id, err := paramInt(r, "id")
	if err != nil {
		respondErr(w, 400, "ID inválido")
		return
	}

	userVM, err := h.UserVMRepo.FindByID(id)
	if err != nil {
		respondErr(w, 404, "VM no encontrada")
		return
	}



	if userVM.VBoxUUID != "" {
		_ = h.VBox.PowerOffVM(userVM.VBoxUUID)
		if err := h.VBox.DeleteVM(userVM.VBoxUUID); err != nil {
			respondErr(w, 500, "Error eliminando VM de VirtualBox: "+err.Error())
			return
		}
	}

	if err := h.UserVMRepo.Delete(id); err != nil {
		respondErr(w, 500, "Error eliminando VM de BD: "+err.Error())
		return
	}

	respond(w, 200, map[string]string{"message": "VM de usuario eliminada"})
}
