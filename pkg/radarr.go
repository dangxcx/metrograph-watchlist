package metrograph

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"golift.io/starr"
	"golift.io/starr/radarr"
)

type RadarrConfig struct {
	Host             string
	APIKey           string
	RootFolderPath   string
	QualityProfileID int
	Monitored        bool
	SearchForMovie   bool
}

type RadarrClient struct {
	client *radarr.Radarr
	config RadarrConfig
}

func NewRadarrClient(config RadarrConfig) (*RadarrClient, error) {
	// Create HTTP client with TLS verification disabled (like curl -k)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: tr,
	}

	// Create starr config with custom HTTP client
	starrConfig := starr.New(config.APIKey, config.Host, 0)
	starrConfig.Client = httpClient
	client := radarr.New(starrConfig)

	return &RadarrClient{
		client: client,
		config: config,
	}, nil
}

func (r *RadarrClient) CreateTag(name string) (int, error) {
	// Check if tag already exists
	tags, err := r.client.GetTags()
	if err != nil {
		return 0, fmt.Errorf("failed to get existing tags: %w", err)
	}

	// Look for existing tag
	for _, tag := range tags {
		if tag.Label == name {
			fmt.Printf("Tag '%s' already exists with ID %d\n", name, tag.ID)
			return int(tag.ID), nil
		}
	}

	// Create new tag
	newTag := &starr.Tag{Label: name}
	createdTag, err := r.client.AddTag(newTag)
	if err != nil {
		return 0, fmt.Errorf("failed to create tag '%s': %w", name, err)
	}

	fmt.Printf("Created new tag '%s' with ID %d\n", name, createdTag.ID)
	return int(createdTag.ID), nil
}

