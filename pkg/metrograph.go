package metrograph

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

type Film struct {
	Title    string
	rawMD    string
	Director string
	Year     int
	TMDBID   int    `json:"tmdb_id,omitempty"`
	IMDBID   string `json:"imdb_id,omitempty"`
}

type Series struct {
	Name   string
	URL    string
	ID     string
	Movies []Film
}

const BASE string = "https://metrograph.com"
const TMDB_BASE_URL string = "https://api.themoviedb.org/3"

type TMDBSearchResponse struct {
	Results []TMDBMovie `json:"results"`
}

type TMDBMovie struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
	IMDBId      string `json:"imdb_id,omitempty"`
}

func extractSeriesID(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	seriesID := u.Query().Get("vista_series_id")
	if seriesID == "" {
		return "", fmt.Errorf("vista_series_id not found in URL")
	}

	return seriesID, nil
}

func cleanTitle(title string) []string {
	variations := []string{title}

	// Remove square brackets and contents
	// Carol [4K DCP] -> Carol
	re := regexp.MustCompile(`\[[^\]]*\]`)
	withoutBrackets := strings.TrimSpace(re.ReplaceAllString(title, ""))
	if withoutBrackets != title && withoutBrackets != "" {
		variations = append(variations, withoutBrackets)
	}

	// Remove everything before "Presents:" (case insensitive)
	// ACE Presents: Carol -> Carol
	presentsRe := regexp.MustCompile(`(?i)^.*presents:\s*`)
	withoutPresents := strings.TrimSpace(presentsRe.ReplaceAllString(title, ""))
	if withoutPresents != title && withoutPresents != "" {
		variations = append(variations, withoutPresents)
	}

	// Combine both rules
	withoutBoth := strings.TrimSpace(presentsRe.ReplaceAllString(withoutBrackets, ""))
	if withoutBoth != title && withoutBoth != withoutBrackets && withoutBoth != withoutPresents && withoutBoth != "" {
		variations = append(variations, withoutBoth)
	}

	return variations
}

func searchTMDBWithTitle(title string, year int, apiKey string) (*TMDBMovie, error) {
	// URL encode the title
	encodedTitle := url.QueryEscape(title)
	searchURL := fmt.Sprintf("%s/search/movie?api_key=%s&query=%s", TMDB_BASE_URL, apiKey, encodedTitle)

	// TODO: remove year and search again if no results
	if year > 0 {
		searchURL += fmt.Sprintf("&year=%d", year)
	}

	// Rate limiting - wait between requests
	// TODO: Handle rate limit better
	time.Sleep(250 * time.Millisecond)

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API returned status %d", resp.StatusCode)
	}

	var searchResp TMDBSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	// Return the first result if available
	if len(searchResp.Results) > 0 {
		return &searchResp.Results[0], nil
	}

	return nil, nil // No results, but no error
}

func SearchTMDB(title string, year int, apiKey string) (*TMDBMovie, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("TMDB API key is required")
	}

	// Get all title variations
	titleVariations := cleanTitle(title)

	// Try each variation
	for i, variation := range titleVariations {
		if i > 0 {
			fmt.Printf("  Trying variation: %s\n", variation)
		}

		movie, err := searchTMDBWithTitle(variation, year, apiKey)
		if err != nil {
			return nil, err
		}
		if movie != nil {
			if i > 0 {
				fmt.Printf("  Success with variation: %s\n", variation)
			}
			return movie, nil
		}
	}

	return nil, fmt.Errorf("no results found for %s (%d) or any variations", title, year)
}

func Crawl(tmdbAPIKey string) (map[string]Series, error) {

	c := colly.NewCollector()
	c.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

	series := []Series{}
	results := map[string]Series{}

	// Get Metograph series website
	c.OnHTML(".row", func(h *colly.HTMLElement) {
		h.ForEach(".movie_title", func(i int, h *colly.HTMLElement) {
			seriesURL := h.ChildAttr("a", "href")
			seriesName := h.Text
			fmt.Printf("Found series: %s -> %s\n", seriesName, seriesURL)

			series = append(series, Series{
				Name:   seriesName,
				URL:    seriesURL,
				Movies: []Film{},
			})
		})
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL.String())
	})

	err := c.Visit(BASE + "/series/")
	if err != nil {
		return nil, err
	}

	for i, s := range series {
		id, err := extractSeriesID(s.URL)
		if err != nil {
			return nil, err
		}
		series[i].ID = id
		results[id] = series[i]

		// Create a new collector for each series to avoid variable capture issues
		movieCollector := c.Clone()
		// DEBUG
		/*
			movieCollector.OnRequest(func(r *colly.Request) {
				fmt.Println("Visiting movie page:", r.URL.String())
			})
		*/

		movieCollector.OnResponse(func(r *colly.Response) {

			// Look for JavaScript redirects
			body := string(r.Body)
			re := regexp.MustCompile(`window\.location\.replace\(['"]([^'"]+)['"]`)
			matches := re.FindStringSubmatch(body)
			if len(matches) > 1 {
				redirectURL := matches[1]
				fmt.Printf("Found JavaScript redirect to: %s\n", redirectURL)

				movieCollector.Visit(BASE + redirectURL)
			}
		})

		movieCollector.OnHTML(".item", func(h *colly.HTMLElement) {
			title := strings.TrimSpace(h.ChildText(".title"))
			metadata := h.ChildText(".film-metadata")

			if title != "" {
				m := Film{
					Title: title,
					rawMD: metadata,
				}

				tmp := results[id]
				tmp.Movies = append(tmp.Movies, m)
				results[id] = tmp
			}
		})

		movieCollector.Visit(BASE + s.URL)
	}

	// Parse metadata for movies that have it
	for seriesID, s := range results {
		var movieList []Film
		for _, m := range s.Movies {
			if m.rawMD != "" {
				parts := strings.Split(m.rawMD, "/")
				if len(parts) >= 2 {
					firstPart := strings.TrimSpace(parts[0])
					secondPart := strings.TrimSpace(parts[1])

					// Check if first part is a year (4 digits)
					// TODO better handling of movies that is titled as a year
					if yr, err := strconv.Atoi(firstPart); err == nil && yr > 1800 && yr < 2100 {
						// First part is year, so director is empty
						m.Year = yr
					} else {
						// First part is director, second part should be year
						m.Director = firstPart
						if yr, err := strconv.Atoi(secondPart); err == nil {
							m.Year = yr
						}
					}
				}
			}

			// Search TMDB for movie
			if tmdbAPIKey != "" {
				if tmdbMovie, err := SearchTMDB(m.Title, m.Year, tmdbAPIKey); err == nil {
					m.TMDBID = tmdbMovie.ID
					fmt.Printf("Found TMDB ID for %s: %d\n", m.Title, m.TMDBID)
				} else {
					fmt.Printf("TMDB lookup failed for %s: %v\n", m.Title, err)
				}
			}

			movieList = append(movieList, m)
		}

		s.Movies = movieList
		results[seriesID] = s
	}

	return results, err
}
