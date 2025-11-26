package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	metrograph "github.com/dangxcx/metrograph-watchlist/pkg"
	"go.yaml.in/yaml/v4"
)

type Config struct {
	TMDB struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"tmdb"`
	Radarr struct {
		Host             string `yaml:"host"`
		APIKey           string `yaml:"api_key"`
		RootFolderPath   string `yaml:"root_folder_path"`
		QualityProfileID int    `yaml:"quality_profile_id"`
		Monitored        bool   `yaml:"monitored"`
		SearchForMovie   bool   `yaml:"search_for_movie"`
	} `yaml:"radarr"`
	Agregarr struct {
		Host   string `yaml:"host"`
		APIKey string `yaml:"api_key"`
	} `yaml:"agregarr"`
	Settings struct {
		RateLimitMs int  `yaml:"rate_limit_ms"`
		Debug       bool `yaml:"debug"`
	} `yaml:"settings"`
}

func loadConfig() (*Config, error) {
	config := &Config{}

	// Try to read config.yaml
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read config.yaml: %v", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config.yaml: %v", err)
	}

	return config, nil
}

func main() {
	args := os.Args[1:]

	// Load config from file
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		fmt.Println("Falling back to environment variable...")

		// Fallback to environment variable
		tmdbAPIKey := os.Getenv("TMDB_API_KEY")
		if tmdbAPIKey == "" {
			fmt.Println("Warning: No TMDB API key found. Movie IDs will not be fetched.")
		}
		config = &Config{}
		config.TMDB.APIKey = tmdbAPIKey
	}

	// Check for command line commands
	if len(args) > 0 {
		switch args[0] {
		case "radarr":
			if len(args) < 2 {
				log.Fatal("Usage: go run main.go radarr <json-file>")
			}

			jsonFile := args[1]
			if config.Radarr.APIKey == "" || config.Radarr.Host == "" {
				log.Fatal("Radarr configuration missing in config.yaml")
			}

			radarrConfig := metrograph.RadarrConfig{
				Host:             config.Radarr.Host,
				APIKey:           config.Radarr.APIKey,
				RootFolderPath:   config.Radarr.RootFolderPath,
				QualityProfileID: config.Radarr.QualityProfileID,
				Monitored:        config.Radarr.Monitored,
				SearchForMovie:   config.Radarr.SearchForMovie,
			}

			err := metrograph.ProcessJSONToRadarr(jsonFile, radarrConfig)
			if err != nil {
				log.Fatal(err)
			}
			return

		case "profiles":
			if config.Radarr.APIKey == "" || config.Radarr.Host == "" {
				log.Fatal("Radarr configuration missing in config.yaml")
			}

			radarrConfig := metrograph.RadarrConfig{
				Host:   config.Radarr.Host,
				APIKey: config.Radarr.APIKey,
			}

			err := metrograph.ListRadarrProfiles(radarrConfig)
			if err != nil {
				log.Fatal(err)
			}
			return

		case "collections":
			if len(args) < 2 {
				log.Fatal("Usage: go run main.go collections <json-file>")
			}

			jsonFile := args[1]
			if config.Agregarr.APIKey == "" || config.Agregarr.Host == "" {
				log.Fatal("Agregarr configuration missing in config.yaml")
			}

			radarrConfig := metrograph.RadarrConfig{
				Host:   config.Radarr.Host,
				APIKey: config.Radarr.APIKey,
			}

			agregarrConfig := metrograph.AgregarrConfig{
				Host:   config.Agregarr.Host,
				APIKey: config.Agregarr.APIKey,
			}

			err := metrograph.CreateCollectionsFromJSON(jsonFile, radarrConfig, agregarrConfig)
			if err != nil {
				log.Fatal(err)
			}
			return

		case "test-agregarr":
			if config.Agregarr.APIKey == "" || config.Agregarr.Host == "" {
				log.Fatal("Agregarr configuration missing in config.yaml")
			}

			agregarrConfig := metrograph.AgregarrConfig{
				Host:   config.Agregarr.Host,
				APIKey: config.Agregarr.APIKey,
			}

			agregarrClient := metrograph.NewAgregarrClient(agregarrConfig)
			err := agregarrClient.TestConnection()
			if err != nil {
				log.Fatal(err)
			}
			return

		case "get-collections":
			if config.Agregarr.APIKey == "" || config.Agregarr.Host == "" {
				log.Fatal("Agregarr configuration missing in config.yaml")
			}

			agregarrConfig := metrograph.AgregarrConfig{
				Host:   config.Agregarr.Host,
				APIKey: config.Agregarr.APIKey,
			}

			agregarrClient := metrograph.NewAgregarrClient(agregarrConfig)
			collections, err := agregarrClient.GetCollections()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Found %d collections\n", len(collections))
			return

		default:
			log.Fatalf("Unknown command: %s\nAvailable commands: radarr, profiles, collections, test-agregarr", args[0])
		}
	}

	// Default behavior: scrape and generate JSON
	results, err := metrograph.Crawl(config.TMDB.APIKey)
	if err != nil {
		log.Fatal(err)
	}

	// Filter results to only include series with >2 valid movies for JSON output
	filteredResults := make(map[string]metrograph.Series)
	for seriesID, series := range results {
		validMovies := 0
		for _, movie := range series.Movies {
			if movie.TMDBID > 0 {
				validMovies++
			}
		}
		if validMovies > 2 {
			filteredResults[seriesID] = series
		}
	}

	// Generate filename with today's date
	today := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s.json", today)

	// Pretty print the JSON data
	jsonData, err := json.MarshalIndent(filteredResults, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	// Write to file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		log.Fatalf("Failed to write to %s: %v", filename, err)
	}

	fmt.Printf("Results written to %s\n", filename)
	fmt.Printf("Found %d total series, %d series with >2 valid movies\n", len(results), len(filteredResults))
}
