package policy

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"gopkg.in/yaml.v2"
)

const hardPolicy = `package main

import rego.v1

deny contains {"rule": "hard_test", "msg": "hard denied"} if {
	input.format_version == "bad"
}
`

const softPolicy = `package main

import rego.v1

deny contains {"rule": "soft_test", "msg": "soft denied"} if {
	input.format_version == "warn"
}
`

const warnPolicy = `package main

import rego.v1

warn contains {"rule": "warn_test", "msg": "warned about X", "severity": "low"} if {
	input.format_version == "warn"
}
`

// requireConftest skips the test when the conftest binary is unavailable so the
// suite still passes in environments that lack it. Tests are expected to run
// with a real conftest on PATH (or CONFTEST_BIN set) to actually exercise the
// wrapper.
func requireConftest(t *testing.T) {
	t.Helper()
	bin := os.Getenv("CONFTEST_BIN")
	if bin == "" {
		bin, _ = exec.LookPath("conftest")
	}
	if bin == "" {
		t.Skip("conftest binary not available; skipping conftest-backed test")
	}
}

// writeDir creates a t.TempDir() populated with the given file contents and
// returns its path. Used to write inline .rego test fixtures.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

func evalInput(t *testing.T, e *Engine, input interface{}) *v1beta1.PolicyEvalResult {
	t.Helper()
	res, err := e.Eval(context.Background(), input)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	return res
}

// violation builds the expected PolicyViolation for a deny element carrying
// the given extra metadata keys. conftest populates "query" and "rule" itself,
// so the expected metadata is the element's string-valued passthrough fields.
func violation(msg string, extra map[string]string) v1beta1.PolicyViolation {
	return v1beta1.PolicyViolation{Msg: msg, Metadata: extra}
}

