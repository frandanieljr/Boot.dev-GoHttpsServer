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

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"MODULE_PATH/internal/database"

	"time"

	"github.com/google/uuid"
        
	"MODULE_PATH/internal/auth" 	
)

type apiConfig struct {
	fileserverHits atomic.Int32
	DB             *database.Queries
	platform	string
	jwtSecret	string
}

//Struct do usuario
type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}


//Struct do Chirp
type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func main() {

	mux := http.NewServeMux()

	//Carrega as variáveis do arquivo.env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Aviso: arquivo .env não encontrado")
	}

	//Obtém a variável platform
	platform := os.Getenv("PLATFORM")

	//Obtém jwt secret do .env
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is not set")
	}

	//Obtém a URL do banco
	dbURL := os.Getenv("DB_URL")

	//Abre conexão com o banco
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Erro ao abrir conexão com o banco: %v", err)
	}

	//Criar o dbQueries PRIMEIRO
	dbQueries := database.New(db)

	//Atribuir o dbQueries à struct apiConfig DEPOIS
	apiCfg := apiConfig{
		DB: dbQueries,
		platform: platform,
		jwtSecret: jwtSecret,
	}

	// Adiciona o prefixo /api às rotas que não são do fileserver:
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Handler do FileServer original
	fsHandler := http.StripPrefix("/app", http.FileServer(http.Dir(".")))

	// Envolver o handler do FileServer com o middleware
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fsHandler))

	// Registar a rota /metrics
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerMetrics)

	// Registar a rota /reset
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)

	// Registrar a nova rota de criação de usuários
        mux.HandleFunc("POST /api/users", apiCfg.handlerUsersCreate)

	// Novo Endpoint: /api/validate_chirp
	//mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)

	// Novo endpoint chirps create
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerChirpsCreate)
	
	// Novo endpoint para listar os chirps
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerChirpsRetrieve)

	// Registar o endpoint para obter um chirp pelo ID
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handlerChirpsGet)

	// Registrar a rota de login
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)

	//registrar a rota de refresh
	mux.HandleFunc("POST /api/refresh", apiCfg.handlerRefresh)

	//registrar rota de revoke
	mux.HandleFunc("POST /api/revoke", apiCfg.handlerRevoke)

	// Iniciar o servidor na porta 8080
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	// Chamar ListenAndServe() para o servidor arrancar e a variável 'server' ser usada
	server.ListenAndServe()
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// Handler modificado para devolver HTML
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	html := fmt.Sprintf(`<html>
				<body>
    				<h1>Welcome, Chirpy Admin</h1>
    				<p>Chirpy has been visited %d times!</p>
				</body>
			     </html>`, cfg.fileserverHits.Load())

	w.Write([]byte(html))
}


func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {


	// Verifica se está em ambiente dev
	if cfg.platform != "dev" {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Reset is only allowed in dev environment"))
		return
	}

	// Reseta os hits de métricas
	cfg.fileserverHits.Store(0)

	// Deleta todos os usuários da base de dados
	err := cfg.DB.DeleteUsers(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete users")
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hits reset to 0 and database cleared"))
}

// Handler da validação e limpeza de Chirp
func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	// Atualiza esta struct para ter o campo CleanedBody
	type returnVal struct {
		CleanedBody string `json:"cleaned_body"`
	}

	type returnError struct {
		Error string `json:"error"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}

	const maxChirpLength = 140
	if len(params.Body) > maxChirpLength {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	// Limpar o texto do chirp substituindo as palavras profanas
	cleaned := getCleanedBody(params.Body)

	respondWithJSON(w, http.StatusOK, returnVal{
		CleanedBody: cleaned,
	})
}

// Funções auxiliares para responder em JSON
func respondWithError(w http.ResponseWriter, code int, msg string) {
	type returnError struct {
		Error string `json:"error"`
	}
	respondWithJSON(w, code, returnError{
		Error: msg,
	})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

// Função auxiliar para substituir palavras proibidas por ****
func getCleanedBody(body string) string {
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	words := strings.Split(body, " ")
	for i, word := range words {
		loweredWord := strings.ToLower(word)
		if _, exists := badWords[loweredWord]; exists {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}


//Handler para criar usuário
func (cfg *apiConfig) handlerUsersCreate(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email string `json:"email"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
        

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password")
		return
	}
        
	user, err := cfg.DB.CreateUser(r.Context(), database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}

	respondWithJSON(w, http.StatusCreated, User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})
}


