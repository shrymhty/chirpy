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
	"github.com/shrymhty/chirpy/internal/auth"
	"github.com/shrymhty/chirpy/internal/database"
)

type apiConfig struct {
	fileServerHits atomic.Int32
	db *database.Queries
	env string
	secret string
}

type chirpResp struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body string `json:"body"`
	UserID uuid.UUID `json:"user_id"`
}

type userResponse struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email string `json:"email"`
}

func main() {
	// .env file to environment variables
	godotenv.Load()

	dbUrl := os.Getenv("DB_URL")
	envPlat := os.Getenv("PLATFORM")
	jwtSecret := os.Getenv("JWT_SECRET")
	db, err := sql.Open("postgres", dbUrl)
	if err != nil {
		log.Fatal(err)
	}

	dbQueries := database.New(db)

	mux := http.NewServeMux()
	apiCgf := &apiConfig{
		db: dbQueries,
		env: envPlat,
		secret: jwtSecret,
	}


	// serve static files which are rpesent in the directory
	fileHandler := http.StripPrefix("/app/", http.FileServer(http.Dir(".")))
	mux.Handle("/app/", apiCgf.middlewareMetricsInc(fileHandler))
	mux.Handle("/assets", http.FileServer(http.Dir("/assets")))

	// serve api endpoints
	mux.HandleFunc("GET /api/healthz", checkReadiness)
	mux.HandleFunc("POST /api/users", apiCgf.createUser)
	mux.HandleFunc("POST /api/chirps", apiCgf.chirpHandler)
	mux.HandleFunc("POST /api/login", apiCgf.userLogin)
	mux.HandleFunc("GET /api/chirps", apiCgf.getChirps)
	mux.HandleFunc("GET /api/chirps/{chirpid}", apiCgf.getChirpById)
	mux.HandleFunc("POST /api/refresh", apiCgf.handleRefreshTokens)
	mux.HandleFunc("POST /api/revoke", apiCgf.revokeTokens)

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
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	var params userParams
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	hashedPass, err := auth.HashPassword(params.Password)
	if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Could not hash password")
        return
    }

	user, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Email: params.Email,
		HashedPassword: hashedPass,
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

func (cfg *apiConfig) chirpHandler(w http.ResponseWriter, r *http.Request) {
	type jsonPayload struct {
		Body string `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	var params jsonPayload
	err = json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}	

	if len(params.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleanedBody := cleanBody(params.Body)

	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Body: cleanedBody,
		UserID: userID,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, chirpResp{
		ID: chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body: chirp.Body,
		UserID: chirp.UserID,
	})
}

func (cfg *apiConfig) getChirps(w http.ResponseWriter, r *http.Request) {

	dbChirps, err := cfg.db.GetChirps(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	chirps := []chirpResp{}

	for _, dbChirps := range dbChirps {
		chirps = append(chirps, chirpResp{
			ID: dbChirps.ID,
			CreatedAt: dbChirps.CreatedAt,
			UpdatedAt: dbChirps.UpdatedAt,
			Body: dbChirps.Body,
			UserID: dbChirps.UserID,
		})
	}

	respondWithJSON(w, http.StatusOK, chirps)
}

func (cfg *apiConfig) getChirpById(w http.ResponseWriter, r *http.Request) {
	chirpIDstr := r.PathValue("chirpid")
	parsedID, err := uuid.Parse(chirpIDstr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Incorrect id passed")
	}

	chirp, err := cfg.db.GetChirpByID(r.Context(), parsedID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, chirpResp{
		ID: chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body: chirp.Body,
			UserID: chirp.UserID,
	})
}

func (cfg *apiConfig) userLogin(w http.ResponseWriter, r *http.Request) {
	type userParams struct {
		Email string `json:"email"`
		Password string `json:"password"`
	}

	type LoginResponse struct {
		ID uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email string `json:"email"`
		Token string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}

	var params userParams

	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user, err := cfg.db.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	match, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Internal server error")
        return
    }
	if !match {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}
	
	expiresIn := time.Hour
	token, err := auth.MakeJWT(user.ID, cfg.secret, expiresIn)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not generate token")
		return
	}

	refreshToken := auth.MakeRefreshToken()
	_, err = cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams {
		Token: refreshToken,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID: user.ID,
		ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not save refresh token")
		return
	}

	respondWithJSON(w, http.StatusOK, LoginResponse{
		ID: user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email: user.Email,
		Token: token,
		RefreshToken: refreshToken,
	})

}

func (cfg *apiConfig) handleRefreshTokens(w http.ResponseWriter, r *http.Request) {

	type response struct {
		Token string `json:"token"`
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	refreshToken, err := cfg.db.GetRefreshTokenByToken(r.Context(), token)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	if refreshToken.RevokedAt.Valid {
        respondWithError(w, http.StatusUnauthorized, "Token revoked")
        return
    }

	if time.Now().UTC().After(refreshToken.ExpiresAt) {
        respondWithError(w, http.StatusUnauthorized, "Token expired")
        return
    }

	accessToken, err := auth.MakeJWT(refreshToken.UserID, cfg.secret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not generate token")
        return
	}

	respondWithJSON(w, http.StatusOK, response{
		Token: accessToken,
	})

}

func (cfg *apiConfig) revokeTokens(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	err = cfg.db.RevokeRefreshToken(r.Context(), database.RevokeRefreshTokenParams{
		RevokedAt: sql.NullTime{
            Time:  time.Now().UTC(),
            Valid: true,
        },
		UpdatedAt: time.Now().UTC(),
		Token: token,
	})
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unable to revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}