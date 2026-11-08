package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		tag   string
		want  version
		valid bool
	}{
		{tag: "1.2.3", want: version{major: 1, minor: 2, patch: 3}, valid: true},
		{tag: "0.0.0", want: version{}, valid: true},
		{tag: "0.1.0", want: version{minor: 1}, valid: true},
		{tag: "v1.2.3"},
		{tag: "01.2.3"},
		{tag: "+1.2.3"},
		{tag: "1.2"},
		{tag: "1.2.3-rc.1"},
		{tag: "1.2.3+build.4"},
		{tag: "release-1.2.3"},
	}

	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			got, valid := parseVersion(test.tag)
			if valid != test.valid || (valid && got != test.want) {
				t.Fatalf("parseVersion(%q) = (%+v, %t), want (%+v, %t)", test.tag, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestClassifyCommit(t *testing.T) {
	tests := []struct {
		message string
		want    bumpLevel
	}{
		{message: "fix: handle empty input", want: patchBump},
		{message: "fix(api): handle empty input", want: patchBump},
		{message: "FEAT(api): add lookup", want: minorBump},
		{message: "feat(api): add lookup", want: minorBump},
		{message: "feat(x): add lookup", want: minorBump},
		{message: "feat: add lookup", want: minorBump},
		{message: "feat!: remove legacy mode", want: majorBump},
		{message: "feat(api)!: remove legacy mode", want: majorBump},
		{message: "refactor!: change the API", want: majorBump},
		{message: "chore!: change deployment behavior", want: majorBump},
		{message: "docs: update examples\n\nBREAKING CHANGE: old examples no longer apply", want: majorBump},
		{message: "chore(ci): change deployment behavior\n\nBREAKING-CHANGE: old configuration no longer applies", want: majorBump},
		{message: "docs: update examples\n\nRefs: #12\nBREAKING CHANGE: old examples no longer apply", want: majorBump},
		{message: "chore: update dependencies", want: noBump},
		{message: "ci(pipeline): update workflow", want: noBump},
		{message: "perf(parser): reduce allocations", want: noBump},
		{message: "patch(ci): update workflow", want: noBump},
		{message: "123type: invalid type", want: noBump},
		{message: "fixed a bug", want: noBump},
		{message: "feat:", want: noBump},
		{message: "feat(scope", want: noBump},
		{message: "feat(): add lookup", want: noBump},
		{message: "feat:missing space", want: noBump},
		{message: "feat! remove legacy mode", want: noBump},
		{message: "chore: update docs\nBREAKING CHANGE: not separated from header", want: noBump},
		{message: "chore: update docs\n\nThis text mentions BREAKING CHANGE: in its body", want: noBump},
		{message: "chore: update docs\n\nRefs: #12\nBody text before the marker\nBREAKING CHANGE: not a footer", want: noBump},
		{message: "chore: update docs\n\nBREAKING CHANGE:   ", want: noBump},
		{message: "not conventional\n\nBREAKING CHANGE: ignored for invalid header", want: noBump},
	}

	for _, test := range tests {
		t.Run(strings.Split(test.message, "\n")[0], func(t *testing.T) {
			if got := classifyCommit(test.message); got != test.want {
				t.Fatalf("classifyCommit(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}

func TestChangelogEmojiUsesConventionalCommitSemantics(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{message: "fix(parser): handle empty input", want: "🐛"},
		{message: "feat(api): add lookup", want: "✨"},
		{message: "perf: tune startup", want: "⚡"},
		{message: "chore!: remove old config", want: "💥"},
		{message: "docs: update guide\n\nBREAKING CHANGE: old guide is incompatible", want: "💥"},
		{message: "feat:missing space", want: "📦"},
	}
	for _, test := range tests {
		t.Run(strings.Split(test.message, "\n")[0], func(t *testing.T) {
			if got := changelogEmoji(test.message); got != test.want {
				t.Fatalf("changelogEmoji(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
}

func TestVersionBumpPrecedenceAndOverflow(t *testing.T) {
	tests := []struct {
		name  string
		start version
		level bumpLevel
		want  version
	}{
		{name: "patch", start: version{major: 1, minor: 2, patch: 3}, level: patchBump, want: version{major: 1, minor: 2, patch: 4}},
		{name: "minor resets patch", start: version{major: 1, minor: 2, patch: 3}, level: minorBump, want: version{major: 1, minor: 3}},
		{name: "major resets minor and patch", start: version{major: 1, minor: 2, patch: 3}, level: majorBump, want: version{major: 2}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.start.bump(test.level)
			if err != nil {
				t.Fatalf("bump returned unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("bump = %+v, want %+v", got, test.want)
			}
		})
	}

	max := ^uint64(0)
	if _, err := (version{patch: max}).bump(patchBump); err == nil {
		t.Fatal("patch overflow should return an error")
	}
	if _, err := (version{major: max}).bump(majorBump); err == nil {
		t.Fatal("major overflow should return an error")
	}
}

func TestRunCreatesInitialVersionAndThenNoOps(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: handle empty input")

	var output bytes.Buffer
	if err := runWithOptions(repo, "v", false, &output); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "v0.0.1")

	output.Reset()
	if err := runWithOptions(repo, "v", false, &output); err != nil {
		t.Fatalf("second run returned error: %v", err)
	}
	if strings.Contains(output.String(), "Created tag") {
		t.Fatalf("second run unexpectedly created a tag: %s", output.String())
	}
}

func TestRunDryRunDoesNotCreateTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "feat: add search")

	var output bytes.Buffer
	if err := runWithOptions(repo, "v", true, &output); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagMissing(t, repo, "v0.1.0")
	if got, want := output.String(), "v0.1.0\n"; got != want {
		t.Fatalf("dry-run output = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly wrote CHANGELOG.md (stat error: %v)", err)
	}
}

func TestRunUsesHighestReachableStableTagAndOnlyNewCommits(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial change")
	git(t, repo, "tag", "1.2.3")
	commit(t, repo, "docs: update usage")
	git(t, repo, "tag", "v1.2.2")
	git(t, repo, "tag", "v9.0.0-rc.1")
	commit(t, repo, "feat: add a feature")

	if err := runWithOptions(repo, "v", false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "v1.3.0")
}

func TestRunDefaultsToEmptyPrefixAndUsesOnlyUnprefixedTags(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial release")
	git(t, repo, "tag", "v9.0.0")
	git(t, repo, "tag", "1.2.3")
	commit(t, repo, "feat: new feature")

	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "1.3.0")
	assertTagMissing(t, repo, "v10.0.0")
}

func TestRunUsesConventionalBumpsForInitialAndSubsequentReleases(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "docs: update readme")
	commit(t, repo, "perf: reduce allocations")
	commit(t, repo, "fix: handle empty input")
	commit(t, repo, "feat(search): add lookup")
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("initial conventional release returned error: %v", err)
	}
	assertTagExists(t, repo, "0.1.0")

	commit(t, repo, "fix: handle another edge case")
	commit(t, repo, "feat: add another feature")
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("subsequent conventional release returned error: %v", err)
	}
	assertTagExists(t, repo, "0.2.0")
}

func TestAutomaticBumpUsesHighestCommitLevelAndDefaultsToNoPrefix(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: add dashes")
	commit(t, repo, "feat!: add dashes")
	commit(t, repo, "adsad")

	if err := runWithChangelog(repo, "", io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "1.0.0")
	assertTagMissing(t, repo, "v1.0.0")

	changelog, err := os.ReadFile(filepath.Join(repo, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	contents := string(changelog)
	if !strings.Contains(contents, "🐛 fix: add dashes") || !strings.Contains(contents, "💥 feat!: add dashes") {
		t.Fatalf("CHANGELOG.md is missing release-worthy commits:\n%s", contents)
	}
	if strings.Contains(contents, "adsad") {
		t.Fatalf("CHANGELOG.md contains an invalid commit:\n%s", contents)
	}
}

func TestAutomaticBumpUsesHighestCommitLevelAfterTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial release")
	git(t, repo, "tag", "0.4.2")
	commit(t, repo, "feat!: add dashes")
	commit(t, repo, "adsad")
	commit(t, repo, "fix: add dashes")

	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "1.0.0")
}

func TestRunDoesNotCreateTagWithoutRecognizedCommits(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "docs: describe the project")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump]\ncommands = [\"touch hook-ran.txt\"]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}

	var output bytes.Buffer
	if err := runWithOptions(repo, "v", false, &output); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagMissing(t, repo, "v0.0.1")
	if !strings.Contains(output.String(), "👍") {
		t.Fatalf("successful no-op message is missing thumbs-up: %q", output.String())
	}
	if _, err := os.Stat(filepath.Join(repo, "hook-ran.txt")); !os.IsNotExist(err) {
		t.Fatalf("no-op unexpectedly ran post-bump hook (stat error: %v)", err)
	}
}

func TestRunWritesDelimitedChangelogWithOnlySemverCommits(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial change")
	if err := runWithChangelog(repo, "v", io.Discard); err != nil {
		t.Fatalf("initial run returned error: %v", err)
	}
	commit(t, repo, "feat(search): add lookup")
	commit(t, repo, "ci: update workflow")
	commit(t, repo, "chore: update release tooling")
	commit(t, repo, "feat:missing space")
	commit(t, repo, "docs: remove old API\n\nBREAKING CHANGE: callers must use the new API")
	commit(t, repo, "fix: handle missing lookup")
	var output bytes.Buffer
	if err := runWithChangelog(repo, "v", &output); err != nil {
		t.Fatalf("release run returned error: %v", err)
	}
	if got := git(t, repo, "tag", "--list", "v1.0.0"); got != "v1.0.0" {
		t.Fatalf("expected v1.0.0; tags output %q, run output %q", got, output.String())
	}

	changelog, err := os.ReadFile(filepath.Join(repo, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	contents := string(changelog)
	for _, expected := range []string{
		"<!-- gittan:release-start v1.0.0 -->",
		"🐛 fix: handle missing lookup",
		"✨ feat(search): add lookup",
		"💥 docs: remove old API",
		"<!-- gittan:release-end v1.0.0 -->",
		"<!-- gittan:release-start v0.0.1 -->",
	} {
		if !strings.Contains(contents, expected) {
			t.Errorf("CHANGELOG.md does not contain %q:\n%s", expected, contents)
		}
	}
	for _, excluded := range []string{"ci: update workflow", "chore: update release tooling", "feat:missing space"} {
		if strings.Contains(contents, excluded) {
			t.Errorf("CHANGELOG.md contains non-release commit %q:\n%s", excluded, contents)
		}
	}
	if strings.Index(contents, "v1.0.0") > strings.Index(contents, "v0.0.1") {
		t.Fatalf("new release was not prepended:\n%s", contents)
	}
}

func TestRunSupportsScopedFixAndFeatureCommits(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial change")
	if err := runWithChangelog(repo, "v", io.Discard); err != nil {
		t.Fatalf("initial run returned error: %v", err)
	}

	commit(t, repo, "fix(ci): update workflow")
	if err := runWithChangelog(repo, "v", io.Discard); err != nil {
		t.Fatalf("scoped fix run returned error: %v", err)
	}
	assertTagExists(t, repo, "v0.0.2")

	commit(t, repo, "feat(x): add release reporting")
	if err := runWithChangelog(repo, "v", io.Discard); err != nil {
		t.Fatalf("scoped feature run returned error: %v", err)
	}
	assertTagExists(t, repo, "v0.1.0")

	changelog, err := os.ReadFile(filepath.Join(repo, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	for _, expected := range []string{"🐛 fix(ci): update workflow", "✨ feat(x): add release reporting"} {
		if !strings.Contains(string(changelog), expected) {
			t.Errorf("CHANGELOG.md does not contain %q:\n%s", expected, changelog)
		}
	}
}

func TestCLIOptionsSupportAutomaticAndManualModes(t *testing.T) {
	options, err := parseCLIOptions([]string{"--auto", "--repo", "project", "--prefix="}, io.Discard)
	if err != nil {
		t.Fatalf("parseCLIOptions returned error: %v", err)
	}
	if options.repo != "project" || options.prefix != "" || options.dryRun || !options.auto || !options.bump {
		t.Fatalf("unexpected CLI options: %+v", options)
	}
	options, err = parseCLIOptions([]string{"--auto"}, io.Discard)
	if err != nil || options.prefix != "" {
		t.Fatalf("default prefix should be empty: options=%+v error=%v", options, err)
	}
	options, err = parseCLIOptions([]string{"--auto", "--prefix", "v"}, io.Discard)
	if err != nil || options.prefix != "v" {
		t.Fatalf("explicit v prefix was not retained: options=%+v error=%v", options, err)
	}
	options, err = parseCLIOptions([]string{"--minor", "--dry-run"}, io.Discard)
	if err != nil {
		t.Fatalf("manual bump options returned error: %v", err)
	}
	if options.auto || options.level != minorBump || !options.dryRun {
		t.Fatalf("unexpected manual CLI options: %+v", options)
	}
	options, err = parseCLIOptions(nil, io.Discard)
	if err != nil || !options.auto {
		t.Fatalf("direct invocation should retain automatic mode: options=%+v error=%v", options, err)
	}
	for _, args := range [][]string{{"bump", "--auto"}, {"--auto", "--major"}, {"--major", "--minor"}, {"--dry-run"}, {"--auto", "--dry-run", "--changelog", "ON"}} {
		if _, err := parseCLIOptions(args, io.Discard); err == nil {
			t.Errorf("parseCLIOptions(%q) should reject legacy syntax or conflicting options", args)
		}
	}
	var errorOutput bytes.Buffer
	if _, err := parseCLIOptions([]string{"bump", "--auto"}, &errorOutput); err == nil {
		t.Fatal("legacy bump subcommand should return an error")
	}
	if !strings.Contains(errorOutput.String(), "unexpected positional arguments") {
		t.Fatalf("validation error was not written: %q", errorOutput.String())
	}
}

func TestCLIShorthandFlags(t *testing.T) {
	options, err := parseCLIOptions([]string{"-a", "-r", "custom-repo", "-p", "v"}, io.Discard)
	if err != nil {
		t.Fatalf("parseCLIOptions with shorthands returned error: %v", err)
	}
	if options.repo != "custom-repo" || options.prefix != "v" || options.dryRun || !options.auto || !options.bump {
		t.Fatalf("unexpected CLI options for -a -r -p: %+v", options)
	}

	options, err = parseCLIOptions([]string{"-a", "-d"}, io.Discard)
	if err != nil {
		t.Fatalf("parseCLIOptions with -a -d returned error: %v", err)
	}
	if !options.auto || !options.dryRun || !options.bump {
		t.Fatalf("unexpected CLI options for -a -d: %+v", options)
	}

	options, err = parseCLIOptions([]string{"--major", "-d"}, io.Discard)
	if err != nil {
		t.Fatalf("parseCLIOptions with --major -d returned error: %v", err)
	}
	if options.auto || options.level != majorBump || !options.dryRun {
		t.Fatalf("unexpected CLI options for --major -d: %+v", options)
	}

	options, err = parseCLIOptions([]string{"-c", "ON"}, io.Discard)
	if err != nil || options.bump || options.changelogMode != changelogOn {
		t.Fatalf("parseCLIOptions with -c ON returned %+v, %v; want standalone ON", options, err)
	}

	options, err = parseCLIOptions([]string{"-c", "full"}, io.Discard)
	if err != nil || options.bump || options.changelogMode != changelogFull {
		t.Fatalf("parseCLIOptions with -c full returned %+v, %v; want standalone FULL", options, err)
	}

	for _, args := range [][]string{{"-d"}, {"-a", "-d", "-c", "ON"}, {"-a", "--major"}} {
		if _, err := parseCLIOptions(args, io.Discard); err == nil {
			t.Errorf("parseCLIOptions(%q) should reject invalid shorthand combination", args)
		}
	}
}

func TestCLIChangelogModesAndStandaloneSelection(t *testing.T) {
	tests := []struct {
		value changelogMode
		args  []string
	}{
		{value: changelogOff, args: []string{"--changelog", "OFF"}},
		{value: changelogOn, args: []string{"--changelog", "on"}},
		{value: changelogFull, args: []string{"--changelog", "FULL"}},
	}
	for _, test := range tests {
		options, err := parseCLIOptions(test.args, io.Discard)
		if err != nil || options.bump || options.changelogMode != test.value {
			t.Errorf("parseCLIOptions(%q) = %+v, %v; want standalone %s mode", test.args, options, err, test.value)
		}
	}
	options, err := parseCLIOptions([]string{"--major", "--changelog", "FULL"}, io.Discard)
	if err != nil || !options.bump || options.changelogMode != changelogFull {
		t.Fatalf("bump with changelog mode = %+v, %v; want combined release", options, err)
	}
	if _, err := parseCLIOptions([]string{"--changelog", "sometimes"}, io.Discard); err == nil {
		t.Fatal("unsupported changelog mode should fail")
	}
}

func TestStandaloneChangelogOnPrependsOnlyReleaseCommitsSinceTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: already released")
	git(t, repo, "tag", "0.1.0")
	commit(t, repo, "feat: add fresh feature")
	commit(t, repo, "chore: update local tooling")
	commit(t, repo, "perf: tune startup")
	commit(t, repo, "feat:missing space")
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("previous entry\n"), 0o600); err != nil {
		t.Fatalf("write existing changelog: %v", err)
	}
	var output bytes.Buffer
	if err := runStandaloneChangelog(repo, "", changelogOn, &output); err != nil {
		t.Fatalf("standalone ON changelog returned error: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(repo, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	text := string(contents)
	for _, expected := range []string{"Unreleased", "✨ feat: add fresh feature", "previous entry"} {
		if !strings.Contains(text, expected) {
			t.Errorf("ON changelog is missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "already released") || strings.Contains(text, "chore: update local tooling") ||
		strings.Contains(text, "perf: tune startup") || strings.Contains(text, "feat:missing space") {
		t.Fatalf("ON changelog included a non-release commit:\n%s", text)
	}
	if got := git(t, repo, "tag", "--list"); got != "0.1.0" {
		t.Fatalf("standalone changelog unexpectedly changed tags: %q", got)
	}
	if !strings.Contains(output.String(), "👍") {
		t.Fatalf("standalone changelog success message lacks thumbs-up: %q", output.String())
	}
}

func TestStandaloneChangelogFullReplacesWithReleaseCommitSubjectsAndFestiveEmoji(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: first historical change")
	git(t, repo, "tag", "0.0.1")
	commit(t, repo, "chore: historical maintenance")
	commit(t, repo, "feat: recent feature")
	commit(t, repo, "ci: update workflow")
	commit(t, repo, "feat:missing space")
	commit(t, repo, "docs: remove old API\n\nBREAKING CHANGE: callers must use the new API")
	path := filepath.Join(repo, "CHANGELOG.md")
	if err := os.WriteFile(path, []byte("stale changelog contents\n"), 0o600); err != nil {
		t.Fatalf("write old changelog: %v", err)
	}
	if err := runStandaloneChangelog(repo, "", changelogFull, io.Discard); err != nil {
		t.Fatalf("standalone FULL changelog returned error: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	text := string(contents)
	for _, expected := range []string{"History", "fix: first historical change", "feat: recent feature", "docs: remove old API"} {
		if !strings.Contains(text, expected) {
			t.Errorf("FULL changelog is missing %q:\n%s", expected, text)
		}
	}
	for _, excluded := range []string{"chore: historical maintenance", "ci: update workflow", "feat:missing space"} {
		if strings.Contains(text, excluded) {
			t.Errorf("FULL changelog contains non-release commit %q:\n%s", excluded, text)
		}
	}
	if strings.Contains(text, "stale changelog contents") {
		t.Fatalf("FULL changelog did not replace previous contents:\n%s", text)
	}
	festive := false
	for _, emoji := range []string{"🎉", "🌟", "🍀", "🎊", "🎁", "🌈", "🪅"} {
		festive = festive || strings.Contains(text, "History "+emoji)
	}
	if !festive {
		t.Fatalf("FULL changelog lacks a festive emoji in its heading:\n%s", text)
	}
}

func TestChangelogOffDoesNotModifyFileAndDryRunRules(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: make a release")
	path := filepath.Join(repo, "CHANGELOG.md")
	if err := os.WriteFile(path, []byte("keep this\n"), 0o600); err != nil {
		t.Fatalf("write changelog: %v", err)
	}
	if err := runStandaloneChangelog(repo, "", changelogOff, io.Discard); err != nil {
		t.Fatalf("OFF changelog returned error: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "keep this\n" {
		t.Fatalf("OFF mode changed the changelog: %q, %v", got, err)
	}
	for _, args := range [][]string{{"--auto", "--dry-run", "--changelog", "OFF"}, {"--dry-run"}} {
		if _, err := parseCLIOptions(args, io.Discard); err == nil {
			t.Errorf("parseCLIOptions(%q) should reject dry-run usage", args)
		}
	}
}

func TestBumpFullChangelogReplacesFileWithReleaseCommits(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: first release change")
	git(t, repo, "tag", "0.1.0")
	commit(t, repo, "chore: update release tooling")
	commit(t, repo, "ci: update workflow")
	commit(t, repo, "feat:missing space")
	commit(t, repo, "docs: remove old API\n\nBREAKING CHANGE: callers must use the new API")
	commit(t, repo, "fix: latest release change")
	path := filepath.Join(repo, "CHANGELOG.md")
	if err := os.WriteFile(path, []byte("old changelog entry\n"), 0o600); err != nil {
		t.Fatalf("write existing changelog: %v", err)
	}
	options, err := parseCLIOptions([]string{"--patch", "--repo", repo, "--changelog", "FULL"}, io.Discard)
	if err != nil {
		t.Fatalf("parse release options: %v", err)
	}
	if err := runWithReleaseOptions(options.repo, options.prefix, options.dryRun, options.auto, options.level, options.overrides, io.Discard); err != nil {
		t.Fatalf("release with FULL changelog returned error: %v", err)
	}
	assertTagExists(t, repo, "0.1.1")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	text := string(contents)
	for _, expected := range []string{"0.1.1", "fix: first release change", "docs: remove old API", "fix: latest release change"} {
		if !strings.Contains(text, expected) {
			t.Errorf("FULL release changelog is missing %q:\n%s", expected, text)
		}
	}
	for _, excluded := range []string{"chore: update release tooling", "ci: update workflow", "feat:missing space"} {
		if strings.Contains(text, excluded) {
			t.Errorf("FULL release changelog contains non-release commit %q:\n%s", excluded, text)
		}
	}
	if strings.Contains(text, "old changelog entry") {
		t.Fatalf("FULL release changelog retained old contents:\n%s", text)
	}
}

func TestManualBumpsCreateInitialVersionsWithoutCommitParsing(t *testing.T) {
	tests := []struct {
		name  string
		level bumpLevel
		want  string
	}{
		{name: "patch", level: patchBump, want: "v0.0.1"},
		{name: "minor", level: minorBump, want: "v0.1.0"},
		{name: "major", level: majorBump, want: "v1.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newGitRepo(t)
			commit(t, repo, "docs: no automatic release")
			var output bytes.Buffer
			if err := runWithReleaseOptions(repo, "v", false, false, test.level, changelogOverride(), &output); err != nil {
				t.Fatalf("manual bump returned error: %v", err)
			}
			assertTagExists(t, repo, test.want)
			if !strings.Contains(output.String(), "👍 Created tag "+test.want) {
				t.Fatalf("success message missing or incorrect: %q", output.String())
			}
			contents, err := os.ReadFile(filepath.Join(repo, "CHANGELOG.md"))
			if err != nil {
				t.Fatalf("read CHANGELOG.md: %v", err)
			}
			if !strings.Contains(string(contents), "<!-- gittan:release-start "+test.want+" -->") || !strings.Contains(string(contents), "## 🚀 "+test.want) {
				t.Fatalf("manual bump did not create its changelog entry: %s", contents)
			}
			if strings.Contains(string(contents), "docs: no automatic release") {
				t.Fatalf("manual bump unexpectedly parsed commit history: %s", contents)
			}
		})
	}
}

func TestManualBumpUsesLatestMatchingPrefixTagAndRunsHooks(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "docs: no automatic release")
	git(t, repo, "tag", "v2.3.4")
	git(t, repo, "tag", "release99.0.0")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump]\ncommands = [\"printf '%s' \\\"$GITTAN_TAG\\\" > hook-ran.txt\"]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	var output bytes.Buffer
	if err := runWithBumpOptions(repo, "v", false, false, minorBump, &output); err != nil {
		t.Fatalf("manual bump returned error: %v", err)
	}
	assertTagExists(t, repo, "v2.4.0")
	assertTagMissing(t, repo, "release2.4.0")
	if got, err := os.ReadFile(filepath.Join(repo, "hook-ran.txt")); err != nil || string(got) != "v2.4.0" {
		t.Fatalf("manual bump hook result = %q, %v; want v2.4.0", got, err)
	}
}

func TestManualDryRunPrintsOnlyVersionAndHasNoSideEffects(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "docs: no automatic release")
	changelogPath := filepath.Join(repo, "CHANGELOG.md")
	if err := os.WriteFile(changelogPath, []byte("existing changelog\n"), 0o600); err != nil {
		t.Fatalf("write CHANGELOG.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump\ninvalid"), 0o600); err != nil {
		t.Fatalf("write invalid gittan.toml: %v", err)
	}
	var output bytes.Buffer
	if err := runWithBumpOptions(repo, "v", true, false, minorBump, &output); err != nil {
		t.Fatalf("manual dry-run returned error: %v", err)
	}
	if got, want := output.String(), "v0.1.0\n"; got != want {
		t.Fatalf("dry-run output = %q, want %q", got, want)
	}
	assertTagMissing(t, repo, "v0.1.0")
	if got, err := os.ReadFile(changelogPath); err != nil || string(got) != "existing changelog\n" {
		t.Fatalf("dry-run changed CHANGELOG.md: contents=%q error=%v", got, err)
	}
}

func TestAutomaticDryRunNoOpPrintsNothing(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "chore: no release")
	var output bytes.Buffer
	if err := run(repo, true, &output); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("automatic dry-run no-op output = %q, want empty output", output.String())
	}
}

func TestManualBumpOverflowFailsWithoutCreatingRelease(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "docs: no automatic release")
	git(t, repo, "tag", "v0.0.18446744073709551615")
	if err := runWithBumpOptions(repo, "v", false, false, patchBump, io.Discard); err == nil {
		t.Fatal("manual patch bump at maximum version should fail")
	}
	assertTagMissing(t, repo, "v0.0.18446744073709551616")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("overflow unexpectedly wrote CHANGELOG.md (stat error: %v)", err)
	}
}

func TestRunExecutesConfiguredPostBumpCommandsInOrder(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	config := "[general]\nupdate-changelog = true\n\n[post-bump]\ncommands = [\"test -f CHANGELOG.md && git rev-parse --verify refs/tags/$GITTAN_TAG >/dev/null && printf first > hook-order.txt\", \"printf second >> hook-order.txt\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}

	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repo, "hook-order.txt"))
	if err != nil {
		t.Fatalf("read hook-order.txt: %v", err)
	}
	if string(got) != "firstsecond" {
		t.Fatalf("hook commands ran out of order or did not finish: %q", got)
	}
}

func TestRunUsesPostBumpShellFallbackOnlyWithoutTOML(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	hookDir := filepath.Join(repo, ".gittan")
	if err := os.Mkdir(hookDir, 0o700); err != nil {
		t.Fatalf("create .gittan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "post-bump.sh"), []byte("printf '%s' %TAG% > fallback-ran.txt\n"), 0o700); err != nil {
		t.Fatalf("write fallback hook: %v", err)
	}
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "fallback-ran.txt")); err != nil || string(got) != "0.0.1" {
		t.Fatalf("fallback hook result = %q, %v; want 0.0.1", got, err)
	}
}

func TestTOMLConfigurationTakesPrecedenceOverFallback(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	hookDir := filepath.Join(repo, ".gittan")
	if err := os.Mkdir(hookDir, 0o700); err != nil {
		t.Fatalf("create .gittan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "post-bump.sh"), []byte("touch fallback-ran.txt\n"), 0o700); err != nil {
		t.Fatalf("write fallback hook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "fallback-ran.txt")); !os.IsNotExist(err) {
		t.Fatalf("fallback hook ran despite existing TOML (stat error: %v)", err)
	}
}

func TestRunReportsHookFailureAfterCreatingRelease(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[general]\nupdate-changelog = true\n\n[post-bump]\ncommands = [\"printf hook-output; exit 1\"]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}

	err := run(repo, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "tag 0.0.1 and CHANGELOG.md were created") || !strings.Contains(err.Error(), "hook-output") {
		t.Fatalf("run error = %v; want hook failure and partial-release details", err)
	}
	assertTagExists(t, repo, "0.0.1")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); err != nil {
		t.Fatalf("CHANGELOG.md was not created before hook failure: %v", err)
	}
}

func TestInvalidHookConfigurationFailsBeforeRelease(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump\n"), 0o600); err != nil {
		t.Fatalf("write invalid gittan.toml: %v", err)
	}
	if err := run(repo, false, io.Discard); err == nil {
		t.Fatal("invalid TOML should fail")
	}
	assertTagMissing(t, repo, "0.0.1")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("invalid TOML unexpectedly wrote CHANGELOG.md (stat error: %v)", err)
	}
}

func TestDryRunDoesNotRunPostBumpHook(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump]\ncommands = [\"touch hook-ran.txt\"]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	if err := run(repo, true, io.Discard); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "hook-ran.txt")); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly ran post-bump hook (stat error: %v)", err)
	}
}

func TestRunReturnsErrorForInvalidRepository(t *testing.T) {
	if err := run(t.TempDir(), false, io.Discard); err == nil {
		t.Fatal("run should return an error for a directory without a Git repository")
	}
}

func TestRunUsesHighestMatchingTagEvenWhenUnreachable(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: initial change")
	tree := git(t, repo, "rev-parse", "HEAD^{tree}")
	unreachableCommit := git(t, repo, "commit-tree", tree, "-m", "unreachable commit")
	git(t, repo, "tag", "v0.0.1", unreachableCommit)
	before := git(t, repo, "rev-parse", "refs/tags/v0.0.1")

	if err := runWithOptions(repo, "v", false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "v0.0.2")
	after := git(t, repo, "rev-parse", "refs/tags/v0.0.1")
	if after != before {
		t.Fatalf("existing tag moved from %s to %s", before, after)
	}
}

func TestEnsureTagAvailableRejectsExistingTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	git(t, repo, "tag", "v0.0.1")
	if err := ensureTagAvailable(repo, "v0.0.1"); err == nil {
		t.Fatal("ensureTagAvailable should reject an existing tag")
	}
}

func TestChangelogIsDisabledByDefault(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	assertTagExists(t, repo, "0.0.1")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("default run unexpectedly created CHANGELOG.md (stat error: %v)", err)
	}
}

