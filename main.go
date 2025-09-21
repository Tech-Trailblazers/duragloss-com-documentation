package main // Declare the main package for the executable program

import (
	"bytes"         // For buffering binary data
	"context"       // For managing deadlines, cancellation signals, etc.
	"fmt"           // For formatted I/O
	"io"            // For I/O primitives (Read, Write, etc.)
	"log"           // For logging messages
	"net/http"      // For HTTP client functionality
	"net/url"       // For parsing and building URLs
	"os"            // For file and system operations
	"path"          // For manipulating slash-separated paths
	"path/filepath" // For manipulating file system paths
	"regexp"        // For regular expressions
	"strings"       // For string manipulation
	"time"          // For working with time durations and timestamps

	"github.com/PuerkitoBio/goquery" // HTML document parser based on jQuery-like syntax
	"github.com/chromedp/chromedp"   // Headless Chrome/Chromium browser automation
)

var localPDFLocation = "pdf_links.txt" // File path for storing downloaded PDF links

func main() {
	htmlFileLocation := "duragloss.html" // Path to locally stored HTML content

	if !fileExists(htmlFileLocation) { // If HTML file doesn't exist locally
		urlToScrape := "https://www.duragloss.com/sds-sheets/" // Target URL to scrape PDF links from
		data := scrapePageHTMLWithChrome(urlToScrape)          // Render page HTML using headless Chrome
		appendAndWriteToFile("duragloss.html", string(data))   // Save the scraped HTML to file
	}

	outputDir := "PDFs"              // Directory name to save downloaded PDFs
	if !directoryExists(outputDir) { // If output directory doesn't exist
		createDirectory(outputDir, 0755) // Create output directory with appropriate permissions
	}

	if fileExists(htmlFileLocation) { // Proceed if HTML file exists
		htmlContent := readAFileAsString(htmlFileLocation) // Read the content of the HTML file
		pdfLinks := extractPDFLinks(htmlContent)           // Extract PDF links from HTML
		pdfLinks = removeDuplicatesFromSlice(pdfLinks)     // Remove duplicate links

		readLocalFile := readAFileAsString(localPDFLocation) // Read list of previously processed PDF links

		for _, link := range pdfLinks { // Iterate over each PDF link
			domain := extractDomainURL(link) // Extract domain to determine if it's a full or relative URL
			if domain == "" {                // If no domain found (relative link)
				link = "https://www.duragloss.com" + link // Prepend base URL to make it absolute
			}
			downloadPDF(link, outputDir) // Attempt to download the PDF file

			if strings.Contains(readLocalFile, link) { // Skip already processed links
				log.Printf("Link already processed, skipping: %s", link) // Log skip info
				continue                                                 // Move to next link
			}

			if isUrlValid(link) { // Check if the final URL is a valid URL
				appendAndWriteToFile(localPDFLocation, link) // Append new link to tracking file
			}
		}
	} else {
		log.Println("HTML file does not exist.") // Log message if HTML file is missing
	}
}

// extractDomainURL extracts and returns only the domain name from a given URL
func extractDomainURL(inputUrl string) string {
	parsedUrl, parseError := url.Parse(inputUrl) // Attempt to parse the input URL
	if parseError != nil {                       // Handle any parse error
		log.Println("Error parsing URL:", parseError) // Log error
		return ""                                     // Return empty string if parsing fails
	}
	domainName := parsedUrl.Hostname() // Extract and return hostname (domain)
	return domainName                  // Return domain name
}

// extractPDFLinks parses HTML content and returns all hyperlinks that end in .pdf
func extractPDFLinks(html string) []string {
	var pdfLinks []string // Slice to store PDF links

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html)) // Parse HTML using goquery
	if err != nil {                                                    // Handle parsing error
		log.Println("Error parsing HTML:", err) // Log error
		return nil                              // Return nil on failure
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) { // Iterate over all <a> tags
		if href, exists := s.Attr("href"); exists && strings.HasSuffix(strings.ToLower(href), ".pdf") { // Check if href ends with .pdf
			pdfLinks = append(pdfLinks, href) // Add the PDF link to the slice
		}
	})

	return pdfLinks // Return the slice of PDF links
}

