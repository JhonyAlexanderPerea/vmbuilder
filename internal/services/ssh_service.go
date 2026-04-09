package services

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/uq/vm-platform/internal/models"
	"golang.org/x/crypto/ssh"
)

// SSHService maneja la generación de llaves RSA y operaciones remotas.
type SSHService struct{}

func NewSSHService() *SSHService {
	return &SSHService{}
}

// ─── Key generation ───────────────────────────────────────────────────────────

// GenerateRSAKeyPair genera un par de llaves RSA de 1024 bits (requerido por el enunciado).
func (s *SSHService) GenerateRSAKeyPair() (*models.SSHKeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, fmt.Errorf("generar llave RSA: %w", err)
	}

	// Serializar clave privada a PEM
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	})

	// Generar clave pública en formato authorized_keys
	pubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("generar clave pública SSH: %w", err)
	}

	return &models.SSHKeyPair{
		PrivateKeyPEM: privPEM,
		PublicKey:     string(ssh.MarshalAuthorizedKey(pubKey)),
	}, nil
}

// ─── Remote operations ────────────────────────────────────────────────────────

// sshClient abre una conexión SSH usando la clave privada PEM.
func (s *SSHService) sshClient(host string, port int, user string, privKeyPEM []byte) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey(privKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsear clave privada: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // entorno de laboratorio
		Timeout:         30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("conexión SSH a %s: %w", addr, err)
	}
	return client, nil
}

// RunCommand ejecuta un comando en el host remoto vía SSH y devuelve stdout+stderr.
func (s *SSHService) RunCommand(host string, port int, user string, privKeyPEM []byte, cmd string) (string, error) {
	client, err := s.sshClient(host, port, user, privKeyPEM)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("abrir sesión SSH: %w", err)
	}
	defer session.Close()

	var out bytes.Buffer
	session.Stdout = &out
	session.Stderr = &out

	if err := session.Run(cmd); err != nil {
		return out.String(), fmt.Errorf("comando '%s' falló: %w — %s", cmd, err, out.String())
	}
	return out.String(), nil
}

// ─── Root key installation ────────────────────────────────────────────────────

// InstallRootPublicKey instala la clave pública en /root/.ssh/authorized_keys
// usando la contraseña de root (solo primera vez, luego se usa la llave).
// En un entorno real usarías expect o sshpass; aquí lo hacemos por llave si ya existe.
func (s *SSHService) InstallRootPublicKey(host string, port int, rootPrivKeyPEM []byte, pubKey string) error {
	commands := []string{
		"mkdir -p /root/.ssh && chmod 700 /root/.ssh",
		fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys", pubKey),
		"chmod 600 /root/.ssh/authorized_keys",
	}

	for _, cmd := range commands {
		if _, err := s.RunCommand(host, port, "root", rootPrivKeyPEM, cmd); err != nil {
			return fmt.Errorf("instalar llave root: %w", err)
		}
	}
	return nil
}

// ─── User management (sin interacción) ───────────────────────────────────────

// CreateUser crea un usuario en el SO remoto e instala sus llaves SSH.
// Se ejecuta como root usando la llave de root ya instalada.
func (s *SSHService) CreateUser(host string, port int, rootPrivKeyPEM []byte, username string, userPubKey string) error {
	commands := []string{
		// Crear usuario sin contraseña (solo llave SSH)
		fmt.Sprintf("id -u %s || useradd -m -s /bin/bash %s", username, username),
		fmt.Sprintf("mkdir -p /home/%s/.ssh && chmod 700 /home/%s/.ssh", username, username),
		fmt.Sprintf("echo '%s' > /home/%s/.ssh/authorized_keys", userPubKey, username),
		fmt.Sprintf("chmod 600 /home/%s/.ssh/authorized_keys", username),
		fmt.Sprintf("chown -R %s:%s /home/%s/.ssh", username, username, username),
	}

	for _, cmd := range commands {
		if _, err := s.RunCommand(host, port, "root", rootPrivKeyPEM, cmd); err != nil {
			return fmt.Errorf("crear usuario '%s': %w", username, err)
		}
	}
	return nil
}

// DeleteUser elimina el usuario del SO remoto.
func (s *SSHService) DeleteUser(host string, port int, rootPrivKeyPEM []byte, username string) error {
	cmd := fmt.Sprintf("userdel -r %s 2>/dev/null || true", username)
	_, err := s.RunCommand(host, port, "root", rootPrivKeyPEM, cmd)
	return err
}

// WaitForSSH espera hasta que el servidor SSH del host esté disponible (máx attempts).
func (s *SSHService) WaitForSSH(host string, port int, attempts int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	for i := 0; i < attempts; i++ {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("SSH no disponible en %s tras %d intentos", addr, attempts)
}
