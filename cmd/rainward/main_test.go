package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func call(args []string, stdin string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestReport(t *testing.T) {
	code, out, errOut := call([]string{"../../lots/clay-slope.json"}, "")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"Rainward: Sloping block on clay", "today", "with plan", "ran along the house walls", "best value first"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestOptionsAndJSON(t *testing.T) {
	code, out, errOut := call([]string{"-storm", "twenty-year", "-budget", "800", "-json", "../../lots/sandy-flat.json"}, "")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var res struct {
		Storm struct{ ID string }
		Cost  float64
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Storm.ID != "twenty-year" || res.Cost > 800 {
		t.Errorf("storm %q, cost %.0f", res.Storm.ID, res.Cost)
	}
	data, _ := os.ReadFile("../../lots/corner-loam.json")
	if code, out, _ := call([]string{"-"}, string(data)); code != 0 || !strings.Contains(out, "Corner block") {
		t.Error("reading from standard input failed")
	}
}

func TestBadInput(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.json")
	_ = os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0o600)
	link := filepath.Join(dir, "link.json")
	_ = os.Symlink("../../lots/clay-slope.json", link)
	cases := [][]string{
		{},
		{"-storm", "hurricane", "../../lots/clay-slope.json"},
		{"-budget", "-5", "../../lots/clay-slope.json"},
		{"-budget", "999999", "../../lots/clay-slope.json"},
		{"-evil", "../../lots/clay-slope.json"},
		{dir},
		{link},
		{filepath.Join(dir, "missing.json")},
		{secret},
		{"a.json", "b.json"},
	}
	for _, args := range cases {
		code, out, errOut := call(args, "")
		if code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
		if out != "" || strings.Contains(errOut, "hunter2") {
			t.Errorf("%v: printed %q / %q", args, out, errOut)
		}
	}
}