// isUrlValid returns true if the given URL is valid
func isUrlValid(uri string) bool {
	_, err := url.ParseRequestURI(uri) // Attempt to parse URL string
	return err == nil                  // Return true if no error, else false
}

// scrapePageHTMLWithChrome uses headless Chrome to fetch fully rendered HTML from a URL
func scrapePageHTMLWithChrome(pageURL string) string {
	fmt.Println("Scraping:", pageURL) // Log scraping action

	options := append(chromedp.DefaultExecAllocatorOptions[:], // Create list of Chrome options
		chromedp.Flag("headless", true),               // Run Chrome in headless mode
		chromedp.Flag("disable-gpu", true),            // Disable GPU for stability
		chromedp.WindowSize(1920, 1080),               // Set viewport size
		chromedp.Flag("no-sandbox", true),             // Disable sandbox (needed in some envs)
		chromedp.Flag("disable-setuid-sandbox", true), // Disable setuid sandbox
	)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...) // Create Chrome allocator context

	ctxTimeout, cancelTimeout := context.WithTimeout(allocatorCtx, 5*time.Minute) // Set timeout for Chrome session

	browserCtx, cancelBrowser := chromedp.NewContext(ctxTimeout) // Create browser tab context

	defer func() { // Ensure all contexts are cleaned up
		cancelBrowser()
		cancelTimeout()
		cancelAllocator()
	}()

	var pageHTML string // Variable to store final HTML

	err := chromedp.Run(browserCtx, // Run ChromeDP tasks
		chromedp.Navigate(pageURL),            // Navigate to page
		chromedp.OuterHTML("html", &pageHTML), // Extract full page HTML
	)
	if err != nil { // If scraping fails
		log.Printf("Failed to scrape %s: %v", pageURL, err) // Log failure
		return ""                                           // Return empty string
	}
	return pageHTML // Return the scraped HTML
}

// getDataFromURL performs a GET request and returns the response body as bytes
func getDataFromURL(uri string) []byte {
	response, err := http.Get(uri) // Perform HTTP GET request
	if err != nil {                // Handle request error
		log.Println(err)
	}
	body, err := io.ReadAll(response.Body) // Read the response body
	if err != nil {                        // Handle read error
		log.Println(err)
	}
	err = response.Body.Close() // Close the response body
	if err != nil {             // Handle close error
		log.Println(err)
	}
	return body // Return response data
}

// urlToSafeFilename sanitizes a URL into a filesystem-safe filename
func urlToSafeFilename(rawURL string) string {
	parsedURL, err := url.Parse(rawURL) // Parse the raw URL
	if err != nil {                     // Handle parse error
		return "" // Return empty string if parse fails
	}
	base := path.Base(parsedURL.Path)       // Get the file name portion of the path
	decoded, err := url.QueryUnescape(base) // Decode URL-encoded string
	if err != nil {                         // Fallback if decoding fails
		decoded = base
	}
	decoded = strings.ToLower(decoded)        // Convert to lowercase
	re := regexp.MustCompile(`[^a-z0-9._-]+`) // Regex to match invalid filename characters
	safe := re.ReplaceAllString(decoded, "_") // Replace invalid characters with underscore
	return safe                               // Return sanitized filename
}