func TestGeneralSettingEnablesChangelogAndCLIFlagCanDisableIt(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "feat: add a feature")
	config := "[general]\nupdate-changelog = true\n"
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("configured run returned error: %v", err)
	}
	changelogPath := filepath.Join(repo, "CHANGELOG.md")
	firstChangelog, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read configured changelog: %v", err)
	}
	if !strings.Contains(string(firstChangelog), "## 🚀 0.1.0") || !strings.Contains(string(firstChangelog), "✨ feat: add a feature") {
		t.Fatalf("configured changelog is missing emojis or release content:\n%s", firstChangelog)
	}

	commit(t, repo, "fix: another release")
	overrides := releaseOverrides{updateChangelogSet: true, updateChangelog: false}
	if err := runWithReleaseOptions(repo, "", false, true, noBump, overrides, io.Discard); err != nil {
		t.Fatalf("run with changelog disabled by override returned error: %v", err)
	}
	secondChangelog, err := os.ReadFile(changelogPath)
	if err != nil || string(secondChangelog) != string(firstChangelog) {
		t.Fatalf("CLI override changed CHANGELOG.md: contents=%q error=%v", secondChangelog, err)
	}
}

func TestCLIHooksOverrideTOMLAndSubstituteProspectiveAndCreatedTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	config := "[pre-bump]\ncommands = [\"touch configured-pre.txt\"]\n\n[post-bump]\ncommands = [\"touch configured-post.txt\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	options, err := parseCLIOptions([]string{
		"--auto", "--repo", repo,
		"--pre-bump", "test ! -e .git/refs/tags/0.0.1 && printf '%s' %TAG% > pre-ran.txt",
		"--post-bump", "test -e .git/refs/tags/0.0.1 && test -f CHANGELOG.md && printf '%s' %TAG% > post-ran.txt",
		"--update-changelog",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parse CLI options: %v", err)
	}
	if err := runWithReleaseOptions(options.repo, options.prefix, options.dryRun, options.auto, options.level, options.overrides, io.Discard); err != nil {
		t.Fatalf("run with CLI hook overrides returned error: %v", err)
	}
	for _, name := range []string{"pre-ran.txt", "post-ran.txt"} {
		if got, err := os.ReadFile(filepath.Join(repo, name)); err != nil || string(got) != "0.0.1" {
			t.Errorf("%s contents = %q, %v; want prospective/created tag 0.0.1", name, got, err)
		}
	}
	for _, name := range []string{"configured-pre.txt", "configured-post.txt"} {
		if _, err := os.Stat(filepath.Join(repo, name)); !os.IsNotExist(err) {
			t.Errorf("CLI override did not replace TOML command %s (stat error: %v)", name, err)
		}
	}
}

