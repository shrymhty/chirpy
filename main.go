package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

type apiConfig struct {
	fileServerHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	apiCgf := &apiConfig{}

	// serve static files which are rpesent in the directory
	fileHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))
	mux.Handle("/app/", apiCgf.middlewareMetricsInc(fileHandler))
	mux.Handle("/assets", http.FileServer(http.Dir("/assets")))

	// serve api endpoints
	mux.HandleFunc("GET /api/healthz", checkReadiness)
	mux.HandleFunc("GET /admin/metrics", apiCgf.handleMetrics)
	mux.HandleFunc("POST /admin/reset", apiCgf.resetMetrics)

	server := http.Server{
		Handler: mux,
		Addr: ":8080",
	}

	err := server.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}

}

func checkReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileServerHits.Add(1)
		next.ServeHTTP(w, r)
	})  
}

func (cfg *apiConfig) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html;charset=utf-8")

	hits := cfg.fileServerHits.Load()

	htmlContent := fmt.Sprintf(`
	<html>
		<body>
			<h1>Welcome, Chirpy Admin</h1>
			<p>Chirpy has been visited %d times!</p>
		</body>
	</html>
	`, hits)

	w.Write([]byte(htmlContent))
}	

func (cfg *apiConfig) resetMetrics(w http.ResponseWriter, r *http.Request) {
	cfg.fileServerHits.Store(0)
	w.Write([]byte("Hits reset to 0"))
}
