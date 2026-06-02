package worker

import (
	"crypto/tls"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server-prewarm/internal/scanner"
	"server-prewarm/internal/ws"
)

// Result represents the result of prewarming a single URL
type Result struct {
	URL        string
	StatusCode int
	Cache      string // CF-Cache-Status: HIT, MISS, EXPIRED, etc.
	Pop        string // CF edge location
	Duration   time.Duration
	Error      error
}

// Stats tracks overall prewarm statistics
type Stats struct {
	Total    int64
	Progress int64
	Hit      int64
	Miss     int64
	Expired  int64
	Failed   int64
}

// PrewarmOptions controls prewarm behavior
type PrewarmOptions struct {
	Parallel  int
	RefDomain string
	Timeout   time.Duration
}

// PrewarmPlaylist fetches m3u8 playlist, extracts segment URLs, and prewarms them all
func PrewarmPlaylist(target scanner.PrewarmTarget, opts PrewarmOptions, statsCh chan<- Stats) {
	if opts.Parallel <= 0 {
		opts.Parallel = 20
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}

	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        opts.Parallel * 2,
			MaxIdleConnsPerHost: opts.Parallel,
			IdleConnTimeout:     30 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		},
		// Don't follow redirects for HEAD
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Fetch master playlist
	masterURL := target.PlaylistURL

	urls, err := collectURLs(client, masterURL, opts.RefDomain)
	if err != nil {
		statsCh <- Stats{Total: 1, Failed: 1, Progress: 1}
		return
	}

	// Prewarm all URLs with concurrency limit
	var stats Stats
	stats.Total = int64(len(urls))

	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.Parallel)

	for _, url := range urls {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			result := headRequest(client, u, opts.RefDomain)
			updateStats(&stats, result)
			atomic.AddInt64(&stats.Progress, 1)

			// Broadcast via WebSocket
			errStr := ""
			if result.Error != nil {
				errStr = result.Error.Error()
			}
			ws.GetHub().Broadcast("url_result", ws.URLResult{
				MediaSlug:  target.MediaSlug,
				FileSlug:   target.FileSlug,
				Resolution: target.Resolution,
				URL:        path.Base(u),
				Status:     result.StatusCode,
				Cache:      result.Cache,
				Pop:        result.Pop,
				Duration:   result.Duration.Round(time.Millisecond).String(),
				Error:      errStr,
				Progress:   atomic.LoadInt64(&stats.Progress),
				Total:      stats.Total,
			})
		}(url)
	}

	wg.Wait()
	statsCh <- stats
}

// collectURLs fetches the m3u8 playlist or WebVTT file and returns all segment/image URLs
func collectURLs(client *http.Client, masterURL string, refDomain string) ([]string, error) {
	urlSet := make(map[string]bool)
	urlSet[masterURL] = true

	masterContent, err := fetchContent(client, masterURL, refDomain)
	if err != nil {
		return nil, err
	}

	baseURL := masterURL[:strings.LastIndex(masterURL, "/")+1]

	// Handle WebVTT sprite map files
	if strings.HasSuffix(strings.ToLower(masterURL), ".vtt") {
		images := parseVTTImages(masterContent)
		for _, img := range images {
			imgURL := buildURL(img, baseURL)
			urlSet[imgURL] = true
		}
	} else {
		// Existing HLS playlist logic
		lines := strings.Split(masterContent, "\n")
		var childPlaylists []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasSuffix(line, ".m3u8") {
				childPlaylists = append(childPlaylists, line)
			}
		}

		if len(childPlaylists) > 0 {
			// Multi-variant playlist
			for _, child := range childPlaylists {
				childURL := buildURL(child, baseURL)
				urlSet[childURL] = true

				childContent, err := fetchContent(client, childURL, refDomain)
				if err != nil {
					continue
				}

				childBase := childURL[:strings.LastIndex(childURL, "/")+1]
				for _, seg := range parseSegments(childContent) {
					segURL := buildURL(seg, childBase)
					urlSet[segURL] = true
				}
			}
		} else {
			// Single playlist
			for _, seg := range parseSegments(masterContent) {
				segURL := buildURL(seg, baseURL)
				urlSet[segURL] = true
			}
		}

		// Also add poster.jpeg in the same folder
		posterURL := baseURL + "poster.jpeg"
		urlSet[posterURL] = true
	}

	urls := make([]string, 0, len(urlSet))
	for u := range urlSet {
		urls = append(urls, u)
	}
	return urls, nil
}