func TestEmptyCLIHookFlagsClearConfiguredCommands(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	config := "[pre-bump]\ncommands = [\"touch pre-ran.txt\"]\n\n[post-bump]\ncommands = [\"touch post-ran.txt\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	options, err := parseCLIOptions([]string{"--auto", "--repo", repo, "--pre-bump=", "--post-bump="}, io.Discard)
	if err != nil {
		t.Fatalf("parse CLI options: %v", err)
	}
	if err := runWithReleaseOptions(options.repo, options.prefix, options.dryRun, options.auto, options.level, options.overrides, io.Discard); err != nil {
		t.Fatalf("run with cleared hooks returned error: %v", err)
	}
	for _, name := range []string{"pre-ran.txt", "post-ran.txt"} {
		if _, err := os.Stat(filepath.Join(repo, name)); !os.IsNotExist(err) {
			t.Errorf("cleared TOML hook unexpectedly ran %s (stat error: %v)", name, err)
		}
	}
}

func TestOverrideCommandsValidatesAndPreservesCommands(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    []string
		wantErr bool
	}{
		{name: "valid commands", values: []string{"echo first", "echo second"}, want: []string{"echo first", "echo second"}},
		{name: "clear commands", values: []string{""}, want: nil},
		{name: "reject empty command", values: []string{"echo first", "  "}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := overrideCommands("--pre-bump", test.values)
			if (err != nil) != test.wantErr {
				t.Fatalf("overrideCommands returned %v; want error %t", err, test.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("overrideCommands returned %#v; want %#v", got, test.want)
			}
		})
	}
}

