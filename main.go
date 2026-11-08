package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type version struct {
	major uint64
	minor uint64
	patch uint64
}

type bumpLevel int

const (
	noBump bumpLevel = iota
	patchBump
	minorBump
	majorBump
)

func main() {
	options, err := parseCLIOptions(os.Args[1:], os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	if !options.bump {
		if err := runStandaloneChangelog(options.repo, options.prefix, options.changelogMode, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gittan:", err)
			os.Exit(1)
		}
		return
	}
	if err := runWithReleaseOptions(options.repo, options.prefix, options.dryRun, options.auto, options.level, options.overrides, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gittan:", err)
		os.Exit(1)
	}
}

type cliOptions struct {
	repo          string
	prefix        string
	dryRun        bool
	bump          bool
	auto          bool
	level         bumpLevel
	changelogMode changelogMode
	overrides     releaseOverrides
}

type changelogMode string

const (
	changelogOff  changelogMode = "OFF"
	changelogOn   changelogMode = "ON"
	changelogFull changelogMode = "FULL"
)

type stringListFlag struct {
	values []string
	set    bool
}

func (flag *stringListFlag) String() string {
	return strings.Join(flag.values, ",")
}

func (flag *stringListFlag) Set(value string) error {
	flag.values = append(flag.values, value)
	flag.set = true
	return nil
}

type optionalBoolFlag struct {
	value bool
	set   bool
}

type optionalChangelogFlag struct {
	value changelogMode
	set   bool
}

func (flag *optionalChangelogFlag) String() string {
	return string(flag.value)
}

func (flag *optionalChangelogFlag) Set(value string) error {
	mode := changelogMode(strings.ToUpper(value))
	if mode != changelogOff && mode != changelogOn && mode != changelogFull {
		return errors.New("must be OFF, ON, or FULL")
	}
	flag.value = mode
	flag.set = true
	return nil
}

func (flag *optionalBoolFlag) String() string {
	return strconv.FormatBool(flag.value)
}

func (flag *optionalBoolFlag) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	flag.value = parsed
	flag.set = true
	return nil
}

func (flag *optionalBoolFlag) IsBoolFlag() bool {
	return true
}

