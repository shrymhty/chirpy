package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/shrymhty/chirpy/internal/database"
)

type apiConfig struct {
	fileServerHits atomic.Int32
	db *database.Queries
	env string
}

func main() {
	// .env file to environment variables
	godotenv.Load()

	dbUrl := os.Getenv("DB_URL")
	envPlat := os.Getenv("PLATFORM")
	db, err := sql.Open("postgres", dbUrl)
	if err != nil {
		log.Fatal(err)
	}

	dbQueries := database.New(db)

	mux := http.NewServeMux()
	apiCgf := &apiConfig{
		db: dbQueries,
		env: envPlat,
	}


	// serve static files which are rpesent in the directory
	fileHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))
	mux.Handle("/app/", apiCgf.middlewareMetricsInc(fileHandler))
	mux.Handle("/assets", http.FileServer(http.Dir("/assets")))

	// serve api endpoints
	mux.HandleFunc("GET /api/healthz", checkReadiness)
	mux.HandleFunc("POST /api/validate_chirp", validateChirps)
	mux.HandleFunc("POST /api/users", apiCgf.createUser)

	mux.HandleFunc("GET /admin/metrics", apiCgf.handleMetrics)
	mux.HandleFunc("POST /admin/reset", apiCgf.resetMetrics)

	server := http.Server{
		Handler: mux,
		Addr: ":8080",
	}

	err = server.ListenAndServe()
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
	if cfg.env != "dev" {
		respondWithError(w, http.StatusForbidden, fmt.Sprintf("Action prohibited for %s environment.", cfg.env))
		return
	}
	
	err := cfg.db.DeleteUsers(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfg.fileServerHits.Store(0)
	w.Write([]byte("Hits reset to 0"))
}


func validateChirps(w http.ResponseWriter, r *http.Request) {
	type returnVals struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	var vals returnVals
	err := decoder.Decode(&vals)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	} 

	if len(vals.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleanedBody := cleanBody(vals.Body)
	respondWithJSON(w, http.StatusOK, map[string]string{"cleaned_body":cleanedBody})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	type errorRes struct {
		Error string `json:"error"`
	}

	respondWithJSON(w, code, errorRes{Error: msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	dat, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	w.Write(dat)
}

func cleanBody(body string) string {
	words := strings.Split(body, " ")
	
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	for i, word := range words {
		lowered := strings.ToLower(word)
		if _, ok := badWords[lowered]; ok {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

func (cfg *apiConfig) createUser(w http.ResponseWriter, r *http.Request) {
	type userParams struct {
		Email string `json:"email"`
	}

	type userResponse struct {
		ID uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
	}

	decoder := json.NewDecoder(r.Body)
	var params userParams
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Email: params.Email,
	})

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, userResponse{
		ID: user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email: user.Email,
	})
}