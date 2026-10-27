package scan

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Probe is what a port answers.
type Probe struct {
	HTTP   bool
	Status int    // the HTTP status, when it speaks HTTP
	Title  string // the page's <title>
	Note   string // "tcp", "ws only", "no answer"
	At     time.Time
}

// Label is the short form for the list: "200 OK", "404", "tcp".
func (p Probe) Label() string {
	switch {
	case p.At.IsZero():
		return "…"
	case p.HTTP && p.Status == 200:
		return "200 OK"
	case p.HTTP:
		return strconv.Itoa(p.Status)
	}
	return p.Note
}

// notHTTP are kinds that speak their own protocol: an HTTP request would
// only put an error in their logs.
var notHTTP = regexp.MustCompile(`(?i)postgres|redis|mysql|mariadb|mongo|rabbitmq|memcache|kafka|zookeeper|nats|clickhouse-server|cassandra|elasticsearch:9300`)

// SkipHTTP says whether to probe a listener only for an open socket.
func SkipHTTP(l Listener) bool {
	if l.Kind == "postgres" || l.Kind == "redis" || l.Kind == "mysql" || l.Kind == "mongo" {
		return true
	}
	if l.Docker != nil && notHTTP.MatchString(l.Docker.Image) {
		return true
	}
	switch l.Port {
	case 5432, 6379, 3306, 27017, 5672, 11211, 9092, 2181, 4222, 1025, 25, 587, 22:
		return true
	}
	return false
}

var reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

var client = &http.Client{
	Timeout: 1500 * time.Millisecond,
	// a redirect's target is still this server answering
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// ProbePort asks localhost:port for its front page.
func ProbePort(ctx context.Context, port int, httpToo bool) Probe {
	p := Probe{At: time.Now()}
	addr := net.JoinHostPort("localhost", strconv.Itoa(port))
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		p.Note = "no answer"
		return p
	}
	conn.Close()
	p.Note = "tcp"
	if !httpToo {
		return p
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/", nil)
	req.Header.Set("User-Agent", "pattan")
	req.Header.Set("Accept", "text/html,*/*")
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "malformed HTTP") {
			p.Note = "not http"
		}
		return p
	}
	defer resp.Body.Close()
	p.HTTP, p.Status = true, resp.StatusCode
	if resp.StatusCode == 426 { // Upgrade Required: a WebSocket server
		p.Note, p.HTTP = "ws only", false
		return p
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if m := reTitle.FindSubmatch(body); m != nil {
		p.Title = strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
	}
	if p.Title == "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		p.Title = fmt.Sprintf("redirects to %s", resp.Header.Get("Location"))
	}
	return p
}
