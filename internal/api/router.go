package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/uq/vm-platform/internal/api/handlers"
	"github.com/uq/vm-platform/internal/repository"
	"github.com/uq/vm-platform/internal/services"
)

func NewRouter(db *repository.DB) http.Handler {
	h := &handlers.Handler{
		BaseVMRepo: repository.NewBaseVMRepo(db),
		DiskRepo:   repository.NewDiskRepo(db),
		UserVMRepo: repository.NewUserVMRepo(db),
		VBox:       services.NewVBoxService(),
		SSH:        services.NewSSHService(),
	}

	r := chi.NewRouter()

	// ─── Middleware ──────────────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Minute))
	r.Use(corsMiddleware)

	// ─── Static / frontend ───────────────────────────────────────────────────
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	r.Get("/", serveIndex)

	// ─── API ─────────────────────────────────────────────────────────────────
	r.Route("/api", func(r chi.Router) {
		// Dashboard
		r.Get("/dashboard", h.GetDashboard)

		// VMs base
		r.Route("/base-vms", func(r chi.Router) {
			r.Get("/", h.ListBaseVMs)
			r.Post("/", h.CreateBaseVM)
			r.Route("/{id}", func(r chi.Router) {
				r.Post("/root-keys", h.CreateRootKeys)
				r.Post("/root-keys/install", h.InstallRootKey)
				r.Get("/root-keys/download", h.DownloadRootKey)
				r.Post("/disks", h.CreateDisk)
				r.Delete("/", h.DeleteBaseVM)
			})
		})

		// Discos
		r.Route("/disks", func(r chi.Router) {
			r.Delete("/{id}", h.DeleteDisk)
		})

		// VMs de usuario
		r.Route("/disks/{diskId}/user-vms", func(r chi.Router) {
			r.Post("/", h.CreateUserVM)
		})
		r.Route("/user-vms/{id}", func(r chi.Router) {
			r.Post("/user-account", h.CreateUserAccount)
			r.Get("/user-keys/download", h.DownloadUserKey)
			r.Delete("/", h.DeleteUserVM)
		})
	})

	return r
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/templates/index.html")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
