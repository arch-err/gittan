package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type releaseOverrides struct {
	preCommands        []string
	preCommandsSet     bool
	postCommands       []string
	postCommandsSet    bool
	updateChangelog    bool
	updateChangelogSet bool
	changelogMode      changelogMode
	changelogModeSet   bool
}

type releaseSettings struct {
	preCommands     []string
	preScript       string
	postCommands    []string
	postScript      string
	updateChangelog bool
	changelogMode   changelogMode
}

func loadReleaseSettings(repo string, overrides releaseOverrides) (releaseSettings, error) {
	configPath := filepath.Join(repo, "gittan.toml")
	_, statErr := os.Stat(configPath)
	configExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return releaseSettings{}, fmt.Errorf("cannot access gittan.toml: %w", statErr)
	}

	settings := releaseSettings{changelogMode: changelogOff}
	if configExists {
		var config struct {
			General struct {
				UpdateChangelog bool `toml:"update-changelog"`
			} `toml:"general"`
			PreBump struct {
				Commands []string `toml:"commands"`
			} `toml:"pre-bump"`
			PostBump struct {
				Commands []string `toml:"commands"`
			} `toml:"post-bump"`
		}
		if _, err := toml.DecodeFile(configPath, &config); err != nil {
			return releaseSettings{}, fmt.Errorf("cannot parse gittan.toml: %w", err)
		}
		settings.preCommands = config.PreBump.Commands
		settings.postCommands = config.PostBump.Commands
		settings.updateChangelog = config.General.UpdateChangelog
		if settings.updateChangelog {
			settings.changelogMode = changelogOn
		}
	} else {
		if !overrides.preCommandsSet {
			script, err := readFallbackScript(repo, "pre-bump")
			if err != nil {
				return releaseSettings{}, err
			}
			settings.preScript = script
		}
		if !overrides.postCommandsSet {
			script, err := readFallbackScript(repo, "post-bump")
			if err != nil {
				return releaseSettings{}, err
			}
			settings.postScript = script
		}
	}

	if overrides.preCommandsSet {
		commands, err := overrideCommands("--pre-bump", overrides.preCommands)
		if err != nil {
			return releaseSettings{}, err
		}
		settings.preCommands = commands
		settings.preScript = ""
	}
	if overrides.postCommandsSet {
		commands, err := overrideCommands("--post-bump", overrides.postCommands)
		if err != nil {
			return releaseSettings{}, err
		}
		settings.postCommands = commands
		settings.postScript = ""
	}
	if overrides.updateChangelogSet {
		settings.updateChangelog = overrides.updateChangelog
		settings.changelogMode = changelogOff
		if overrides.updateChangelog {
			settings.changelogMode = changelogOn
		}
	}
	if overrides.changelogModeSet {
		settings.changelogMode = overrides.changelogMode
		settings.updateChangelog = overrides.changelogMode != changelogOff
	}
	if err := validateCommands("pre-bump", settings.preCommands); err != nil {
		return releaseSettings{}, err
	}
	if err := validateCommands("post-bump", settings.postCommands); err != nil {
		return releaseSettings{}, err
	}
	return settings, nil
}

func overrideCommands(flagName string, values []string) ([]string, error) {
	if len(values) == 1 && values[0] == "" {
		return nil, nil
	}
	if err := validateCommands(flagName, values); err != nil {
		return nil, err
	}
	return values, nil
}

func validateCommands(phase string, commands []string) error {
	for i, command := range commands {
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("%s command %d is empty; pass the flag once with an empty value to clear configured commands", phase, i+1)
		}
	}
	return nil
}

func readFallbackScript(repo, phase string) (string, error) {
	path := filepath.Join(repo, ".gittan", phase+".sh")
	script, err := os.ReadFile(path)
	if err == nil {
		return string(script), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", fmt.Errorf("cannot read %s hook: %w", phase, err)
}

func (settings releaseSettings) runPreBump(repo string, output io.Writer, tag string) error {
	if err := settings.runCommands(repo, output, tag, "pre-bump", settings.preCommands); err != nil {
		return err
	}
	if settings.preScript != "" {
		if err := runHookScript(repo, output, tag, settings.preScript); err != nil {
			return fmt.Errorf("pre-bump hook failed before tag %s was created: %w", tag, err)
		}
	}
	return nil
}

func (settings releaseSettings) runPostBump(repo string, output io.Writer, tag string) error {
	if err := settings.runCommands(repo, output, tag, "post-bump", settings.postCommands); err != nil {
		return err
	}
	if settings.postScript != "" {
		if err := runHookScript(repo, output, tag, settings.postScript); err != nil {
			return settings.postBumpError("post-bump hook failed", tag, err)
		}
	}
	return nil
}

func (settings releaseSettings) runCommands(repo string, output io.Writer, tag, phase string, commands []string) error {
	for i, command := range commands {
		if err := runHookScript(repo, output, tag, command); err != nil {
			if phase == "pre-bump" {
				return fmt.Errorf("pre-bump command %d failed before tag %s was created: %w", i+1, tag, err)
			}
			return settings.postBumpError(fmt.Sprintf("post-bump command %d failed", i+1), tag, err)
		}
	}
	return nil
}

func (settings releaseSettings) postBumpError(message, tag string, err error) error {
	if settings.changelogMode != changelogOff {
		return fmt.Errorf("%s; tag %s and CHANGELOG.md were created: %w", message, tag, err)
	}
	return fmt.Errorf("%s; tag %s was created: %w", message, tag, err)
}

func runHookScript(repo string, output io.Writer, tag, script string) error {
	command := strings.ReplaceAll(script, "%TAG%", shellQuote(tag))
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = repo
	cmd.Env = hookEnvironment(repo, tag)
	return runHookCommand(cmd, output)
}

func hookEnvironment(repo, tag string) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "GITTAN_TAG=") || strings.HasPrefix(value, "GITTAN_REPO=") {
			continue
		}
		environment = append(environment, value)
	}
	return append(environment, "GITTAN_TAG="+tag, "GITTAN_REPO="+repo)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runHookCommand(cmd *exec.Cmd, output io.Writer) error {
	commandOutput, err := cmd.CombinedOutput()
	if len(commandOutput) > 0 {
		if _, writeErr := output.Write(commandOutput); writeErr != nil {
			return fmt.Errorf("cannot write hook output: %w", writeErr)
		}
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, commandOutput)
	}
	return nil
}