func TestFallbackPreAndPostScriptsSubstituteTag(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	hookDir := filepath.Join(repo, ".gittan")
	if err := os.Mkdir(hookDir, 0o700); err != nil {
		t.Fatalf("create .gittan: %v", err)
	}
	preScript := "test ! -e .git/refs/tags/0.0.1 && printf '%s' %TAG% > fallback-pre.txt\n"
	postScript := "test -e .git/refs/tags/0.0.1 && printf '%s' %TAG% > fallback-post.txt\n"
	for name, contents := range map[string]string{"pre-bump.sh": preScript, "post-bump.sh": postScript} {
		if err := os.WriteFile(filepath.Join(hookDir, name), []byte(contents), 0o700); err != nil {
			t.Fatalf("write fallback hook %s: %v", name, err)
		}
	}
	if err := run(repo, false, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	for _, name := range []string{"fallback-pre.txt", "fallback-post.txt"} {
		if got, err := os.ReadFile(filepath.Join(repo, name)); err != nil || string(got) != "0.0.1" {
			t.Errorf("%s contents = %q, %v; want 0.0.1", name, got, err)
		}
	}
}

func TestPreBumpFailurePreventsTagAndChangelog(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	config := "[general]\nupdate-changelog = true\n\n[pre-bump]\ncommands = [\"printf pre-failed; exit 1\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	var output bytes.Buffer
	err := run(repo, false, &output)
	if err == nil || !strings.Contains(err.Error(), "before tag 0.0.1 was created") || !strings.Contains(err.Error(), "pre-failed") {
		t.Fatalf("pre-bump error = %v; want phase, tag timing and command output", err)
	}
	assertTagMissing(t, repo, "0.0.1")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("failed pre-bump unexpectedly created CHANGELOG.md (stat error: %v)", err)
	}
}

