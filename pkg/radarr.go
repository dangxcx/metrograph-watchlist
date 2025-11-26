package metrograph

import (
	"encoding/json"
	"fmt"
	"os"

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
	starr := starr.New(config.APIKey, config.Host, 0)
	client := radarr.New(starr)

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

func (r *RadarrClient) AddMovie(tmdbID int, title string, year int, tagIDs []int) error {
	// Check if movie already exists by looking up via TMDB ID
	// Skip the check for now and let Radarr handle duplicates
	// movies, err := r.client.GetMovies()
	// if err != nil {
	//     return fmt.Errorf("failed to get existing movies: %w", err)
	// }

	// for _, movie := range movies {
	//     if movie.TmdbID == int64(tmdbID) {
	//         fmt.Printf("Movie '%s' (%d) already exists in Radarr\n", title, year)
	//         return nil
	//     }
	// }

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
		return fmt.Errorf("failed to add movie '%s' (%d): %w", title, year, err)
	}

	fmt.Printf("Added movie '%s' (%d) to Radarr with ID %d\n", title, year, addedMovie.ID)
	return nil
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

func ProcessJSONToRadarr(jsonFile string, config RadarrConfig) error {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("failed to read JSON file %s: %w", jsonFile, err)
	}

	var results map[string]Series
	if err := json.Unmarshal(data, &results); err != nil {
		return fmt.Errorf("failed to parse JSON file %s: %w", jsonFile, err)
	}

	radarrClient, err := NewRadarrClient(config)
	if err != nil {
		return fmt.Errorf("failed to create Radarr client: %w", err)
	}

	fmt.Printf("Processing %d series from %s\n", len(results), jsonFile)

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