func parseCLIOptions(args []string, errorOutput io.Writer) (cliOptions, error) {
	flags := flag.NewFlagSet("gittan", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	var repo, prefix string
	var dryRun, auto, major, minor, patch bool
	flags.StringVar(&repo, "repo", ".", "Git repository to inspect")
	flags.StringVar(&repo, "r", ".", "Git repository to inspect (shorthand)")
	flags.StringVar(&prefix, "prefix", "", "version tag prefix (defaults to no prefix)")
	flags.StringVar(&prefix, "p", "", "version tag prefix (defaults to no prefix) (shorthand)")
	flags.BoolVar(&dryRun, "dry-run", false, "show the proposed release without changing the repository")
	flags.BoolVar(&dryRun, "d", false, "show the proposed release without changing the repository (shorthand)")
	flags.BoolVar(&auto, "auto", false, "automatically determine the version bump")
	flags.BoolVar(&auto, "a", false, "automatically determine the version bump (shorthand)")
	flags.BoolVar(&major, "major", false, "increment the major version without inspecting commit history")
	flags.BoolVar(&minor, "minor", false, "increment the minor version without inspecting commit history")
	flags.BoolVar(&patch, "patch", false, "increment the patch version without inspecting commit history")
	var preBump, postBump stringListFlag
	flags.Var(&preBump, "pre-bump", "run a pre-bump shell command (repeatable; an empty value clears configured commands)")
	flags.Var(&postBump, "post-bump", "run a post-bump shell command (repeatable; an empty value clears configured commands)")
	var updateChangelog optionalBoolFlag
	flags.Var(&updateChangelog, "update-changelog", "update CHANGELOG.md for this release (true or false)")
	var changelog optionalChangelogFlag
	flags.Var(&changelog, "changelog", "write CHANGELOG.md: OFF, ON (since latest tag), or FULL (all commits)")
	flags.Var(&changelog, "c", "write CHANGELOG.md: OFF, ON (since latest tag), or FULL (all commits) (shorthand)")
	if err := flags.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if flags.NArg() != 0 {
		err := errors.New("unexpected positional arguments")
		fmt.Fprintln(errorOutput, err)
		return cliOptions{}, err
	}
	selected := 0
	level := noBump
	for _, option := range []struct {
		selected bool
		level    bumpLevel
	}{
		{major, majorBump},
		{minor, minorBump},
		{patch, patchBump},
	} {
		if option.selected {
			selected++
			level = option.level
		}
	}
	if auto {
		selected++
	}
	if selected > 1 {
		err := errors.New("choose only one of --auto, --major, --minor, or --patch")
		fmt.Fprintln(errorOutput, err)
		return cliOptions{}, err
	}
	if changelog.set && updateChangelog.set {
		err := errors.New("choose either --changelog or --update-changelog")
		fmt.Fprintln(errorOutput, err)
		return cliOptions{}, err
	}
	if dryRun && selected == 0 {
		err := errors.New("--dry-run requires an explicit --auto, --major, --minor, or --patch flag")
		fmt.Fprintln(errorOutput, err)
		return cliOptions{}, err
	}
	if dryRun && (changelog.set || updateChangelog.set) {
		err := errors.New("--dry-run cannot be combined with changelog flags")
		fmt.Fprintln(errorOutput, err)
		return cliOptions{}, err
	}
	bumpSelected := selected > 0
	if !bumpSelected && !changelog.set && !updateChangelog.set {
		bumpSelected = true
		auto = true
	}
	autoMode := auto
	mode := changelog.value
	if !changelog.set && updateChangelog.set {
		mode = changelogOff
		if updateChangelog.value {
			mode = changelogOn
		}
	}
	return cliOptions{
		repo:          repo,
		prefix:        prefix,
		dryRun:        dryRun,
		bump:          bumpSelected,
		auto:          autoMode,
		level:         level,
		changelogMode: mode,
		overrides: releaseOverrides{
			preCommands:        preBump.values,
			preCommandsSet:     preBump.set,
			postCommands:       postBump.values,
			postCommandsSet:    postBump.set,
			updateChangelog:    updateChangelog.value,
			updateChangelogSet: updateChangelog.set,
			changelogMode:      changelog.value,
			changelogModeSet:   changelog.set,
		},
	}, nil
}

func run(repo string, dryRun bool, output io.Writer) error {
	return runWithBumpOptions(repo, "", dryRun, true, noBump, output)
}

func runWithOptions(repo, prefix string, dryRun bool, output io.Writer) error {
	return runWithBumpOptions(repo, prefix, dryRun, true, noBump, output)
}

func runWithBumpOptions(repo, prefix string, dryRun, auto bool, manualLevel bumpLevel, output io.Writer) error {
	return runWithReleaseOptions(repo, prefix, dryRun, auto, manualLevel, releaseOverrides{}, output)
}

func runWithReleaseOptions(repo, prefix string, dryRun, auto bool, manualLevel bumpLevel, overrides releaseOverrides, output io.Writer) error {
	head, err := gitOutput(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot read repository HEAD: %w", err)
	}

	tags, err := gitOutput(repo, "tag", "--list")
	if err != nil {
		return err
	}
	baseVersion := version{}
	baseTag := ""
	for _, tag := range strings.Split(tags, "\n") {
		if !strings.HasPrefix(tag, prefix) {
			continue
		}
		candidate, valid := parseVersion(strings.TrimPrefix(tag, prefix))
		if valid && (baseTag == "" || candidate.compare(baseVersion) > 0) {
			baseVersion = candidate
			baseTag = tag
		}
	}

	level := manualLevel
	if auto {
		args := []string{"log", "--format=%B%x00", "HEAD"}
		if baseTag != "" {
			args = []string{"log", "--format=%B%x00", baseTag + "..HEAD"}
		}
		log, err := gitOutput(repo, args...)
		if err != nil {
			return err
		}
		level = noBump
		for _, message := range strings.Split(log, "\x00") {
			message = strings.TrimSpace(message)
			if message == "" {
				continue
			}
			candidate := classifyCommit(message)
			if candidate > level {
				level = candidate
			}
		}
	}
	if auto && level == noBump {
		if !dryRun {
			fmt.Fprintln(output, "👍 No release-worthy commits since the latest matching version tag.")
		}
		return nil
	}

	next := version{}
	if baseTag != "" {
		next = baseVersion
	}
	next, err = next.bump(level)
	if err != nil {
		return err
	}
	tag := prefix + next.String()
	if _, err := gitOutput(repo, "check-ref-format", "refs/tags/"+tag); err != nil {
		return fmt.Errorf("invalid target tag %q: %w", tag, err)
	}
	if err := ensureTagAvailable(repo, tag); err != nil {
		return err
	}

	if dryRun {
		fmt.Fprintln(output, tag)
		return nil
	}

	settings, err := loadReleaseSettings(repo, overrides)
	if err != nil {
		return err
	}
	if err := settings.runPreBump(repo, output, tag); err != nil {
		return err
	}

	changelogPath := filepath.Join(repo, "CHANGELOG.md")
	var changelog string
	if settings.changelogMode != changelogOff {
		messageBaseTag := baseTag
		if settings.changelogMode == changelogFull {
			messageBaseTag = ""
		}
		messages, err := gitCommitMessages(repo, messageBaseTag, head)
		if err != nil {
			return fmt.Errorf("pre-bump completed, but cannot read commit history for CHANGELOG.md: %w", err)
		}
		messages = releaseMessages(messages)
		var existingChangelog []byte
		if settings.changelogMode == changelogFull {
			existingChangelog = nil
		} else {
			existingChangelog, err = os.ReadFile(changelogPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("pre-bump completed, but cannot read CHANGELOG.md: %w", err)
			}
		}
		changelog = renderChangelogMode(string(existingChangelog), tag, messages, settings.changelogMode)
	}

	if _, err := gitOutput(repo, "tag", tag, head); err != nil {
		return fmt.Errorf("cannot create tag %s: %w", tag, err)
	}
	if settings.changelogMode != changelogOff {
		if err := os.WriteFile(changelogPath, []byte(changelog), 0o644); err != nil {
			return fmt.Errorf("created tag %s, but cannot write CHANGELOG.md: %w", tag, err)
		}
	}
	if err := settings.runPostBump(repo, output, tag); err != nil {
		return err
	}
	fmt.Fprintf(output, "👍 Created tag %s at HEAD.\n", tag)
	return nil
}

func renderChangelog(existing, tag string, messages []string) string {
	return renderChangelogMode(existing, tag, messages, changelogOn)
}

func renderChangelogMode(existing, tag string, messages []string, mode changelogMode) string {
	var entry strings.Builder
	fmt.Fprintf(&entry, "<!-- gittan:release-start %s -->\n## 🚀 %s %s\n\n", tag, tag, festiveEmoji(tag))
	for _, message := range messages {
		subject := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
		if subject != "" {
			fmt.Fprintf(&entry, "- %s %s\n", changelogEmoji(message), subject)
		}
	}
	fmt.Fprintf(&entry, "<!-- gittan:release-end %s -->\n", tag)
	if mode != changelogFull && existing != "" {
		entry.WriteByte('\n')
		entry.WriteString(existing)
		if !strings.HasSuffix(existing, "\n") {
			entry.WriteByte('\n')
		}
	}
	return entry.String()
}

func festiveEmoji(label string) string {
	emojis := []string{"🎉", "🌟", "🍀", "🎊", "🎁", "🌈", "🪅"}
	var hash uint64
	for _, character := range []byte(label) {
		hash = hash*31 + uint64(character)
	}
	return emojis[hash%uint64(len(emojis))]
}

func gitCommitMessages(repo, baseTag, head string) ([]string, error) {
	args := []string{"log", "--format=%B%x00", head}
	if baseTag != "" {
		args = []string{"log", "--format=%B%x00", baseTag + ".." + head}
	}
	log, err := gitOutput(repo, args...)
	if err != nil {
		return nil, err
	}
	messages := make([]string, 0)
	for _, message := range strings.Split(log, "\x00") {
		message = strings.TrimSpace(message)
		if message != "" {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func releaseMessages(messages []string) []string {
	releases := make([]string, 0, len(messages))
	for _, message := range messages {
		if classifyCommit(message) != noBump {
			releases = append(releases, message)
		}
	}
	return releases
}

func runStandaloneChangelog(repo, prefix string, mode changelogMode, output io.Writer) error {
	if mode == changelogOff {
		return nil
	}
	if mode != changelogOn && mode != changelogFull {
		return fmt.Errorf("unsupported changelog mode %q", mode)
	}
	head, err := gitOutput(repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot read repository HEAD: %w", err)
	}
	tags, err := gitOutput(repo, "tag", "--list")
	if err != nil {
		return err
	}
	baseTag := ""
	baseVersion := version{}
	for _, tag := range strings.Split(tags, "\n") {
		if !strings.HasPrefix(tag, prefix) {
			continue
		}
		candidate, valid := parseVersion(strings.TrimPrefix(tag, prefix))
		if valid && (baseTag == "" || candidate.compare(baseVersion) > 0) {
			baseTag = tag
			baseVersion = candidate
		}
	}
	messageBaseTag := baseTag
	if mode == changelogFull {
		messageBaseTag = ""
	}
	messages, err := gitCommitMessages(repo, messageBaseTag, head)
	if err != nil {
		return fmt.Errorf("cannot read commit history for CHANGELOG.md: %w", err)
	}
	messages = releaseMessages(messages)
	label := "Unreleased"
	if mode == changelogFull {
		label = "History"
	} else if len(messages) == 0 {
		return nil
	}
	changelogPath := filepath.Join(repo, "CHANGELOG.md")
	var existing []byte
	if mode == changelogFull {
		existing = nil
	} else {
		existing, err = os.ReadFile(changelogPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot read CHANGELOG.md: %w", err)
		}
	}
	changelog := renderChangelogMode(string(existing), label, messages, mode)
	if err := os.WriteFile(changelogPath, []byte(changelog), 0o644); err != nil {
		return fmt.Errorf("cannot write CHANGELOG.md: %w", err)
	}
	fmt.Fprintf(output, "👍 Updated CHANGELOG.md in %s mode.\n", mode)
	return nil
}

func changelogEmoji(message string) string {
	lines := strings.Split(message, "\n")
	commitType, breaking, valid := parseConventionalHeader(lines[0])
	if !valid {
		return "📦"
	}
	if breaking || hasBreakingFooter(lines) {
		return "💥"
	}
	switch strings.ToLower(commitType) {
	case "feat":
		return "✨"
	case "fix":
		return "🐛"
	case "perf":
		return "⚡"
	default:
		return "📦"
	}
}

func parseVersion(tag string) (version, bool) {
	parts := strings.Split(tag, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	values := [3]uint64{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return version{}, false
			}
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version{}, false
		}
		values[i] = value
	}
	return version{major: values[0], minor: values[1], patch: values[2]}, true
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) compare(other version) int {
	if v.major != other.major {
		if v.major > other.major {
			return 1
		}
		return -1
	}
	if v.minor != other.minor {
		if v.minor > other.minor {
			return 1
		}
		return -1
	}
	if v.patch != other.patch {
		if v.patch > other.patch {
			return 1
		}
		return -1
	}
	return 0
}

func (v version) bump(level bumpLevel) (version, error) {
	switch level {
	case patchBump:
		if v.patch == math.MaxUint64 {
			return version{}, errors.New("patch version overflow")
		}
		v.patch++
	case minorBump:
		if v.minor == math.MaxUint64 {
			return version{}, errors.New("minor version overflow")
		}
		v.minor++
		v.patch = 0
	case majorBump:
		if v.major == math.MaxUint64 {
			return version{}, errors.New("major version overflow")
		}
		v.major++
		v.minor = 0
		v.patch = 0
	default:
		return version{}, errors.New("cannot bump version without a release level")
	}
	return v, nil
}

func classifyCommit(message string) bumpLevel {
	message = strings.TrimSpace(message)
	if message == "" {
		return noBump
	}
	lines := strings.Split(message, "\n")
	commitType, breaking, valid := parseConventionalHeader(lines[0])
	if !valid {
		return noBump
	}
	if breaking || hasBreakingFooter(lines) {
		return majorBump
	}
	switch strings.ToLower(commitType) {
	case "feat":
		return minorBump
	case "fix":
		return patchBump
	default:
		return noBump
	}
}

func parseConventionalHeader(header string) (string, bool, bool) {
	header = strings.TrimSpace(header)
	colon := strings.IndexByte(header, ':')
	if colon < 0 || colon+1 >= len(header) || header[colon+1] != ' ' || strings.TrimSpace(header[colon+2:]) == "" {
		return "", false, false
	}
	typeAndScope := header[:colon]
	breaking := strings.HasSuffix(typeAndScope, "!")
	if breaking {
		typeAndScope = strings.TrimSuffix(typeAndScope, "!")
	}
	if strings.Contains(typeAndScope, "!") {
		return "", false, false
	}

	commitType := typeAndScope
	if scopeStart := strings.IndexByte(typeAndScope, '('); scopeStart >= 0 {
		if !strings.HasSuffix(typeAndScope, ")") || scopeStart == 0 {
			return "", false, false
		}
		scope := typeAndScope[scopeStart+1 : len(typeAndScope)-1]
		if strings.TrimSpace(scope) == "" || strings.ContainsAny(scope, "()\r\n") {
			return "", false, false
		}
		commitType = typeAndScope[:scopeStart]
	} else if strings.Contains(typeAndScope, ")") {
		return "", false, false
	}
	if !validCommitType(commitType) {
		return "", false, false
	}
	return commitType, breaking, true
}

func validCommitType(commitType string) bool {
	if commitType == "" {
		return false
	}
	for index, character := range commitType {
		if index == 0 && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return false
		}
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func hasBreakingFooter(lines []string) bool {
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != "" {
			continue
		}
		start := index + 1
		for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		end := start
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		if start < end && isFooterLine(lines[start]) {
			for footerIndex := start; footerIndex < end; footerIndex++ {
				if isBreakingFooterLine(lines[footerIndex]) {
					return true
				}
				if footerIndex > start && !isFooterLine(lines[footerIndex]) &&
					!strings.HasPrefix(lines[footerIndex], " ") && !strings.HasPrefix(lines[footerIndex], "\t") {
					break
				}
			}
		}
		index = end
	}
	return false
}

func isFooterLine(line string) bool {
	token, value, valid := parseFooterLine(line)
	return valid && token != "" && value != ""
}

func isBreakingFooterLine(line string) bool {
	token, value, valid := parseFooterLine(line)
	return valid && (token == "BREAKING CHANGE" || token == "BREAKING-CHANGE") && value != ""
}

func parseFooterLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	colon := strings.IndexByte(line, ':')
	if colon < 0 || colon+1 >= len(line) || line[colon+1] != ' ' {
		return "", "", false
	}
	token := line[:colon]
	if token != "BREAKING CHANGE" && token != "BREAKING-CHANGE" && !validCommitType(token) {
		return "", "", false
	}
	return token, strings.TrimSpace(line[colon+2:]), true
}

func ensureTagAvailable(repo, tag string) error {
	if _, err := gitOutput(repo, "show-ref", "--verify", "--quiet", "refs/tags/"+tag); err == nil {
		return fmt.Errorf("tag %s already exists; refusing to overwrite it", tag)
	} else if !isExitCode(err, 1) {
		return fmt.Errorf("cannot check whether tag %s exists: %w", tag, err)
	}
	return nil
}

func gitOutput(repo string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
