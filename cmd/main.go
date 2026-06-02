package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"server-prewarm/internal/config"
	"server-prewarm/internal/db/database"
	"server-prewarm/internal/handlers"
	"server-prewarm/internal/manager"
)

func main() {
	log.Println("🚀 Starting Prewarm Server")

	// Load configuration
	config.Load()

	// Connect to MongoDB
	if err := database.Connect(); err != nil {
		log.Fatalf("❌ Failed to connect to MongoDB: %v", err)
	}
	defer database.Disconnect()

	port := config.AppConfig.Port

	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: corsMiddleware(mux),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("⏹️ Shutting down...")
		cancel()
		shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		server.Shutdown(shutdownCtx)
	}()

	go func() {
		time.Sleep(2 * time.Second)
		log.Println("🔥 Auto-starting prewarm...")
		manager.GetManager().StartPrewarm(ctx)
	}()

	log.Printf("🌐 Dashboard: http://localhost:%s/", port)
	log.Printf("📡 API: http://localhost:%s/api/status", port)
	log.Printf("🔌 WebSocket: ws://localhost:%s/ws", port)
	log.Printf("⚙️  Config: STORAGE_ID=%s, MAX_CONCURRENT=%d, PARALLEL=%d",
		config.AppConfig.StorageID, config.AppConfig.MaxConcurrent, config.AppConfig.Parallel)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("❌ Server error: %v", err)
	}
	log.Println("👋 Server stopped")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
