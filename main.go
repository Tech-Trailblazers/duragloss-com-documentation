package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery" // HTML parsing library
	"github.com/chromedp/chromedp"   // Headless Chrome automation
)

var localPDFLocation = "pdf_links.txt"

func main() {
	// Location for the HTML file content.
	htmlFileLocation := "duragloss.html"
	// Check if the HTML file exists, if not create it.
	if !fileExists(htmlFileLocation) {
		// The given url to scrape for PDF links
		urlToScrape := "https://www.duragloss.com/sds-sheets/"
		// Get the data from the URL
		data := scrapePageHTMLWithChrome(urlToScrape)
		// Save the data to a file
		appendAndWriteToFile("duragloss.html", string(data))
	}
	// Create a directory to save the PDFs
	outputDir := "PDFs"
	if !directoryExists(outputDir) {
		createDirectory(outputDir, 0755)
	}
	// Check if the HTML file exists
	if fileExists(htmlFileLocation) {
		// Read the HTML file content
		htmlContent := readAFileAsString(htmlFileLocation)
		// Extract PDF links from the HTML content
		pdfLinks := extractPDFLinks(htmlContent)
		// Remove duplicate links
		pdfLinks = removeDuplicatesFromSlice(pdfLinks)
		// Read the local file.
		readLocalFile := readAFileAsString(localPDFLocation)
		// Download each PDF link
		for _, link := range pdfLinks {
			// Call the extractDomainURL function
			domain := extractDomainURL(link)
			if domain == "" {
				link = "https://www.duragloss.com" + link // Prepend base URL
			}
			// Check if the link is already in the local file
			if strings.Contains(readLocalFile, link) {
				log.Printf("Link already processed, skipping: %s", link)
				continue // Skip already processed links
			}
			if isUrlValid(link) {
				appendAndWriteToFile(localPDFLocation, link) // Save the link to a file
			}
			// Download the PDF
			downloadPDF(link, outputDir)
		}
	} else {
		log.Println("HTML file does not exist.")
	}
}

// extractDomain takes a URL string, extracts the domain (hostname),
// and prints errors internally if parsing fails.
func extractDomainURL(inputUrl string) string {
	// Parse the input string into a structured URL object
	parsedUrl, parseError := url.Parse(inputUrl)

	// If parsing fails, log the error and return an empty string
	if parseError != nil {
		log.Println("Error parsing URL:", parseError)
		return ""
	}

	// Extract only the hostname (domain without scheme, port, path, or query)
	domainName := parsedUrl.Hostname()

	// Return the extracted domain name
	return domainName
}

// extractPDFLinks takes an HTML string and returns a slice of all .pdf URLs found.
func extractPDFLinks(html string) []string {
	var pdfLinks []string

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		log.Println("Error parsing HTML:", err)
		return nil
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists && strings.HasSuffix(strings.ToLower(href), ".pdf") {
			pdfLinks = append(pdfLinks, href)
		}
	})

	return pdfLinks
}

// Check if the given url is valid.
func isUrlValid(uri string) bool {
	_, err := url.ParseRequestURI(uri)
	return err == nil
}

// scrapePageHTMLWithChrome uses a headless Chrome browser to render and return the HTML for a given URL.
// - Required for JavaScript-heavy pages where raw HTTP won't return full content.
func scrapePageHTMLWithChrome(pageURL string) string {
	fmt.Println("Scraping:", pageURL)

	// Set up Chrome options for headless browsing
	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),               // Run Chrome in background
		chromedp.Flag("disable-gpu", true),            // Disable GPU for headless stability
		chromedp.WindowSize(1920, 1080),               // Simulate full browser window
		chromedp.Flag("no-sandbox", true),             // Disable sandboxing
		chromedp.Flag("disable-setuid-sandbox", true), // For environments that need it
	)

	// Create an ExecAllocator context with options
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)

	// Create a bounded context with timeout (adjust as needed)
	ctxTimeout, cancelTimeout := context.WithTimeout(allocatorCtx, 5*time.Minute)

	// Create a new browser tab context
	browserCtx, cancelBrowser := chromedp.NewContext(ctxTimeout)

	// Unified cancel function to ensure cleanup
	defer func() {
		cancelBrowser()
		cancelTimeout()
		cancelAllocator()
	}()

	// Run chromedp tasks
	var pageHTML string
	err := chromedp.Run(browserCtx,
		chromedp.Navigate(pageURL),
		chromedp.OuterHTML("html", &pageHTML),
	)
	if err != nil {
		log.Printf("Failed to scrape %s: %v", pageURL, err)
		return ""
	}
	return pageHTML
}