func (r *RadarrClient) GetMovieByTMDBID(tmdbID int) (*radarr.Movie, error) {
	// Get all movies and find the one matching this TMDB ID
	movies, err := r.client.GetMovie(&radarr.GetMovie{
		TMDBID: int64(tmdbID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get movies: %w", err)
	}
	if len(movies) == 0 {
		return nil, fmt.Errorf("movie with TMDB ID %d not found", tmdbID)
	}

	return movies[0], nil
}

func (r *RadarrClient) UpdateMovieTags(movieID int64, newTagIDs []int) error {
	// Get the movie first
	movie, err := r.client.GetMovieByID(movieID)
	if err != nil {
		return fmt.Errorf("failed to get movie: %w", err)
	}

	// Update tags
	movie.Tags = newTagIDs

	// Update the movie (requires: id, movie object, moveFiles bool)
	_, err = r.client.UpdateMovie(movieID, movie, false)
	if err != nil {
		return fmt.Errorf("failed to update movie: %w", err)
	}

	return nil
}

func (r *RadarrClient) AddMovie(tmdbID int, title string, year int, tagIDs []int) error {
	// Create new movie
	addMovieInput := &radarr.AddMovieInput{
		Title:            title,
		Year:             year,
		TmdbID:           int64(tmdbID),
		QualityProfileID: int64(r.config.QualityProfileID),
		RootFolderPath:   r.config.RootFolderPath,
		Monitored:        r.config.Monitored,
		Tags:             tagIDs,
		AddOptions: &radarr.AddMovieOptions{
			SearchForMovie: r.config.SearchForMovie,
		},
	}

	addedMovie, err := r.client.AddMovie(addMovieInput)
	if err != nil {
		// Check if the error is because the movie already exists
		errMsg := err.Error()
		if containsAny(errMsg, []string{"already been added", "already exists"}) {
			fmt.Printf("Movie '%s' (%d) already exists in Radarr, adding tags...\n", title, year)

			// Get the existing movie
			existingMovie, err := r.GetMovieByTMDBID(tmdbID)
			if err != nil {
				return fmt.Errorf("failed to get existing movie: %w", err)
			}

			// Merge tags (avoid duplicates)
			existingTagMap := make(map[int]bool)
			for _, tagID := range existingMovie.Tags {
				existingTagMap[tagID] = true
			}

			// Add new tags that don't already exist
			updatedTags := existingMovie.Tags
			addedCount := 0
			for _, tagID := range tagIDs {
				if !existingTagMap[tagID] {
					updatedTags = append(updatedTags, tagID)
					addedCount++
				}
			}

			if addedCount > 0 {
				// Update the movie with the new tags
				err = r.UpdateMovieTags(existingMovie.ID, updatedTags)
				if err != nil {
					return fmt.Errorf("failed to update tags for existing movie: %w", err)
				}
				fmt.Printf("Added %d new tag(s) to existing movie '%s' (%d)\n", addedCount, title, year)
			} else {
				fmt.Printf("Movie '%s' (%d) already has all specified tags\n", title, year)
			}
			return nil
		}

		return fmt.Errorf("failed to add movie '%s' (%d): %w", title, year, err)
	}

	fmt.Printf("Added movie '%s' (%d) to Radarr with ID %d\n", title, year, addedMovie.ID)
	return nil
}

// Helper function to check if a string contains any of the given substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

func (r *RadarrClient) GetTagIDByName(tagName string) (int, error) {
	tags, err := r.client.GetTags()
	if err != nil {
		return 0, fmt.Errorf("failed to get tags from Radarr: %w", err)
	}

	for _, tag := range tags {
		if tag.Label == tagName {
			return int(tag.ID), nil
		}
	}

	return 0, fmt.Errorf("tag '%s' not found in Radarr", tagName)
}

func (r *RadarrClient) DeleteTag(tagName string) error {
	// Get the tag ID first
	tagID, err := r.GetTagIDByName(tagName)
	if err != nil {
		return err // Tag doesn't exist, which is fine
	}

	// Delete the tag by ID
	err = r.client.DeleteTag(tagID)
	if err != nil {
		return fmt.Errorf("failed to delete tag '%s' (ID %d): %w", tagName, tagID, err)
	}

	fmt.Printf("Deleted Radarr tag '%s' (ID %d)\n", tagName, tagID)
	return nil
}

func ProcessJSONToRadarr(jsonFile string, config RadarrConfig) error {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("failed to read JSON file %s: %w", jsonFile, err)
	}

	var scrapedData ScrapedData
	if err := json.Unmarshal(data, &scrapedData); err != nil {
		return fmt.Errorf("failed to parse JSON file %s: %w", jsonFile, err)
	}

	radarrClient, err := NewRadarrClient(config)
	if err != nil {
		return fmt.Errorf("failed to create Radarr client: %w", err)
	}

	fmt.Printf("Processing %d series from %s (scraped on %s)\n", len(scrapedData.Collections), jsonFile, scrapedData.Date)
	results := scrapedData.Collections

	for seriesID, series := range results {
		// Count valid movies (those with TMDB IDs)
		validMovies := 0
		for _, movie := range series.Movies {
			if movie.TMDBID > 0 {
				validMovies++
			}
		}

		if validMovies < 2 {
			continue
		}

		// Create tag for the series
		tagName := fmt.Sprintf("metrograph-%s", seriesID)
		tagID, err := radarrClient.CreateTag(tagName)
		if err != nil {
			fmt.Printf("Warning: Failed to create tag for series %s: %v\n", series.Name, err)
			continue
		}

		// Add each movie with the tag
		addedCount := 0
		for _, movie := range series.Movies {
			if movie.TMDBID > 0 {
				err := radarrClient.AddMovie(movie.TMDBID, movie.Title, movie.Year, []int{tagID})
				if err != nil {
					fmt.Printf("Warning: Failed to add movie %s: %v\n", movie.Title, err)
				} else {
					addedCount++
				}
			}
		}
		fmt.Printf("Added %d/%d movies from series '%s'\n", addedCount, validMovies, series.Name)
	}

	return nil
}

func ListRadarrProfiles(config RadarrConfig) error {
	radarrClient, err := NewRadarrClient(config)
	if err != nil {
		return fmt.Errorf("failed to create Radarr client: %w", err)
	}

	profiles, err := radarrClient.client.GetQualityProfiles()
	if err != nil {
		return fmt.Errorf("failed to get quality profiles: %w", err)
	}

	fmt.Println("Available Quality Profiles:")
	fmt.Println("ID\tName")
	fmt.Println("--\t----")
	for _, profile := range profiles {
		fmt.Printf("%d\t%s\n", profile.ID, profile.Name)
	}

	return nil
}
