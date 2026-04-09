package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/uq/vm-platform/internal/api"
	"github.com/uq/vm-platform/internal/repository"
)

func main() {
	// Configuración mínima vía variables de entorno
	port := envOr("PORT", "8081")
	dbPath := envOr("DB_PATH", "vm_platform.db")

	// Inicializar BD
	db, err := repository.New(dbPath)
	if err != nil {
		log.Fatalf("Error inicializando base de datos: %v", err)
	}
	defer db.Close()

	// Crear router con todas las dependencias
	router := api.NewRouter(db)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("🚀 Servidor iniciado en http://localhost%s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Error iniciando servidor: %v", err)
	}
}

func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
