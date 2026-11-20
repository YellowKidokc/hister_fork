package document

import (
	"errors"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var ErrNoURL = errors.New("no URL found in HTML")

func FromHTML(html string) (*Document, error) {
	return FromHTMLWithFallbackURL(html, "")
}

// FromHTMLWithFallbackURL builds a document from HTML and uses fallbackURL
// when the HTML does not contain URL metadata.
func FromHTMLWithFallbackURL(html, fallbackURL string) (*Document, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	u := extractURL(doc)
	if u == "" {
		u = strings.TrimSpace(fallbackURL)
	}
	if u == "" {
		return nil, ErrNoURL
	}

	return &Document{
		URL:   u,
		HTML:  html,
		Title: extractTitle(doc),
	}, nil
}

// StripHTMLImages removes page image elements while preserving adjacent content.
func StripHTMLImages(source string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	images := doc.Find("img, picture, svg, input[type=image]")
	if images.Length() == 0 {
		return source, nil
	}
	images.Remove()
	return doc.Html()
}

func extractURL(doc *goquery.Document) string {
	if href, ok := doc.Find(`link[rel="canonical"]`).First().Attr("href"); ok {
		if u := strings.TrimSpace(href); u != "" {
			return u
		}
	}

	for _, sel := range []string{
		`meta[property="og:url"]`,
		`meta[name="og:url"]`,
		`meta[name="twitter:url"]`,
		`meta[property="twitter:url"]`,
	} {
		if content, ok := doc.Find(sel).First().Attr("content"); ok {
			if u := strings.TrimSpace(content); u != "" {
				return u
			}
		}
	}

	return ""
}

func extractTitle(doc *goquery.Document) string {
	for _, sel := range []string{
		`meta[property="og:title"]`,
		`meta[name="og:title"]`,
		`meta[name="twitter:title"]`,
		`meta[property="twitter:title"]`,
	} {
		if content, ok := doc.Find(sel).First().Attr("content"); ok {
			if t := strings.TrimSpace(content); t != "" {
				return t
			}
		}
	}

	return strings.TrimSpace(doc.Find("title").First().Text())
}
