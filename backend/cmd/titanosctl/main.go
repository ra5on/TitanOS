// titanosctl accesses the documented TitanOS API with a personal scoped token.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("titanosctl", flag.ContinueOnError)
	base := f.String("url", os.Getenv("TITANOS_URL"), "NAS HTTPS URL (or TITANOS_URL)")
	tokenFile := f.String("token-file", "", "private file containing the API token; otherwise TITANOS_TOKEN")
	caFile := f.String("ca-file", "", "trusted NAS certificate or CA PEM file")
	showVersion := f.Bool("version", false, "show CLI version")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(out, version.Version)
		return err
	}
	u, err := url.Parse(*base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("provide --url https://nas.example")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return fmt.Errorf("HTTPS is required except for localhost development")
	}
	token := os.Getenv("TITANOS_TOKEN")
	if *tokenFile != "" {
		info, err := os.Stat(*tokenFile)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("token file must be private (chmod 600)")
		}
		data, e := os.ReadFile(*tokenFile)
		if e != nil {
			return e
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return fmt.Errorf("set TITANOS_TOKEN or --token-file; tokens are never command-line arguments")
	}
	cmd := f.Args()
	if len(cmd) == 0 {
		return fmt.Errorf("usage: titanosctl --url URL status | files ROOT [PATH] | docker [ID ACTION] | vm [NAME ACTION] | openapi | request METHOD /path [JSON-FILE]")
	}
	method, path := http.MethodGet, ""
	var body []byte
	switch cmd[0] {
	case "status":
		path = "/system"
	case "openapi":
		path = "/openapi.json"
	case "files":
		if len(cmd) < 2 {
			return fmt.Errorf("files needs a root ID")
		}
		q := url.Values{"root": {cmd[1]}}
		if len(cmd) > 2 {
			q.Set("path", cmd[2])
		}
		path = "/files/list?" + q.Encode()
	case "docker", "vm":
		path = "/docker"
		if cmd[0] == "vm" {
			path = "/vms"
		}
		if len(cmd) > 1 {
			if len(cmd) != 3 {
				return fmt.Errorf("provide ID and action")
			}
			if cmd[0] == "docker" {
				path += "/containers"
			}
			path += "/" + url.PathEscape(cmd[1]) + "/" + url.PathEscape(cmd[2])
			method = http.MethodPost
		}
	case "request":
		if len(cmd) < 3 || len(cmd) > 4 {
			return fmt.Errorf("request needs METHOD /path [JSON-FILE]")
		}
		method = strings.ToUpper(cmd[1])
		path = cmd[2]
		if len(cmd) == 4 {
			body, err = os.ReadFile(cmd[3])
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown command %q", cmd[0])
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n#") {
		return fmt.Errorf("API path must start with a single slash")
	}
	endpoint := strings.TrimSuffix(u.String(), "/") + "/api/v1" + path
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	if *caFile != "" {
		data, e := os.ReadFile(*caFile)
		if e != nil {
			return e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(data) {
			return fmt.Errorf("CA file contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 && !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		_, err = io.Copy(out, resp.Body)
		return err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("JSON response exceeds CLI display limit")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		_, err = fmt.Fprintln(out, pretty.String())
		return err
	}
	_, err = out.Write(data)
	return err
}