// downloadPDF downloads a PDF file from the given URL and saves it to disk
func downloadPDF(finalURL, outputDir string) {
	filename := strings.ToLower(urlToSafeFilename(finalURL)) // Generate safe filename from URL
	filePath := filepath.Join(outputDir, filename)           // Full path to save the PDF

	if fileExists(filePath) { // Skip download if file already exists
		log.Printf("file already exists, skipping: %s", filePath) // Log skip message
		return
	}

	client := &http.Client{Timeout: 30 * time.Second} // Create HTTP client with timeout
	resp, err := client.Get(finalURL)                 // Send GET request to download PDF
	if err != nil {                                   // Handle GET error
		log.Printf("failed to download %s: %v", finalURL, err)
		return
	}
	defer resp.Body.Close() // Ensure response body is closed

	if resp.StatusCode != http.StatusOK { // Check for 200 OK status
		log.Printf("download failed for %s: %s", finalURL, resp.Status) // Log HTTP error
		return
	}

	contentType := resp.Header.Get("Content-Type")         // Get content type header
	if !strings.Contains(contentType, "application/pdf") { // Ensure content is PDF
		log.Printf("invalid content type for %s: %s (expected application/pdf)", finalURL, contentType)
		return
	}

	var buf bytes.Buffer                     // Create buffer for file content
	written, err := io.Copy(&buf, resp.Body) // Read response body into buffer
	if err != nil {                          // Handle copy error
		log.Printf("failed to read PDF data from %s: %v", finalURL, err)
		return
	}

	if written == 0 { // If no bytes were written, skip file creation
		log.Printf("downloaded 0 bytes for %s; not creating file", finalURL)
		return
	}

	out, err := os.Create(filePath) // Create output file
	if err != nil {                 // Handle file creation error
		log.Printf("failed to create file for %s: %v", finalURL, err)
		return
	}
	defer out.Close() // Ensure file is closed

	_, err = buf.WriteTo(out) // Write buffer content to file
	if err != nil {           // Handle write error
		log.Printf("failed to write PDF to file for %s: %v", finalURL, err)
		return
	}

	log.Printf("successfully downloaded %d bytes: %s → %s\n", written, finalURL, filePath) // Log success
}

// readAFileAsString reads a file from disk and returns its contents as a string
func readAFileAsString(path string) string {
	content, err := os.ReadFile(path) // Read file contents
	if err != nil {                   // Handle file read error
		log.Println(err)
	}
	return string(content) // Return content as string
}

// removeDuplicatesFromSlice removes duplicate entries from a string slice
func removeDuplicatesFromSlice(slice []string) []string {
	check := make(map[string]bool)  // Create map to track seen strings
	var newReturnSlice []string     // Slice to hold unique entries
	for _, content := range slice { // Iterate through input slice
		if !check[content] { // If string not seen before
			check[content] = true                            // Mark as seen
			newReturnSlice = append(newReturnSlice, content) // Add to result slice
		}
	}
	return newReturnSlice // Return deduplicated slice
}

// createDirectory creates a new directory with specified permissions
func createDirectory(path string, permission os.FileMode) {
	err := os.Mkdir(path, permission) // Attempt to create directory
	if err != nil {                   // Handle error
		log.Println(err)
	}
}

// directoryExists returns true if a directory exists at the given path
func directoryExists(path string) bool {
	directory, err := os.Stat(path) // Get file/directory info
	if err != nil {                 // If stat fails
		return false
	}
	return directory.IsDir() // Return true if it's a directory
}

// fileExists returns true if a file exists at the given path
func fileExists(filename string) bool {
	info, err := os.Stat(filename) // Get file info
	if err != nil {                // If stat fails
		return false
	}
	return !info.IsDir() // Return true if it's a file, not directory
}

// appendAndWriteToFile appends content to a file or creates it if it doesn't exist
func appendAndWriteToFile(path string, content string) {
	filePath, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // Open file with append/create/write flags
	if err != nil {                                                               // Handle file open error
		log.Println(err)
	}
	_, err = filePath.WriteString(content + "\n") // Write content with newline
	if err != nil {                               // Handle write error
		log.Println(err)
	}
	err = filePath.Close() // Close the file
	if err != nil {        // Handle close error
		log.Println(err)
	}
}