func TestPostHookFailureReportsWhenChangelogIsDisabled(t *testing.T) {
	repo := newGitRepo(t)
	commit(t, repo, "fix: create release")
	if err := os.WriteFile(filepath.Join(repo, "gittan.toml"), []byte("[post-bump]\ncommands = [\"exit 1\"]\n"), 0o600); err != nil {
		t.Fatalf("write gittan.toml: %v", err)
	}
	err := run(repo, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "tag 0.0.1 was created") || strings.Contains(err.Error(), "CHANGELOG.md") {
		t.Fatalf("post-hook error = %v; want tag-only partial-release details", err)
	}
	assertTagExists(t, repo, "0.0.1")
	if _, err := os.Stat(filepath.Join(repo, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Fatalf("disabled changelog was unexpectedly created (stat error: %v)", err)
	}
}

func runWithChangelog(repo, prefix string, output io.Writer) error {
	return runWithReleaseOptions(repo, prefix, false, true, noBump, changelogOverride(), output)
}

func changelogOverride() releaseOverrides {
	return releaseOverrides{updateChangelog: true, updateChangelogSet: true}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Test User")
	git(t, repo, "config", "user.email", "test@example.com")
	return repo
}

func commit(t *testing.T, repo, message string) {
	t.Helper()
	file := filepath.Join(repo, "file.txt")
	if err := os.WriteFile(file, []byte(message), 0o600); err != nil {
		t.Fatalf("write commit file: %v", err)
	}
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "-q", "-m", message)
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func assertTagExists(t *testing.T, repo, tag string) {
	t.Helper()
	if got := git(t, repo, "tag", "--list", tag); got != tag {
		t.Fatalf("tag %q does not exist (listed %q)", tag, got)
	}
}

func assertTagMissing(t *testing.T, repo, tag string) {
	t.Helper()
	if got := git(t, repo, "tag", "--list", tag); got != "" {
		t.Fatalf("tag %q unexpectedly exists", tag)
	}
}