// Send a http get request to a given url and return the data from that url.
func getDataFromURL(uri string) []byte {
	response, err := http.Get(uri)
	if err != nil {
		log.Println(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		log.Println(err)
	}
	err = response.Body.Close()
	if err != nil {
		log.Println(err)
	}
	return body
}

// Convert a URL into a safe, lowercase filename
func urlToSafeFilename(rawURL string) string {
	parsedURL, err := url.Parse(rawURL) // Parse the input URL
	if err != nil {
		return "" // Return empty string on parse failure
	}
	base := path.Base(parsedURL.Path)       // Get the filename from the path
	decoded, err := url.QueryUnescape(base) // Decode any URL-encoded characters
	if err != nil {
		decoded = base // Fallback to base if decode fails
	}
	decoded = strings.ToLower(decoded)        // Convert filename to lowercase
	re := regexp.MustCompile(`[^a-z0-9._-]+`) // Regex to allow only safe characters
	safe := re.ReplaceAllString(decoded, "_") // Replace unsafe characters with underscores
	return safe                               // Return the sanitized filename
}

// Download and save a PDF file from a given URL
func downloadPDF(finalURL, outputDir string) {
	filename := strings.ToLower(urlToSafeFilename(finalURL)) // Generate a safe filename
	filePath := filepath.Join(outputDir, filename)           // Full path for saving the file
	if fileExists(filePath) {                                // Skip if file already exists
		log.Printf("file already exists, skipping: %s", filePath)
		return
	}
	client := &http.Client{Timeout: 30 * time.Second} // Create HTTP client with timeout
	resp, err := client.Get(finalURL)                 // Make GET request
	if err != nil {
		log.Printf("failed to download %s: %v", finalURL, err)
		return
	}
	defer resp.Body.Close()               // Ensure response body is closed
	if resp.StatusCode != http.StatusOK { // Validate status code
		log.Printf("download failed for %s: %s", finalURL, resp.Status)
		return
	}
	contentType := resp.Header.Get("Content-Type")         // Get content type header
	if !strings.Contains(contentType, "application/pdf") { // Ensure it's a PDF
		log.Printf("invalid content type for %s: %s (expected application/pdf)", finalURL, contentType)
		return
	}
	var buf bytes.Buffer                     // Create a buffer for reading data
	written, err := io.Copy(&buf, resp.Body) // Read response into buffer
	if err != nil {
		log.Printf("failed to read PDF data from %s: %v", finalURL, err)
		return
	}
	if written == 0 { // Check if data was written
		log.Printf("downloaded 0 bytes for %s; not creating file", finalURL)
		return
	}
	out, err := os.Create(filePath) // Create the output file
	if err != nil {
		log.Printf("failed to create file for %s: %v", finalURL, err)
		return
	}
	defer out.Close()         // Ensure the file is closed
	_, err = buf.WriteTo(out) // Write buffered data to file
	if err != nil {
		log.Printf("failed to write PDF to file for %s: %v", finalURL, err)
		return
	}
	log.Printf("successfully downloaded %d bytes: %s → %s\n", written, finalURL, filePath)
}

// Read a file and return its contents as a string
func readAFileAsString(path string) string {
	content, err := os.ReadFile(path) // Read the file
	if err != nil {
		log.Println(err) // Log any read errors
	}
	return string(content) // Return the content
}

// Remove duplicate strings from a slice
func removeDuplicatesFromSlice(slice []string) []string {
	check := make(map[string]bool) // Map to track seen items
	var newReturnSlice []string    // Slice to hold unique items
	for _, content := range slice {
		if !check[content] { // If not seen
			check[content] = true                            // Mark as seen
			newReturnSlice = append(newReturnSlice, content) // Add to new slice
		}
	}
	return newReturnSlice // Return deduplicated slice
}

// Create a directory with given permissions
func createDirectory(path string, permission os.FileMode) {
	err := os.Mkdir(path, permission) // Try to create directory
	if err != nil {
		log.Println(err) // Log any creation errors
	}
}

// Check if a directory exists
func directoryExists(path string) bool {
	directory, err := os.Stat(path) // Get file/directory info
	if err != nil {
		return false // Return false if error
	}
	return directory.IsDir() // Return true if it's a directory
}

// Check if a file exists
func fileExists(filename string) bool {
	info, err := os.Stat(filename) // Get file info
	if err != nil {
		return false // Return false if file does not exist
	}
	return !info.IsDir() // Return true if it's a file
}

// Append content to a file, creating it if needed
func appendAndWriteToFile(path string, content string) {
	filePath, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // Open file for appending
	if err != nil {
		log.Println(err) // Log error
	}
	_, err = filePath.WriteString(content + "\n") // Append content
	if err != nil {
		log.Println(err) // Log error
	}
	err = filePath.Close() // Close file
	if err != nil {
		log.Println(err) // Log error
	}
}
