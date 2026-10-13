// Command runner does the Go README walk-through in a fresh module, then runs the checks
// program against the SDK at $GO_REF. Standard library only, so it runs before anything is
// installed.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const module = "github.com/zenrows/zenrows-go-sdk/service/crawl"

var (
	key = os.Getenv("ZENROWS_API_KEY")
	seq = 0
)

func redact(s string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "<REDACTED>")
	}
	return s
}

func trim(s string, n int) string {
	s = redact(s)
	if len(s) > n {
		return fmt.Sprintf("%s… [%d more chars]", s[:n], len(s)-n)
	}
	return s
}

func record(check, title, status, expected, actual, evidence string) {
	seq++
	r := map[string]string{"surface": "go", "check": check, "title": title, "status": status,
		"expected": expected, "actual": trim(actual, 800), "evidence": trim(evidence, 2500)}
	b, _ := json.MarshalIndent(r, "", "  ")
	name := regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(check, "_")
	_ = os.WriteFile(filepath.Join("/results/checks", fmt.Sprintf("0%02d-%s.json", seq, name)), b, 0o644)
	fmt.Printf("[%s] go %s %s: %s\n", status, check, title, strings.SplitN(r["actual"], "\n", 2)[0])
}

func passIf(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func run(dir string, timeout time.Duration, name string, args ...string) (int, string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return 127, err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), out.String()
		}
		if err != nil {
			return 1, out.String() + err.Error()
		}
		return 0, out.String()
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return 124, out.String() + "\n[timed out]"
	}
}

func newModule(dir string) {
	_ = os.MkdirAll(dir, 0o755)
	run(dir, time.Minute, "go", "mod", "init", "example.com/"+filepath.Base(dir))
}

func main() {
	_ = os.MkdirAll("/results/checks", 0o755)
	ref := os.Getenv("GO_REF")

	// README install, exactly as written: go get without a version.
	plain := "/work/plain"
	newModule(plain)
	code, out := run(plain, 5*time.Minute, "go", "get", module)
	status, actual := passIf(code == 0), fmt.Sprintf("exit %d", code)
	if code != 0 && strings.Contains(out, "does not contain package") {
		// Before the first release tag, `go get` without a version resolves the default branch,
		// which does not have the package yet. The SHA stands in for the tag (readme-1b).
		status, actual = "SKIP", fmt.Sprintf("exit %d: not released yet, no service/crawl tag on the default branch", code)
	}
	record("readme-1", "Install as the README says: `go get "+module+"`", status, "the module resolves", actual,
		"$ go get "+module+"\n"+out)

	// The release stand-in: go get at the commit under test.
	app := "/work/app"
	newModule(app)
	code, out = run(app, 5*time.Minute, "go", "get", module+"@"+ref)
	record("readme-1b", "Install at the commit under test (stands in for the release tag)", passIf(code == 0),
		"the module resolves", fmt.Sprintf("exit %d", code), fmt.Sprintf("$ go get %s@%s\n%s", module, ref, out))
	if code != 0 {
		os.Exit(1)
	}
	_, ver := run(app, time.Minute, "go", "list", "-m", "-f", "{{.Version}} {{.Dir}}", module)
	parts := strings.Fields(ver)
	dir := parts[len(parts)-1]
	_ = os.WriteFile("/results/go-module.txt", []byte(ver), 0o644)

	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	m := regexp.MustCompile("(?s)## Usage.*?```go\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		record("readme-2", "README has a usage example", "FAIL", "a ```go block under ## Usage", "not found", "")
	} else {
		block := string(m[1])
		record("readme-2", "README has a usage example", "PASS", "a ```go block under ## Usage",
			fmt.Sprintf("%d lines", strings.Count(block, "\n")), block)

		// Extra: the example compiles as it is.
		ex := filepath.Join(app, "readme_asis")
		_ = os.MkdirAll(ex, 0o755)
		_ = os.WriteFile(filepath.Join(ex, "main.go"), []byte(block), 0o644)
		code, out = run(app, 5*time.Minute, "go", "build", "-o", "/dev/null", "./readme_asis")
		record("x-go-readme-compiles", "The README example compiles as it is", passIf(code == 0),
			"go build succeeds on the unedited example", fmt.Sprintf("go build exit %d", code), "$ go build ./readme_asis\n"+out)
		_ = os.RemoveAll(ex)

		// Run it, with the changes the cost rules need.
		quick := block
		// The start URL is read from the example itself, so a README edit cannot break the match.
		startURL := `"<no URL field>"`
		if u := regexp.MustCompile(`\bURL:\s+("[^"]*")`).FindStringSubmatch(block); u != nil {
			startURL = u[1]
		}
		var applied []string
		for _, s := range [][3]string{
			{`client := crawl.NewClient(crawl.WithAPIKey("YOUR_API_KEY"))`,
				fmt.Sprintf("client := crawl.NewClient(crawl.WithBaseURL(%q)) // the key comes from ZENROWS_API_KEY", os.Getenv("CRAWL_API_BASE")),
				"set the key (the README's ZENROWS_API_KEY route); base URL → CRAWL_API_BASE, so clean-up finds these crawls"},
			{startURL, fmt.Sprintf("%q", os.Getenv("CRAWL_URL")), "start URL → CRAWL_URL"},
			{"MaxItems:        20,", "MaxItems:        5,", "max_items 20 → 5"},
			{"MaxPages:        30,", "MaxPages:        10,", "max_pages 30 → 10 (at most 10)"},
			{`IncludePatterns: []string{"/product/"},`, fmt.Sprintf("IncludePatterns: []string{%q},", os.Getenv("CRAWL_INCLUDE")), "include → CRAWL_INCLUDE"},
			{"OutputFormat:    crawl.OutputFormatHTML, // omit for URLs only", "// OutputFormat dropped: one html crawl per run (check 2)", "no html"},
		} {
			if strings.Contains(quick, s[0]) {
				quick = strings.Replace(quick, s[0], s[1], 1)
				applied = append(applied, s[2])
			} else {
				applied = append(applied, "not found, left as is: "+s[2])
			}
		}
		if key == "" {
			record("readme-3", "Run the README example", "SKIP", "it runs to the end without an error",
				"ZENROWS_API_KEY not set (run without --no-key)", "")
		} else {
			q := filepath.Join(app, "quickstart")
			_ = os.MkdirAll(q, 0o755)
			_ = os.WriteFile(filepath.Join(q, "main.go"), []byte(quick), 0o644)
			since := time.Now().Add(-5 * time.Second)
			_ = os.WriteFile("/results/readme-start.txt", []byte(since.UTC().Format(time.RFC3339)), 0o644)
			code, out = run(app, 12*time.Minute, "go", "run", "./quickstart")
			record("readme-3", "Run the README example", passIf(code == 0), "it runs to the end without an error",
				fmt.Sprintf("exit %d; %d lines printed", code, strings.Count(out, "\n")),
				"substitutions: "+strings.Join(applied, "; ")+"\n$ go run ./quickstart\n"+out)
		}
	}

	// The checks program, against the same module version.
	checks := filepath.Join(app, "checks")
	_ = os.MkdirAll(checks, 0o755)
	src, _ := os.ReadFile("/harness/checks/main.go")
	_ = os.WriteFile(filepath.Join(checks, "main.go"), src, 0o644)
	_ = os.Setenv("CRAWL_MODULE_DIR", dir)
	code, out = run(app, 30*time.Minute, "go", "run", "./checks")
	fmt.Print(redact(out))
	if code != 0 {
		record("checks", "Checks program", "FAIL", "the checks program runs", fmt.Sprintf("exit %d", code), out)
	}
}