func TestEval(t *testing.T) {
	requireConftest(t)
	tests := []struct {
		name    string
		cfg     Config
		input   map[string]interface{}
		allowed bool
		hard    []v1beta1.PolicyViolation
		soft    []v1beta1.PolicyViolation
	}{
		{
			name: "hard deny violation",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
			},
			input:   map[string]interface{}{"format_version": "bad"},
			allowed: false,
			hard:    []v1beta1.PolicyViolation{violation("hard denied", map[string]string{"query": "data.main.deny", "rule": "hard_test"})},
			soft:    nil,
		},
		{
			name: "soft deny violation",
			cfg: Config{
				SoftDeny: []string{writeDir(t, map[string]string{"soft.rego": softPolicy})},
			},
			input:   map[string]interface{}{"format_version": "warn"},
			allowed: false,
			hard:    nil,
			soft:    []v1beta1.PolicyViolation{violation("soft denied", map[string]string{"query": "data.main.deny", "rule": "soft_test"})},
		},
		{
			name:    "empty config disables both tiers",
			cfg:     Config{},
			input:   map[string]interface{}{"format_version": "anything"},
			allowed: true,
		},
		{
			name: "non-firing policy yields no violations",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
			},
			input:   map[string]interface{}{"format_version": "fine"},
			allowed: true,
		},
		{
			name: "both tiers firing",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
				SoftDeny: []string{writeDir(t, map[string]string{"soft.rego": softPolicy})},
			},
			input:   map[string]interface{}{"format_version": "bad"},
			allowed: false,
			hard:    []v1beta1.PolicyViolation{violation("hard denied", map[string]string{"query": "data.main.deny", "rule": "hard_test"})},
			soft:    nil, // input "bad" does not fire the soft rule
		},
		{
			name: "both tiers firing simultaneously",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"h.rego": `package main

import rego.v1

deny contains {"rule": "h", "msg": "hard"} if {
	input.format_version == "boom"
}
`})},
				SoftDeny: []string{writeDir(t, map[string]string{"s.rego": `package main

import rego.v1

deny contains {"rule": "s", "msg": "soft"} if {
	input.format_version == "boom"
}
`})},
			},
			input:   map[string]interface{}{"format_version": "boom"},
			allowed: false,
			hard:    []v1beta1.PolicyViolation{violation("hard", map[string]string{"query": "data.main.deny", "rule": "h"})},
			soft:    []v1beta1.PolicyViolation{violation("soft", map[string]string{"query": "data.main.deny", "rule": "s"})},
		},
		{
			name: "malformed deny entry is handled by conftest",
			cfg: Config{
				// deny is a set mixing a valid object, an object with an
				// arbitrary extra key (proving passthrough), and a bare string.
				// conftest maps all of them; a non-object (number) is dropped by
				// conftest without a bogus violation or panic.
				HardDeny: []string{writeDir(t, map[string]string{"m.rego": `package main

import rego.v1

deny contains {"rule": "ok", "msg": "fine"} if {
	input.format_version == "mix"
}

deny contains {"severity": "high", "msg": "severe"} if {
	input.format_version == "mix"
}

deny contains "bare message" if {
	input.format_version == "mix"
}

deny contains 42 if {
	input.format_version == "mix"
}
`})},
			},
			input:   map[string]interface{}{"format_version": "mix"},
			allowed: false,
			hard: []v1beta1.PolicyViolation{
				violation("fine", map[string]string{"query": "data.main.deny", "rule": "ok"}),
				violation("severe", map[string]string{"query": "data.main.deny", "severity": "high"}),
				v1beta1.PolicyViolation{Msg: "bare message", Metadata: map[string]string{"query": "data.main.deny"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireConftest(t)
			e, err := New(tt.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			res := evalInput(t, e, tt.input)

			if res.Allowed != tt.allowed {
				t.Fatalf("Allowed = %v, want %v (res: %+v)", res.Allowed, tt.allowed, res)
			}
			// OPA sets are unordered, so compare as multisets, not by index.
			if !sameViolations(res.HardDenies, tt.hard) {
				t.Fatalf("HardDenies = %+v, want %+v", res.HardDenies, tt.hard)
			}
			if !sameViolations(res.SoftDenies, tt.soft) {
				t.Fatalf("SoftDenies = %+v, want %+v", res.SoftDenies, tt.soft)
			}
		})
	}
}

// TestWarningsAreAdvisory proves conftest warn rules are surfaced on
// PolicyEvalResult.Warnings but never affect the verdict: a warnings-only run
// is still Allowed, and warnings ride along without changing Allowed even when
// a deny fires. It also verifies the string-valued warn metadata survives the
// map[string]string unmarshal.
func TestWarningsAreAdvisory(t *testing.T) {
	requireConftest(t)
	warnDir := writeDir(t, map[string]string{"warn.rego": warnPolicy})

	t.Run("warnings-only run is still Allowed", func(t *testing.T) {
		requireConftest(t)
		e, err := New(Config{HardDeny: []string{warnDir}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		res := evalInput(t, e, map[string]interface{}{"format_version": "warn"})
		if !res.Allowed {
			t.Fatalf("Allowed = false, want true: warnings must never block the run (%+v)", res)
		}
		if len(res.HardDenies) != 0 || len(res.SoftDenies) != 0 {
			t.Fatalf("expected no denials, got hard=%+v soft=%+v", res.HardDenies, res.SoftDenies)
		}
		if len(res.Warnings) != 1 {
			t.Fatalf("Warnings = %+v, want exactly 1 entry", res.Warnings)
		}

		// The warning msg and string-valued metadata survive unmarshal.
		w := res.Warnings[0]
		if w.Msg != "warned about X" {
			t.Fatalf("warning Msg = %q, want %q", w.Msg, "warned about X")
		}
		if w.Metadata["rule"] != "warn_test" || w.Metadata["query"] != "data.main.warn" || w.Metadata["severity"] != "low" {
			t.Fatalf("warning metadata mismatch, got %+v", w.Metadata)
		}
	})

	t.Run("warnings ride along with a deny without changing Allowed", func(t *testing.T) {
		requireConftest(t)
		bothDir := writeDir(t, map[string]string{"both.rego": `package main

import rego.v1

warn contains {"rule": "warn_boom", "msg": "warned"} if {
	input.format_version == "boom"
}

deny contains {"rule": "hard_boom", "msg": "hard denied"} if {
	input.format_version == "boom"
}
`})

		e, err := New(Config{HardDeny: []string{bothDir}})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		res := evalInput(t, e, map[string]interface{}{"format_version": "boom"})
		if res.Allowed {
			t.Fatalf("Allowed = true, want false when a deny fires")
		}
		if len(res.HardDenies) != 1 || res.HardDenies[0].Metadata["rule"] != "hard_boom" {
			t.Fatalf("expected the deny to fire, got %+v", res.HardDenies)
		}
		if len(res.Warnings) != 1 || res.Warnings[0].Metadata["rule"] != "warn_boom" {
			t.Fatalf("expected the warn to ride along, got %+v", res.Warnings)
		}
	})

	t.Run("warnings from both tiers are collected", func(t *testing.T) {
		requireConftest(t)
		e, err := New(Config{
			HardDeny: []string{warnDir},
			SoftDeny: []string{writeDir(t, map[string]string{"warn2.rego": `package main

import rego.v1

warn contains {"rule": "soft_warn", "msg": "soft tier warn"} if {
	input.format_version == "warn"
}
`})},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		res := evalInput(t, e, map[string]interface{}{"format_version": "warn"})
		if !res.Allowed {
			t.Fatalf("Allowed = false, want true (%+v)", res)
		}
		if len(res.Warnings) != 2 {
			t.Fatalf("Warnings = %+v, want 2 entries across both tiers", res.Warnings)
		}
	})
}

func TestFailClosed(t *testing.T) {
	requireConftest(t)
	tests := []struct {
		name string
		cfg  Config
		want string // optional substring expected in the error
	}{
		{
			name: "broken rego syntax in hard_deny",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"broken.rego": `package main

import rego.v1

deny contains { if { input.format_version == "x" }`})},
			},
		},
		{
			name: "broken rego syntax in soft_deny",
			cfg: Config{
				SoftDeny: []string{writeDir(t, map[string]string{"broken.rego": `package main

import rego.v1

deny contains {"rule" "message"} if { input.format_version == "x" }`})},
			},
		},
		{
			name: "nonexistent hard_deny path",
			cfg: Config{
				HardDeny: []string{filepath.Join(t.TempDir(), "does-not-exist")},
			},
			want: "no such file or directory",
		},
		{
			name: "nonexistent soft_deny path alongside valid hard tier",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
				SoftDeny: []string{filepath.Join(t.TempDir(), "missing")},
			},
			want: "no such file or directory",
		},
		{
			name: "configured path is a file not a directory",
			cfg: Config{
				SoftDeny: []string{mustWriteFile(t, filepath.Join(t.TempDir(), "not-a-dir"), "hi")},
			},
			want: "is not a directory",
		},
		{
			name: "nonexistent data path",
			cfg: Config{
				HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
				Data:     []string{filepath.Join(t.TempDir(), "missing-data")},
			},
			want: "no such file or directory",
		},
		{
			// conftest fails closed on a policy directory containing no .rego
			// files at all, so an empty configured tier does not silently pass.
			name: "configured tier with empty directory fails closed",
			cfg: Config{
				HardDeny: []string{t.TempDir()},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireConftest(t)
			_, err := New(tt.cfg)
			if err == nil {
				t.Fatal("expected New to fail closed, got nil error")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestCombinedSmokeCheck proves hard_deny and soft_deny are smoke-checked in a
// SINGLE conftest invocation at startup, so a broken bundle in one tier fails
// New regardless of which tier is checked first: with a valid hard tier and a
// syntactically broken soft tier, New must still fail closed.
func TestCombinedSmokeCheck(t *testing.T) {
	requireConftest(t)

	t.Run("broken soft tier with valid hard tier fails New", func(t *testing.T) {
		requireConftest(t)
		_, err := New(Config{
			HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
			SoftDeny: []string{writeDir(t, map[string]string{"broken.rego": `package main

import rego.v1

deny contains {"rule" "message"} if { input.format_version == "x" }`})},
		})
		if err == nil {
			t.Fatal("expected New to fail closed on a broken soft_deny bundle alongside a valid hard tier, got nil error")
		}
	})

	t.Run("both tiers valid constructs with both enabled", func(t *testing.T) {
		requireConftest(t)
		e, err := New(Config{
			HardDeny: []string{writeDir(t, map[string]string{"hard.rego": hardPolicy})},
			SoftDeny: []string{writeDir(t, map[string]string{"soft.rego": softPolicy})},
		})
		if err != nil {
			t.Fatalf("New with both valid tiers: %v", err)
		}
		// Both tiers fire on their own inputs, proving both are enabled.
		res := evalInput(t, e, map[string]interface{}{"format_version": "bad"})
		if res.Allowed || len(res.HardDenies) != 1 {
			t.Fatalf("expected hard_deny to fire, got %+v", res)
		}
		res = evalInput(t, e, map[string]interface{}{"format_version": "warn"})
		if res.Allowed || len(res.SoftDenies) != 1 {
			t.Fatalf("expected soft_deny to fire, got %+v", res)
		}
	})
}

// TestCustomNamespace exercises the configurable namespace field: a bundle
// declaring `package foo` is evaluated via --namespace foo, querying
// data.foo.deny.
func TestCustomNamespace(t *testing.T) {
	requireConftest(t)
	dir := writeDir(t, map[string]string{"foo.rego": `package foo

import rego.v1

deny contains {"rule": "nsfoo", "msg": "foo denied"} if {
	input.format_version == "bad"
}
`})

	e, err := New(Config{
		Namespace: "foo",
		HardDeny:  []string{dir},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := evalInput(t, e, map[string]interface{}{"format_version": "bad"})
	if res.Allowed || len(res.HardDenies) != 1 {
		t.Fatalf("expected custom namespace deny to fire, got %+v", res)
	}
	v := res.HardDenies[0]
	if v.Msg != "foo denied" || v.Metadata["rule"] != "nsfoo" || v.Metadata["query"] != "data.foo.deny" {
		t.Fatalf("expected custom namespace violation, got %+v", v)
	}
}

// TestDataMerge proves a `data` config directory is passed to conftest via
// --data and its JSON files are loaded as DATA documents: the policy reads
// data.allowed_owners (not input.allowed_owners), and whether the rule fires
// depends on the data file, not on anything carried in the input.
func TestDataMerge(t *testing.T) {
	requireConftest(t)
	// The allowlist lives ONLY in the --data file. The input carries just the
	// resource's owner tag; whether that tag is allowed is decided solely by
	// the data document, proving the --data path is what drives the outcome.
	policyDir := writeDir(t, map[string]string{"owner.rego": `package main

import rego.v1

deny contains {"rule": "owner", "msg": "not an allowed owner"} if {
	owner := input.owner_tag
	not owner in data.allowed_owners
}
`})
	dataDir := writeDir(t, map[string]string{
		"allowlist.json": `{"allowed_owners": ["alice", "bob"]}`,
	})

	e, err := New(Config{
		HardDeny: []string{policyDir},
		Data:     []string{dataDir},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// An owner NOT in the data allowlist fires the deny.
	res := evalInput(t, e, map[string]interface{}{"owner_tag": "carol"})
	if res.Allowed || len(res.HardDenies) != 1 || res.HardDenies[0].Metadata["rule"] != "owner" {
		t.Fatalf("expected non-allowlisted owner to be denied via --data, got %+v", res)
	}

	// An owner IN the data allowlist does not fire.
	res = evalInput(t, e, map[string]interface{}{"owner_tag": "bob"})
	if !res.Allowed {
		t.Fatalf("expected allowlisted owner to pass via --data, got %+v", res)
	}
}

// TestMetadataPassthrough proves an arbitrary extra key on a deny element
// appears verbatim in the violation metadata alongside conftest's query/rule.
func TestMetadataPassthrough(t *testing.T) {
	requireConftest(t)
	dir := writeDir(t, map[string]string{"m.rego": `package main

import rego.v1

deny contains {"rule": "r", "msg": "denied", "severity": "high", "owner": "alice"} if {
	input.format_version == "bad"
}
`})

	e, err := New(Config{HardDeny: []string{dir}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := evalInput(t, e, map[string]interface{}{"format_version": "bad"})
	if len(res.HardDenies) != 1 {
		t.Fatalf("expected one violation, got %+v", res)
	}
	md := res.HardDenies[0].Metadata
	if md["rule"] != "r" || md["query"] != "data.main.deny" || md["severity"] != "high" || md["owner"] != "alice" {
		t.Fatalf("metadata passthrough mismatch, got %+v", md)
	}
}

// TestEvalStructInput proves a typed-struct input works directly: the runner
// passes the raw *tfjson.Plan (a struct), which is marshaled to JSON so a
// policy can read `input.format_version`.
func TestEvalStructInput(t *testing.T) {
	requireConftest(t)
	type plan struct {
		FormatVersion string `json:"format_version"`
	}

	dir := writeDir(t, map[string]string{"struct.rego": `package main

import rego.v1

deny contains {"rule": "bad-version", "msg": "unexpected format_version"} if {
	input.format_version == "0.2"
}
`})

	e, err := New(Config{HardDeny: []string{dir}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := evalInput(t, e, plan{FormatVersion: "0.2"})
	if res.Allowed || len(res.HardDenies) != 1 {
		t.Fatalf("expected struct input to fire deny, got %+v", res)
	}
	v := res.HardDenies[0]
	if v.Msg != "unexpected format_version" || v.Metadata["rule"] != "bad-version" || v.Metadata["query"] != "data.main.deny" {
		t.Fatalf("unexpected violation from struct input, got %+v", v)
	}

	res = evalInput(t, e, plan{FormatVersion: "0.1"})
	if !res.Allowed {
		t.Fatalf("expected no violations for non-matching struct input, got %+v", res)
	}
}

func TestMultiplePathsPerTier(t *testing.T) {
	requireConftest(t)
	dirA := writeDir(t, map[string]string{"a.rego": hardPolicy})
	dirB := writeDir(t, map[string]string{"b.rego": softPolicy})

	e, err := New(Config{
		HardDeny: []string{dirA, dirB},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := evalInput(t, e, map[string]interface{}{"format_version": "bad"})
	if res.Allowed || len(res.HardDenies) != 1 {
		t.Fatalf("expected hard violation from first path, got %+v", res)
	}
	res = evalInput(t, e, map[string]interface{}{"format_version": "warn"})
	if res.Allowed || len(res.HardDenies) != 1 || res.HardDenies[0].Metadata["rule"] != "soft_test" {
		t.Fatalf("expected violation from second path in same tier, got %+v", res)
	}
}

// TestDisabledTierSkipped proves a tier with no configured paths is skipped
// without invoking conftest: an engine with no policies is Allowed with no
// error even when the conftest binary is absent.
func TestDisabledTierSkipped(t *testing.T) {
	// Point CONFTEST_BIN at a nonexistent binary: a disabled (empty) engine must
	// still construct and evaluate fine because no tier invokes conftest.
	t.Setenv("CONFTEST_BIN", filepath.Join(t.TempDir(), "no-such-conftest"))

	e, err := New(Config{})
	if err != nil {
		t.Fatalf("New with disabled tiers: %v", err)
	}
	res := evalInput(t, e, map[string]interface{}{"format_version": "bad"})
	if !res.Allowed || len(res.HardDenies) != 0 || len(res.SoftDenies) != 0 {
		t.Fatalf("expected disabled engine to be Allowed, got %+v", res)
	}
}

func TestMissingConftestBinary(t *testing.T) {
	// Any enabled tier requires the conftest binary; a nonexistent CONFTEST_BIN
	// must make New fail closed.
	t.Setenv("CONFTEST_BIN", filepath.Join(t.TempDir(), "no-such-conftest"))

	dir := writeDir(t, map[string]string{"hard.rego": hardPolicy})
	if _, err := New(Config{HardDeny: []string{dir}}); err == nil {
		t.Fatal("expected New to fail closed on missing conftest binary")
	}
}

func TestConcurrentEval(t *testing.T) {
	requireConftest(t)
	dir := writeDir(t, map[string]string{"hard.rego": hardPolicy})
	e, err := New(Config{HardDeny: []string{dir}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const workers = 16
	const iterations = 20
	start := make(chan struct{})
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			for j := 0; j < iterations; j++ {
				res, err := e.Eval(context.Background(), map[string]interface{}{"format_version": "bad"})
				if err != nil {
					errs <- err
					return
				}
				if res.Allowed || len(res.HardDenies) != 1 {
					errs <- errors.New("expected hard denial under concurrency")
					return
				}
			}
			errs <- nil
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// TestConfigParseSanity locks in the YAML surface of Config (namespace, tier
// paths, and data dirs) so a config typo surfaces as a parse/field issue.
func TestConfigParseSanity(t *testing.T) {
	y := `
policies:
  namespace: foo
  hard_deny:
    - /p/hard
  soft_deny:
    - /p/soft
  data:
    - /p/data
`
	var cfg struct {
		Policies Config `yaml:"policies"`
	}
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Policies.Namespace != "foo" {
		t.Fatalf("Namespace = %q, want foo", cfg.Policies.Namespace)
	}
	if got := cfg.Policies.HardDeny; !reflect.DeepEqual(got, []string{"/p/hard"}) {
		t.Fatalf("HardDeny = %v", got)
	}
	if got := cfg.Policies.SoftDeny; !reflect.DeepEqual(got, []string{"/p/soft"}) {
		t.Fatalf("SoftDeny = %v", got)
	}
	if got := cfg.Policies.Data; !reflect.DeepEqual(got, []string{"/p/data"}) {
		t.Fatalf("Data = %v", got)
	}
}

// sameViolations reports whether got and want contain the same violations,
// ignoring order (deny sets are unordered).
func sameViolations(got, want []v1beta1.PolicyViolation) bool {
	if len(got) != len(want) {
		return false
	}
	remaining := make([]v1beta1.PolicyViolation, len(want))
	copy(remaining, want)
	for _, g := range got {
		found := false
		for i, w := range remaining {
			if reflect.DeepEqual(w, g) {
				remaining = append(remaining[:i], remaining[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func mustWriteFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