// parseVTTImages extracts unique image filenames referenced in WebVTT sprites
func parseVTTImages(content string) []string {
	var images []string
	seen := make(map[string]bool)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "WEBVTT") || strings.HasPrefix(line, "NOTE") {
			continue
		}
		if strings.Contains(line, "-->") {
			continue
		}
		// Match image files like sprite1.jpg, 1.png, 1.jpeg, etc.
		if strings.Contains(line, ".jpg") || strings.Contains(line, ".jpeg") || strings.Contains(line, ".png") {
			part := line
			if idx := strings.Index(line, "#"); idx >= 0 {
				part = line[:idx]
			}
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				images = append(images, part)
			}
		}
	}
	return images
}

// parseSegments extracts segment filenames from m3u8 content
func parseSegments(content string) []string {
	var segments []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ".ts") || strings.HasSuffix(line, ".jpeg") ||
			strings.HasPrefix(line, "http") || strings.HasPrefix(line, "//") {
			segments = append(segments, line)
		}
	}
	return segments
}

// buildURL resolves a relative URL against a base
func buildURL(segment, base string) string {
	if strings.HasPrefix(segment, "http") {
		return segment
	}
	if strings.HasPrefix(segment, "//") {
		return "https:" + segment
	}
	if strings.HasPrefix(segment, "/") {
		// Absolute path - need domain from base
		idx := strings.Index(base[8:], "/") // skip "https://"
		if idx > 0 {
			return base[:idx+8] + segment
		}
		return base + segment
	}
	return base + segment
}

// fetchContent does a GET request and returns the body as string
func fetchContent(client *http.Client, url string, refDomain string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Prewarm/1.0)")
	if refDomain != "" {
		req.Header.Set("Referer", refDomain)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// headRequest performs a HEAD request and returns cache info
func headRequest(client *http.Client, url string, refDomain string) Result {
	start := time.Now()

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return Result{URL: url, Error: err, Duration: time.Since(start)}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Prewarm/1.0)")
	if refDomain != "" {
		req.Header.Set("Referer", refDomain)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{URL: url, Error: err, Duration: time.Since(start)}
	}
	defer resp.Body.Close()

	cacheStatus := resp.Header.Get("CF-Cache-Status")
	if cacheStatus == "" {
		cacheStatus = resp.Header.Get("X-Cache")
		if cacheStatus == "" {
			cacheStatus = "NONE"
		}
	}

	cfRay := resp.Header.Get("CF-Ray")
	pop := "UNK"
	if parts := strings.Split(cfRay, "-"); len(parts) > 1 {
		pop = parts[len(parts)-1]
	}

	return Result{
		URL:        url,
		StatusCode: resp.StatusCode,
		Cache:      cacheStatus,
		Pop:        pop,
		Duration:   time.Since(start),
	}
}

// updateStats updates the stats atomically based on a result
func updateStats(stats *Stats, result Result) {
	if result.Error != nil || (result.StatusCode != 200 && result.StatusCode != 206) {
		atomic.AddInt64(&stats.Failed, 1)
		return
	}

	switch result.Cache {
	case "HIT", "REVALIDATED":
		atomic.AddInt64(&stats.Hit, 1)
	case "MISS":
		atomic.AddInt64(&stats.Miss, 1)
	case "EXPIRED":
		atomic.AddInt64(&stats.Expired, 1)
	default:
		atomic.AddInt64(&stats.Miss, 1)
	}
}
