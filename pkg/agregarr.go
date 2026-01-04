package metrograph

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type AgregarrConfig struct {
	Host   string
	APIKey string
}

type AgregarrClient struct {
	config     AgregarrConfig
	httpClient *http.Client
}

type VisibilityConfig struct {
	UsersHome          bool `json:"usersHome"`
	ServerOwnerHome    bool `json:"serverOwnerHome"`
	LibraryRecommended bool `json:"libraryRecommended"`
}

type Collection struct {
	// Required fields
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	VisibilityConfig VisibilityConfig `json:"visibilityConfig"`
	MaxItems         int              `json:"maxItems"`

	// Source configuration
	Type      string `json:"type,omitempty"`      // e.g. "radarr", "trakt", "tmdb"
	Subtype   string `json:"subtype,omitempty"`   // tag name for radarr, list ID for others
	MediaType string `json:"mediaType,omitempty"` // "movie", "tv", "both"

	// Library settings
	LibraryIds   []string `json:"libraryIds,omitempty"`
	LibraryNames []string `json:"libraryNames,omitempty"`

	// Display settings
	Template           string `json:"template,omitempty"`
	SortOrderHome      int    `json:"sortOrderHome,omitempty"`
	SortOrderLibrary   int    `json:"sortOrderLibrary,omitempty"`
	RandomizeHomeOrder bool   `json:"randomizeHomeOrder,omitempty"`
	RandomizeOrder     bool   `json:"randomizeOrder,omitempty"`

	// Poster settings
	AutoPoster         bool `json:"autoPoster,omitempty"`
	AutoPosterTemplate int  `json:"autoPosterTemplate,omitempty"`

	// Download and search automation
	SearchMissingMovies bool   `json:"searchMissingMovies,omitempty"` // Auto-request missing movies
	AutoApproveMovies   bool   `json:"autoApproveMovies,omitempty"`   // Auto-approve movie requests
	DownloadMode        string `json:"downloadMode,omitempty"`        // "overseerr" or "direct"

	// Radarr/Sonarr instance settings
	RadarrInstanceID               int    `json:"radarrInstanceId"`                         // Radarr instance ID for direct mode
	DirectDownloadRadarrProfileID  int    `json:"directDownloadRadarrProfileId,omitempty"`  // Radarr quality profile ID
	DirectDownloadRadarrRootFolder string `json:"directDownloadRadarrRootFolder,omitempty"` // Radarr root folder path
	RadarrTagID                    int    `json:"radarrTagId,omitempty"`                    // Radarr tag ID for the collection
}

func NewAgregarrClient(config AgregarrConfig) *AgregarrClient {
	// Create HTTP client with TLS verification disabled (like curl -k)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &AgregarrClient{
		config: AgregarrConfig{
			Host:   config.Host,
			APIKey: config.APIKey,
		},
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		},
	}
}

