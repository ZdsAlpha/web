// Command dns manages Porkbun DNS records for the site's domain. It uses the
// Porkbun JSON API directly (Go's TLS stack, so it avoids the Windows schannel
// revocation issue that breaks curl on some machines).
//
// Credentials come from the environment:
//
//	PORKBUN_API_KEY, PORKBUN_SECRET_API_KEY
//
// Usage:
//
//	go run ./tools/dns list
//	go run ./tools/dns plan  -v4 1.2.3.4 -v6 2a09:... -www app.fly.dev
//	go run ./tools/dns apply -v4 1.2.3.4 -v6 2a09:... -www app.fly.dev
//
// `plan` prints what would change; `apply` performs it: it deletes the Porkbun
// parking records (apex ALIAS + wildcard CNAME) and creates the Fly records
// (apex A + AAAA, www CNAME).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	api    = "https://api.porkbun.com/api/json/v3"
	domain = "arehman.dev"
	ttl    = "600"
)

type creds struct {
	APIKey    string `json:"apikey"`
	SecretKey string `json:"secretapikey"`
}

type record struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	c := creds{
		APIKey:    os.Getenv("PORKBUN_API_KEY"),
		SecretKey: os.Getenv("PORKBUN_SECRET_API_KEY"),
	}
	if c.APIKey == "" || c.SecretKey == "" {
		fail("PORKBUN_API_KEY and PORKBUN_SECRET_API_KEY must be set")
	}

	switch os.Args[1] {
	case "list":
		records, err := retrieve(c)
		check(err)
		for _, r := range records {
			fmt.Printf("%-12s %-6s %-30s %-40s ttl=%s\n", r.ID, r.Type, r.Name, r.Content, r.TTL)
		}
	case "plan":
		v4, v6, www := parseSyncFlags(os.Args[2:])
		printPlan(c, v4, v6, www)
	case "apply":
		v4, v6, www := parseSyncFlags(os.Args[2:])
		apply(c, v4, v6, www)
	default:
		usage()
	}
}

func parseSyncFlags(args []string) (v4, v6, www string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	fs.StringVar(&v4, "v4", "", "Fly IPv4 address for the apex A record")
	fs.StringVar(&v6, "v6", "", "Fly IPv6 address for the apex AAAA record")
	fs.StringVar(&www, "www", "", "CNAME target for www (e.g. <app>.fly.dev)")
	_ = fs.Parse(args)
	if v4 == "" && v6 == "" {
		fail("at least one of -v4 or -v6 is required")
	}
	return v4, v6, www
}

func printPlan(c creds, v4, v6, www string) {
	records, err := retrieve(c)
	check(err)
	fmt.Println("Current records:")
	for _, r := range records {
		fmt.Printf("  %-6s %-28s %s\n", r.Type, r.Name, r.Content)
	}
	fmt.Println("\nWill DELETE parking records:")
	for _, r := range parking(records) {
		fmt.Printf("  - %-6s %-28s %s (id %s)\n", r.Type, r.Name, r.Content, r.ID)
	}
	fmt.Println("\nWill CREATE:")
	if v4 != "" {
		fmt.Printf("  + A     %-28s %s\n", domain, v4)
	}
	if v6 != "" {
		fmt.Printf("  + AAAA  %-28s %s\n", domain, v6)
	}
	if www != "" {
		fmt.Printf("  + CNAME %-28s %s\n", "www."+domain, www)
	}
	fmt.Println("\nRun the same command with `apply` to perform these changes.")
}

func apply(c creds, v4, v6, www string) {
	records, err := retrieve(c)
	check(err)

	for _, r := range parking(records) {
		fmt.Printf("deleting %s %s (id %s)...\n", r.Type, r.Name, r.ID)
		check(del(c, r.ID))
	}
	if v4 != "" {
		fmt.Printf("creating A %s -> %s...\n", domain, v4)
		check(create(c, "", "A", v4))
	}
	if v6 != "" {
		fmt.Printf("creating AAAA %s -> %s...\n", domain, v6)
		check(create(c, "", "AAAA", v6))
	}
	if www != "" {
		fmt.Printf("creating CNAME www -> %s...\n", www)
		check(create(c, "www", "CNAME", www))
	}
	fmt.Println("done. Verify with `go run ./tools/dns list` and `fly certs check`.")
}

// parking returns the default Porkbun records that point at pixie.porkbun.com:
// the apex ALIAS and the wildcard CNAME. These must go before adding Fly records.
func parking(records []record) []record {
	var out []record
	for _, r := range records {
		if r.Content == "pixie.porkbun.com" && (r.Type == "ALIAS" || r.Type == "CNAME") {
			out = append(out, r)
		}
	}
	return out
}

// --- Porkbun API calls ---

func retrieve(c creds) ([]record, error) {
	var resp struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		Records []record `json:"records"`
	}
	if err := post("/dns/retrieve/"+domain, c, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "SUCCESS" {
		return nil, fmt.Errorf("retrieve: %s", resp.Message)
	}
	return resp.Records, nil
}

func create(c creds, name, typ, content string) error {
	body := map[string]string{"name": name, "type": typ, "content": content, "ttl": ttl}
	var resp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := post("/dns/create/"+domain, c, body, &resp); err != nil {
		return err
	}
	if resp.Status != "SUCCESS" {
		return fmt.Errorf("create %s %s: %s", typ, name, resp.Message)
	}
	return nil
}

func del(c creds, id string) error {
	var resp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := post("/dns/delete/"+domain+"/"+id, c, nil, &resp); err != nil {
		return err
	}
	if resp.Status != "SUCCESS" {
		return fmt.Errorf("delete %s: %s", id, resp.Message)
	}
	return nil
}

// post sends a Porkbun API request. extra fields are merged with credentials.
func post(path string, c creds, extra map[string]string, out any) error {
	payload := map[string]string{"apikey": c.APIKey, "secretapikey": c.SecretKey}
	for k, v := range extra {
		payload[k] = v
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(api+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s (%d): %s", path, resp.StatusCode, string(data))
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: dns <list|plan|apply> [-v4 IP] [-v6 IP] [-www TARGET]")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
