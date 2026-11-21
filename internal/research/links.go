package research

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// AnyPublicHost in TP_URL_HOSTS allows downloads from every public website.
// Hostnames that resolve to private, loopback or link-local addresses stay
// blocked at connection time, so a link list cannot reach the home network.
const AnyPublicHost = "*"

func allowsAnyPublicHost(allowed []string) bool {
	for _, h := range allowed {
		if strings.TrimSpace(h) == AnyPublicHost {
			return true
		}
	}
	return false
}

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// publicIP reports whether ip is a globally routable unicast address.
func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !carrierGradeNAT.Contains(ip)
}

// publicOnlyDial refuses connections to non-public addresses after DNS
// resolution, which also covers DNS rebinding.
func publicOnlyDial(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, addr string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}
		if !publicIP(net.ParseIP(host)) {
			return errors.New("source address is not a public internet address")
		}
		return nil
	}}
	return d.DialContext(ctx, network, address)
}

// Fetching is opt-in for exact public hostnames (or "*" for any public host),
// including every redirect host.
// This also works through the inherited cloud proxy; it never changes TLS trust.
func ValidateSourceURL(raw string, allowed []string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return nil, errors.New("invalid URL")
	}
	host := strings.ToLower(u.Hostname())
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || host == "" || u.Opaque != "" || u.Fragment != "" {
		return nil, errors.New("use an HTTP(S) URL without credentials or fragment")
	}
	if u.Port() != "" && u.Port() != "80" && u.Port() != "443" {
		return nil, errors.New("source URL uses an unsupported port")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return nil, errors.New("URL import requires an allowlisted public hostname")
	}
	if allowsAnyPublicHost(allowed) {
		return u, nil
	}
	for _, h := range allowed {
		if host == strings.ToLower(strings.TrimSpace(h)) {
			return u, nil
		}
	}
	return nil, errors.New("source hostname is not enabled in TP_URL_HOSTS")
}
func SourceHTTP(allowed []string) *http.Client {
	c := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := ValidateSourceURL(req.URL.String(), allowed)
		return err
	}}
	if allowsAnyPublicHost(allowed) {
		// Connect directly so the address check sees the real destination.
		c.Transport = &http.Transport{
			DialContext:           publicOnlyDial,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConns:          20,
			IdleConnTimeout:       60 * time.Second,
		}
	}
	return c
}

// SpreadsheetURLs reads CSV/TSV or XLSX cells and hyperlink targets, with
// independent ZIP expansion bounds. Formulas/macros are never evaluated.
func SpreadsheetURLs(name string, raw []byte) ([]string, error) {
	cells := []string{}
	if strings.EqualFold(filepath.Ext(name), ".xlsx") {
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, errors.New("invalid XLSX")
		}
		if len(z.File) > 2000 {
			return nil, errors.New("spreadsheet has too many ZIP entries")
		}
		remaining := int64(16 << 20)
		for _, f := range z.File {
			if !strings.HasPrefix(f.Name, "xl/") || !(strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels")) {
				continue
			}
			if f.UncompressedSize64 > uint64(remaining) {
				return nil, errors.New("spreadsheet expanded content exceeds 16 MiB")
			}
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			b, e := io.ReadAll(io.LimitReader(r, remaining+1))
			r.Close()
			remaining -= int64(len(b))
			if e != nil || remaining < 0 {
				return nil, errors.New("spreadsheet expanded content exceeds 16 MiB")
			}
			dec := xml.NewDecoder(bytes.NewReader(b))
			for {
				tok, e := dec.Token()
				if e == io.EOF {
					break
				}
				if e != nil {
					return nil, errors.New("invalid spreadsheet XML")
				}
				switch t := tok.(type) {
				case xml.CharData:
					cells = append(cells, string(t))
				case xml.StartElement:
					for _, a := range t.Attr {
						if a.Name.Local == "Target" {
							cells = append(cells, a.Value)
						}
					}
				}
				if len(cells) > 200000 {
					return nil, errors.New("spreadsheet has too many cells")
				}
			}
		}
	} else {
		r := csv.NewReader(bytes.NewReader(raw))
		r.FieldsPerRecord = -1
		if strings.EqualFold(filepath.Ext(name), ".tsv") {
			r.Comma = '\t'
		}
		for {
			row, e := r.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			cells = append(cells, row...)
			if len(cells) > 200000 {
				return nil, errors.New("spreadsheet has too many cells")
			}
		}
	}
	result := []string{}
	seen := map[string]bool{}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		u, e := url.Parse(cell)
		if e != nil || !(u.Scheme == "https" || u.Scheme == "http") || u.Hostname() == "" {
			continue
		}
		if !seen[cell] {
			result = append(result, cell)
			seen[cell] = true
		}
		if len(result) > 5000 {
			return nil, errors.New("spreadsheet exceeds 5000 links")
		}
	}
	if len(result) == 0 {
		return nil, errors.New("no HTTP(S) links found in spreadsheet cells")
	}
	return result, nil
}