func (a *AgregarrClient) makeRequest(method, endpoint string, body any) (*http.Response, error) {
	url := fmt.Sprintf("%s/api/v1/%s", a.config.Host, endpoint)
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if a.config.APIKey != "" {
		req.Header.Set("X-API-Key", a.config.APIKey)
		req.Header.Set("Authorization", a.config.APIKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	return resp, nil
}

func (a *AgregarrClient) CreateCollection(collection Collection) (*Collection, error) {
	endpoint := "collections/create"
	fmt.Printf("Creating collection via POST %s\n", endpoint)

	jsonData, _ := json.MarshalIndent(collection, "", "  ")
	fmt.Printf("Request body: %s\n", string(jsonData))

	resp, err := a.makeRequest("POST", endpoint, collection)
	if err != nil {
		return nil, fmt.Errorf("request error for %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response status for %s: %d\n", endpoint, resp.StatusCode)

	// Read response body
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		fmt.Printf("Response body: %s\n", string(body))
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to create collection at %s: HTTP %d - %s", endpoint, resp.StatusCode, string(body))
	}

	// Parse the response which contains collectionConfigs array
	var response struct {
		CollectionConfigs []Collection `json:"collectionConfigs"`
		Message           string       `json:"message"`
	}

	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&response); err != nil {
		fmt.Printf("Success but couldn't decode response: %v\n", err)
		return &Collection{ID: "created", Name: collection.Name}, nil
	}

	if len(response.CollectionConfigs) > 0 {
		return &response.CollectionConfigs[0], nil
	}

	return &Collection{ID: "created", Name: collection.Name}, nil
}

func (a *AgregarrClient) GetCollections() ([]Collection, error) {
	resp, err := a.makeRequest("GET", "collections", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get collections: HTTP %d", resp.StatusCode)
	}

	// Read and print response for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	fmt.Printf("Collections response: %s\n", string(body)[:min(1000, len(body))])

	// API returns {"collectionConfigs": [...]} not just [...]
	var response struct {
		CollectionConfigs []Collection `json:"collectionConfigs"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.CollectionConfigs, nil
}

func (a *AgregarrClient) DeleteCollection(collectionID string) error {
	endpoint := fmt.Sprintf("collections/%s", collectionID)
	fmt.Printf("Deleting collection %s via DELETE %s\n", collectionID, endpoint)

	resp, err := a.makeRequest("DELETE", endpoint, nil)
	if err != nil {
		return fmt.Errorf("request error for %s: %v", endpoint, err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response status for DELETE %s: %d\n", endpoint, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		fmt.Printf("Response body: %s\n", string(body))
	}

	if resp.StatusCode >= 300 {
		return fmt.Errorf("failed to delete collection %s: HTTP %d - %s", collectionID, resp.StatusCode, string(body))
	}

	fmt.Printf("Successfully deleted collection %s\n", collectionID)
	return nil
}

func (a *AgregarrClient) TestConnection() error {
	// Try to hit various endpoints to see what's available
	testPaths := []string{
		"health",      // Health check
		"status",      // Status
		"ping",        // Ping
		"version",     // Version
		"collections", // Collections
	}

	fmt.Printf("Testing Agregarr connection to: %s\n", a.config.Host)
	for _, path := range testPaths {
		fmt.Printf("Testing: %s\n", path)
		resp, err := a.makeRequest("GET", path, nil)
		if err != nil {
			fmt.Printf("   Error: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("  Status: %d\n", resp.StatusCode)
		if len(body) > 0 && len(body) < 200 {
			fmt.Printf("  Response: %s\n", string(body))
		}
	}
	return nil
}

func SyncCollectionsFromJSON(jsonFile string, radarrConfig RadarrConfig, agregarrConfig AgregarrConfig) error {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("failed to read JSON file %s: %w", jsonFile, err)
	}

	var scrapedData ScrapedData
	if err := json.Unmarshal(data, &scrapedData); err != nil {
		return fmt.Errorf("failed to parse JSON file %s: %w", jsonFile, err)
	}

	agregarrClient := NewAgregarrClient(agregarrConfig)
	radarrClient, err := NewRadarrClient(radarrConfig)
	if err != nil {
		return fmt.Errorf("failed to create Radarr client: %w", err)
	}

	// Get existing collections from Agregarr
	existingCollections, err := agregarrClient.GetCollections()
	if err != nil {
		return fmt.Errorf("failed to get existing collections: %w", err)
	}

	fmt.Printf("Syncing collections from %s (scraped on %s)\n", jsonFile, scrapedData.Date)

	// Build a set of expected collection names from the JSON file
	expectedNames := make(map[string]bool)
	results := scrapedData.Collections
	for _, series := range results {
		// Count valid movies
		validMovies := 0
		for _, movie := range series.Movies {
			if movie.TMDBID > 0 {
				validMovies++
			}
		}

		// Only include series with enough valid movies
		if validMovies >= 2 {
			collectionName := fmt.Sprintf("Metrograph: %s", series.Name)
			expectedNames[collectionName] = true
		}
	}

	// Find and delete collections that are no longer in the JSON file
	deletedCollectionCount := 0
	deletedTagCount := 0
	for _, collection := range existingCollections {
		// Only consider collections that start with "Metrograph: "
		if len(collection.Name) > 12 && collection.Name[:12] == "Metrograph: " {
			if !expectedNames[collection.Name] {
				fmt.Printf("Deleting collection '%s' (no longer in JSON file)\n", collection.Name)

				// Delete from Agregarr
				if err := agregarrClient.DeleteCollection(collection.ID); err != nil {
					fmt.Printf("Warning: Failed to delete collection '%s': %v\n", collection.Name, err)
				} else {
					deletedCollectionCount++
				}

				// Delete corresponding Radarr tag (Subtype contains the tag name like "metrograph-12345")
				if collection.Subtype != "" && len(collection.Subtype) > 11 && collection.Subtype[:11] == "metrograph-" {
					if err := radarrClient.DeleteTag(collection.Subtype); err != nil {
						fmt.Printf("Warning: Failed to delete Radarr tag '%s': %v\n", collection.Subtype, err)
					} else {
						deletedTagCount++
					}
				}
			}
		}
	}

	fmt.Printf("Deleted %d obsolete collection(s) and %d Radarr tag(s)\n", deletedCollectionCount, deletedTagCount)
	return nil
}

func CreateCollectionsFromJSON(jsonFile string, radarrConfig RadarrConfig, agregarrConfig AgregarrConfig) error {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("failed to read JSON file %s: %w", jsonFile, err)
	}

	var scrapedData ScrapedData
	if err := json.Unmarshal(data, &scrapedData); err != nil {
		return fmt.Errorf("failed to parse JSON file %s: %w", jsonFile, err)
	}

	radarrClient, err := NewRadarrClient(radarrConfig)
	if err != nil {
		return fmt.Errorf("failed to create Radarr client: %w", err)
	}

	agregarrClient := NewAgregarrClient(agregarrConfig)

	fmt.Printf("Creating collections from %d series in %s (scraped on %s)\n", len(scrapedData.Collections), jsonFile, scrapedData.Date)
	results := scrapedData.Collections
	for seriesID, series := range results {
		// Count valid movies
		validMovies := 0
		for _, movie := range series.Movies {
			if movie.TMDBID > 0 {
				validMovies++
			}
		}

		fmt.Printf("Creating collection for '%s' with %d movies\n", series.Name, validMovies)

		// Get the tag ID from Radarr for this series
		tagName := fmt.Sprintf("metrograph-%s", seriesID)
		tagID, err := radarrClient.GetTagIDByName(tagName)
		if err != nil {
			fmt.Printf("error: Could not find tag ID for '%s': %v\n", tagName, err)
			return err
		}

		// Create collection
		collectionName := fmt.Sprintf("Metrograph: %s", series.Name)
		collection := Collection{
			ID:   "", // Will be auto-assigned
			Name: collectionName,
			VisibilityConfig: VisibilityConfig{
				UsersHome:          true,
				ServerOwnerHome:    true,
				LibraryRecommended: true,
			},
			MaxItems:  10,
			Type:      "radarrtag",
			Subtype:   fmt.Sprintf("metrograph-%s", seriesID), // This should match the tag name
			MediaType: "movie",

			// Library settings - include all libraries (you can adjust this)
			LibraryIds: []string{"1"}, // Use library ID 1

			// Display options
			Template:       collectionName, // Template should match the collection name
			AutoPoster:     true,           // Auto-generate collection posters
			RandomizeOrder: false,          // Keep original order

			// Search automation
			SearchMissingMovies: true,     // Auto-request missing movies
			AutoApproveMovies:   true,     // Auto-approve movie requests
			DownloadMode:        "direct", // Use direct Radarr integration (not Overseerr)
			RadarrInstanceID:    0,        // Radarr instance ID (0 for first instance)

			// Direct download Radarr settings
			DirectDownloadRadarrProfileID:  radarrConfig.QualityProfileID, // Quality profile ID from config
			DirectDownloadRadarrRootFolder: radarrConfig.RootFolderPath,   // Root folder from config
			RadarrTagID:                    tagID,                         // Tag ID from Radarr
		}

		createdCollection, err := agregarrClient.CreateCollection(collection)
		if err != nil {
			fmt.Printf("Warning: Failed to create collection for series %s: %v\n", series.Name, err)
			continue
		}

		fmt.Printf("Created collection '%s' with ID %s\n", createdCollection.Name, createdCollection.ID)
	}

	return nil
}