//Handler para criar chirp no banco
func (cfg *apiConfig) handlerChirpsCreate(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body   string    `json:"body"`
		
	}

	// 1. Extrair e validar o Token do cabeçalho HTTP
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing or malformed token")
		return
	}

	userID, err := auth.ValidateJWT(tokenString, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	// 2. Fazer o parse do corpo da requisição
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validação de tamanho do Chirp
	const maxChirpLength = 140
	if len(params.Body) > maxChirpLength {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	// Limpeza de palavras profanas
	cleanedBody := getCleanedBody(params.Body)

	// Persistência no Banco de Dados
	chirp, err := cfg.DB.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: userID,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create chirp")
		return
	}

	// Retorno da resposta com status 201 Created
	respondWithJSON(w, http.StatusCreated, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

//Handler para retornar chirps do banco
func (cfg *apiConfig) handlerChirpsRetrieve(w http.ResponseWriter, r *http.Request) {
	dbChirps, err := cfg.DB.GetChirps(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't retrieve chirps")
		return
	}

	chirps := []Chirp{}
	for _, dbChirp := range dbChirps {
		chirps = append(chirps, Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body:      dbChirp.Body,
			UserID:    dbChirp.UserID,
		})
	}

	respondWithJSON(w, http.StatusOK, chirps)
}

func (cfg *apiConfig) handlerChirpsGet(w http.ResponseWriter, r *http.Request) {
	chirpIDString := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID")
		return
	}

	dbChirp, err := cfg.DB.GetChirp(r.Context(), chirpID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}

	respondWithJSON(w, http.StatusOK, Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	})
}

func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Password         string `json:"password"`
		Email            string `json:"email"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// 1. Procurar o utilizador na base de dados PRIMEIRO
	user, err := cfg.DB.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	// 2. Verificar a password
	match, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil || !match {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	// 1. Gerar JWT (Acesso) com expiração fixa de 1 hora
	accessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create access token")
		return
	}

	// 2. Gerar Refresh Token
	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create refresh token")
		return
	}

	// 3. Guardar Refresh Token na base de dados (expira em 60 dias)
	_, err = cfg.DB.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		Token:     refreshToken,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour * 24 * 60),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't save refresh token")
		return
	}


	// struct de resposta do token
	type response struct {
		User
		Token 		string `json:"token"`
		RefreshToken 	string `json:"refresh_token"`
	}

	// 5. Enviar a resposta final 
	respondWithJSON(w, http.StatusOK, response{
		User: User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		},
		Token: accessToken,
		RefreshToken: refreshToken,
	})
}


func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, r *http.Request) {
	// Extrair o token do cabeçalho usando a função que já criou no exercício anterior
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find token")
		return
	}

	// Procurar o utilizador associado a este token na DB
	user, err := cfg.DB.GetUserFromRefreshToken(r.Context(), refreshToken)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid, expired or revoked refresh token")
		return
	}

	// Gerar um novo access token (JWT) válido por 1 hora
	accessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create access token")
		return
	}

	// Retornar apenas o novo access token
	type response struct {
		Token string `json:"token"`
	}
	respondWithJSON(w, http.StatusOK, response{
		Token: accessToken,
	})
}

func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find token")
		return
	}

	err = cfg.DB.RevokeRefreshToken(r.Context(), refreshToken)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
