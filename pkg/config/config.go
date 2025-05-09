// Copyright The Helm Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/helm/chart-testing/v3/pkg/util"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	homeDir, _            = homedir.Dir()
	configSearchLocations = []string{
		".",
		".ct",
		filepath.Join(homeDir, ".ct"),
		"/usr/local/etc/ct",
		"/etc/ct",
	}
)

type Configuration struct {
	Remote                  string        `mapstructure:"remote"`
	TargetBranch            string        `mapstructure:"target-branch"`
	Since                   string        `mapstructure:"since"`
	BuildID                 string        `mapstructure:"build-id"`
	LintConf                string        `mapstructure:"lint-conf"`
	ChartYamlSchema         string        `mapstructure:"chart-yaml-schema"`
	ValidateMaintainers     bool          `mapstructure:"validate-maintainers"`
	ValidateChartSchema     bool          `mapstructure:"validate-chart-schema"`
	ValidateYaml            bool          `mapstructure:"validate-yaml"`
	SkipHelmDependencies    bool          `mapstructure:"skip-helm-dependencies"`
	AdditionalCommands      []string      `mapstructure:"additional-commands"`
	CheckVersionIncrement   bool          `mapstructure:"check-version-increment"`
	ProcessAllCharts        bool          `mapstructure:"all"`
	Charts                  []string      `mapstructure:"charts"`
	ChartRepos              []string      `mapstructure:"chart-repos"`
	ChartDirs               []string      `mapstructure:"chart-dirs"`
	ExcludedCharts          []string      `mapstructure:"excluded-charts"`
	HelmExtraArgs           string        `mapstructure:"helm-extra-args"`
	HelmExtraSetArgs        string        `mapstructure:"helm-extra-set-args"`
	HelmLintExtraArgs       string        `mapstructure:"helm-lint-extra-args"`
	HelmRepoExtraArgs       []string      `mapstructure:"helm-repo-extra-args"`
	HelmDependencyExtraArgs []string      `mapstructure:"helm-dependency-extra-args"`
	Debug                   bool          `mapstructure:"debug"`
	Upgrade                 bool          `mapstructure:"upgrade"`
	SkipMissingValues       bool          `mapstructure:"skip-missing-values"`
	SkipCleanUp             bool          `mapstructure:"skip-clean-up"`
	Namespace               string        `mapstructure:"namespace"`
	ReleaseName             string        `mapstructure:"release-name"`
	ReleaseLabel            string        `mapstructure:"release-label"`
	ExcludeDeprecated       bool          `mapstructure:"exclude-deprecated"`
	KubectlTimeout          time.Duration `mapstructure:"kubectl-timeout"`
	PrintLogs               bool          `mapstructure:"print-logs"`
	GithubGroups            bool          `mapstructure:"github-groups"`
	UseHelmignore           bool          `mapstructure:"use-helmignore"`
}

func LoadConfiguration(cfgFile string, cmd *cobra.Command, printConfig bool) (*Configuration, error) {
	v := viper.New()

	v.SetDefault("kubectl-timeout", 30*time.Second)
	v.SetDefault("print-logs", bool(true))

	cmd.Flags().VisitAll(func(flag *flag.Flag) {
		flagName := flag.Name
		if flagName != "config" && flagName != "help" {
			if err := v.BindPFlag(flagName, flag); err != nil {
				// can't really happen
				panic(fmt.Sprintf("failed binding flag %q: %v\n", flagName, err.Error()))
			}
		}
	})

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix("CT")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("ct")
		if cfgFile, ok := os.LookupEnv("CT_CONFIG_DIR"); ok {
			v.AddConfigPath(cfgFile)
		} else {
			for _, searchLocation := range configSearchLocations {
				v.AddConfigPath(searchLocation)
			}
		}
	}

	if err := v.ReadInConfig(); err != nil {
		if cfgFile != "" {
			// Only error out for specified config file. Ignore for default locations.
			return nil, fmt.Errorf("failed loading config file: %w", err)
		}
	} else {
		if printConfig {
			fmt.Fprintln(os.Stderr, "Using config file:", v.ConfigFileUsed())
		}
	}

	isLint := strings.Contains(cmd.Use, "lint")
	isInstall := strings.Contains(cmd.Use, "install")

	cfg := &Configuration{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed unmarshaling configuration: %w", err)
	}

	if cfg.ProcessAllCharts && len(cfg.Charts) > 0 {
		return nil, errors.New("specifying both, '--all' and '--charts', is not allowed")
	}

	if cfg.Namespace != "" && cfg.ReleaseLabel == "" {
		return nil, errors.New("specifying '--namespace' without '--release-label' is not allowed")
	}

	// Disable upgrade (this does some expensive dependency building on previous revisions)
	// when neither "install" nor "lint-and-install" have not been specified.
	cfg.Upgrade = isInstall && cfg.Upgrade
	if (cfg.TargetBranch == "" || cfg.Remote == "") && cfg.Upgrade {
		return nil, errors.New("specifying '--upgrade=true' without '--target-branch' or '--remote', is not allowed")
	}

	chartYamlSchemaPath := cfg.ChartYamlSchema
	if chartYamlSchemaPath == "" {
		var err error
		cfgFile, err = findConfigFile("chart_schema.yaml")
		if err != nil && isLint && cfg.ValidateChartSchema {
			return nil, errors.New("'chart_schema.yaml' neither specified nor found in default locations")
		}
		cfg.ChartYamlSchema = cfgFile
	}

	lintConfPath := cfg.LintConf
	if lintConfPath == "" {
		var err error
		cfgFile, err = findConfigFile("lintconf.yaml")
		if err != nil && isLint && cfg.ValidateYaml {
			return nil, errors.New("'lintconf.yaml' neither specified nor found in default locations")
		}
		cfg.LintConf = cfgFile
	}

	if len(cfg.Charts) > 0 || cfg.ProcessAllCharts {
		fmt.Fprintln(os.Stderr, "Version increment checking disabled.")
		cfg.CheckVersionIncrement = false
	}

	if printConfig {
		printCfg(cfg)
	}

	return cfg, nil
}

func printCfg(cfg *Configuration) {
	if !cfg.GithubGroups {
		util.PrintDelimiterLineToWriter(os.Stderr, "-")
		fmt.Fprintln(os.Stderr, " Configuration")
		util.PrintDelimiterLineToWriter(os.Stderr, "-")
	} else {
		util.GithubGroupsBegin(os.Stderr, "Configuration")
	}

	e := reflect.ValueOf(cfg).Elem()
	typeOfCfg := e.Type()

	for i := 0; i < e.NumField(); i++ {
		var pattern string
		switch e.Field(i).Kind() {
		case reflect.Bool:
			pattern = "%s: %t\n"
		default:
			pattern = "%s: %s\n"
		}
		fmt.Fprintf(os.Stderr, pattern, typeOfCfg.Field(i).Name, e.Field(i).Interface())
	}

	if !cfg.GithubGroups {
		util.PrintDelimiterLineToWriter(os.Stderr, "-")
	} else {
		util.GithubGroupsEnd(os.Stderr)
	}
}

func findConfigFile(fileName string) (string, error) {
	if dir, ok := os.LookupEnv("CT_CONFIG_DIR"); ok {
		return filepath.Join(dir, fileName), nil
	}

	for _, location := range configSearchLocations {
		filePath := filepath.Join(location, fileName)
		if util.FileExists(filePath) {
			return filePath, nil
		}
	}

	return "", fmt.Errorf("config file not found: %s", fileName)
}
