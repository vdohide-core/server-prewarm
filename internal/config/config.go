package config

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string // Support both PORT and HTTP_PORT
	MongoURI      string
	DBName        string
	StorageID     string
	PrewarmPOP    string
	MaxConcurrent int
	Parallel      int
	DomainContent string
	DomainStatic  string
	DomainPlayer  string
}

var AppConfig Config

func Load() {
	_ = godotenv.Load()

	mongoURI := getEnv("MONGODB_URI", "")
	if mongoURI == "" {
		mongoURI = getEnv("MONGO_URI", "")
	}
	if mongoURI == "" {
		mongoURI = getEnv("DATABASE_URL", "mongodb://localhost:27017/vdohide")
	}

	dbName := getEnv("DB_NAME", "")
	if dbName == "" {
		dbName = extractDBName(mongoURI)
		if dbName == "" {
			dbName = "vdohide"
		}
	}

	port := getEnv("HTTP_PORT", "")
	if port == "" {
		port = getEnv("PORT", "8886")
	}

	AppConfig = Config{
		Port:          port,
		MongoURI:      mongoURI,
		DBName:        dbName,
		StorageID:     getEnv("STORAGE_ID", ""),
		PrewarmPOP:    getEnv("PREWARM_POP", "auto"),
		MaxConcurrent: getEnvInt("MAX_CONCURRENT", 5),
		Parallel:      getEnvInt("PARALLEL", 20),
		DomainContent: getEnv("DOMAIN_CONTENT", ""),
		DomainStatic:  getEnv("DOMAIN_STATIC", ""),
		DomainPlayer:  getEnv("DOMAIN_PLAYER", ""),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func extractDBName(uri string) string {
	for _, prefix := range []string{"mongodb+srv://", "mongodb://"} {
		if len(uri) > len(prefix) && uri[:len(prefix)] == prefix {
			uri = uri[len(prefix):]
			break
		}
	}
	for i, c := range uri {
		if c == '?' {
			uri = uri[:i]
			break
		}
	}
	for i, c := range uri {
		if c == '/' {
			return uri[i+1:]
		}
	}
	return ""
}

// NeedAutoDetectPOP returns true if POP needs to be auto-detected
func NeedAutoDetectPOP() bool {
	return AppConfig.PrewarmPOP == "" || AppConfig.PrewarmPOP == "auto"
}

// SetPOP sets the detected POP
func SetPOP(pop string) {
	AppConfig.PrewarmPOP = pop
	log.Printf("🌍 POP set to: %s", pop)
}

// DetectPOP detects the Cloudflare POP by making HEAD requests to multiple storage URLs
// and returning the most frequently seen POP (handles routing inconsistencies)
func DetectPOP(urls []string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
			MaxIdleConns:        1,
			MaxIdleConnsPerHost: 1,
			DisableKeepAlives:   true, // force new connections for each request
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	const roundsPerURL = 2
	popCount := make(map[string]int)
	total := 0

	for _, url := range urls {
		for i := 0; i < roundsPerURL; i++ {
			total++
			pop := detectOnce(client, url)
			if pop != "" {
				popCount[pop]++
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	if len(popCount) == 0 {
		return "", nil
	}

	// Find most common POP
	bestPOP := ""
	bestCount := 0
	for pop, count := range popCount {
		if count > bestCount {
			bestPOP = pop
			bestCount = count
		}
	}

	log.Printf("   📊 POP votes: %v → winner: %s (%d/%d)", popCount, bestPOP, bestCount, total)
	return bestPOP, nil
}

// detectOnce makes a single HEAD request and returns the POP from CF-Ray
func detectOnce(client *http.Client, url string) string {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Prewarm/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	cfRay := resp.Header.Get("CF-Ray")
	if cfRay == "" {
		return ""
	}

	parts := strings.Split(cfRay, "-")
	if len(parts) > 1 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return ""
}
