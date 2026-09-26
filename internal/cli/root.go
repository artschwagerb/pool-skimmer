// Package cli implements the pool-skimmer command-line interface.
package cli

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	endpointconfig "pool-skimmer/internal/config"
	"pool-skimmer/internal/scim"
	"pool-skimmer/internal/tui"
)

type config struct {
	endpoint     string
	apiKey       string
	apiKeyFile   string
	promptAPIKey bool
	authHeader   string
	authScheme   string
	timeout      string
	readRetries  int
	profile      string
	configDir    string
}

// NewRootCommand builds an isolated command tree, which makes the CLI easy to test and embed.
func NewRootCommand(version string) *cobra.Command {
	cfg := &config{}
	command := &cobra.Command{
		Use:           "pool-skimmer",
		Short:         "Manage users and groups through a SCIM 2.0 API",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			input, inputOK := command.InOrStdin().(*os.File)
			output, outputOK := command.OutOrStdout().(*os.File)
			if !inputOK || !outputOK || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
				return errors.New("the interactive TUI requires a terminal; use --help or a command such as users list")
			}
			if cfg.useEndpointSelector(command) {
				store, err := cfg.endpointStore()
				if err != nil {
					return err
				}
				endpoints, err := store.Load()
				if err != nil {
					return err
				}
				return tui.RunEndpointSelector(command.Context(), store, endpoints, version)
			}
			client, endpoint, err := cfg.client(command)
			if err != nil {
				return err
			}
			return tui.Run(command.Context(), client, endpoint.Name, client.Endpoint(), version)
		},
	}
	command.SetVersionTemplate("{{.Version}}\n")

	flags := command.PersistentFlags()
	flags.StringVar(&cfg.endpoint, "endpoint", "", "SCIM service root (SCIM_ENDPOINT)")
	flags.StringVar(&cfg.apiKey, "api-key", "", "API key; environment or a file is safer (SCIM_API_KEY)")
	flags.StringVar(&cfg.apiKeyFile, "api-key-file", "", "read the API key from a file (SCIM_API_KEY_FILE)")
	flags.BoolVar(&cfg.promptAPIKey, "prompt-api-key", false, "securely prompt for the API key")
	flags.StringVar(&cfg.authHeader, "auth-header", "", "authentication header (default: Authorization)")
	flags.StringVar(&cfg.authScheme, "auth-scheme", "", "authentication value prefix (default: Bearer; use '' for none)")
	flags.StringVar(&cfg.timeout, "timeout", "", "request timeout, such as 30s or 1m (SCIM_TIMEOUT)")
	flags.IntVar(&cfg.readRetries, "read-retries", 2, "GET retries for transient errors (SCIM_READ_RETRIES)")
	flags.StringVar(&cfg.profile, "profile", "", "use a named SCIM endpoint from ~/.pool-skimmer/config.json")
	flags.StringVar(&cfg.configDir, "config-dir", "", "configuration directory (default: ~/.pool-skimmer)")

	command.AddCommand(newResourceCommand(cfg, "Users"))
	command.AddCommand(newResourceCommand(cfg, "Groups"))
	command.AddCommand(newDoctorCommand(cfg))
	command.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "pool-skimmer %s\n", version)
			return err
		},
	})
	return command
}

func (cfg *config) client(command *cobra.Command) (*scim.Client, endpointconfig.Endpoint, error) {
	profile, err := cfg.selectedProfile()
	if err != nil {
		return nil, endpointconfig.Endpoint{}, err
	}
	endpoint := cfg.endpoint
	endpointFromProfile := false
	if !command.Root().PersistentFlags().Changed("endpoint") {
		if configured, ok := os.LookupEnv("SCIM_ENDPOINT"); ok {
			endpoint = configured
		} else if profile != nil {
			endpoint = profile.URL
			endpointFromProfile = true
		}
	}
	if strings.TrimSpace(endpoint) == "" {
		return nil, endpointconfig.Endpoint{}, errors.New("select a SCIM endpoint profile, set SCIM_ENDPOINT, or pass --endpoint")
	}

	credentialProfile := profile
	if !endpointFromProfile {
		credentialProfile = nil
	}
	apiKey, err := cfg.resolveAPIKey(command, credentialProfile)
	if err != nil {
		return nil, endpointconfig.Endpoint{}, err
	}
	authHeader := cfg.setting(command, "auth-header", "SCIM_AUTH_HEADER", "Authorization", cfg.authHeader, profileValue(profile, func(value endpointconfig.Endpoint) string { return value.AuthHeader }))
	authScheme := cfg.setting(command, "auth-scheme", "SCIM_AUTH_SCHEME", "Bearer", cfg.authScheme, profileValue(profile, func(value endpointconfig.Endpoint) string { return value.AuthScheme }))
	timeoutRaw := cfg.setting(command, "timeout", "SCIM_TIMEOUT", "30s", cfg.timeout, profileValue(profile, func(value endpointconfig.Endpoint) string { return value.Timeout }))
	timeout, err := parseDuration(timeoutRaw)
	if err != nil {
		return nil, endpointconfig.Endpoint{}, fmt.Errorf("invalid timeout %q: use seconds or a duration such as 30s", timeoutRaw)
	}

	retries := cfg.readRetries
	if !command.Root().PersistentFlags().Changed("read-retries") {
		retries = 2
		if value, ok := os.LookupEnv("SCIM_READ_RETRIES"); ok {
			parsed, parseErr := strconv.Atoi(value)
			if parseErr != nil || parsed < 0 {
				return nil, endpointconfig.Endpoint{}, errors.New("SCIM_READ_RETRIES must be a non-negative integer")
			}
			retries = parsed
		} else if profile != nil {
			retries = profile.ReadRetries
		}
	}
	if retries < 0 {
		return nil, endpointconfig.Endpoint{}, errors.New("read retries must be non-negative")
	}

	client, err := scim.NewClient(
		endpoint,
		apiKey,
		authHeader,
		authScheme,
		timeout,
		scim.WithMaxReadRetries(retries),
	)
	if err != nil {
		return nil, endpointconfig.Endpoint{}, err
	}
	selected := endpointconfig.Endpoint{Name: "Command line", URL: client.Endpoint()}
	if endpointFromProfile {
		selected = *profile
	}
	return client, selected, nil
}

func (cfg *config) resolveAPIKey(command *cobra.Command, profile *endpointconfig.Endpoint) (string, error) {
	flags := command.Root().PersistentFlags()
	explicit := 0
	if flags.Changed("api-key") {
		explicit++
	}
	if flags.Changed("api-key-file") {
		explicit++
	}
	if flags.Changed("prompt-api-key") && cfg.promptAPIKey {
		explicit++
	}
	if explicit > 1 {
		return "", errors.New("use only one of --api-key, --api-key-file, or --prompt-api-key")
	}

	direct, file, prompt := cfg.apiKey, cfg.apiKeyFile, cfg.promptAPIKey
	if explicit == 0 {
		environmentKey, directSet := os.LookupEnv("SCIM_API_KEY")
		environmentFile, fileSet := os.LookupEnv("SCIM_API_KEY_FILE")
		direct = environmentKey
		file = environmentFile
		prompt = false
		if directSet && fileSet {
			return "", errors.New("set only one of SCIM_API_KEY or SCIM_API_KEY_FILE")
		}
		if !directSet && !fileSet && profile != nil {
			store, err := cfg.endpointStore()
			if err != nil {
				return "", err
			}
			return store.APIKey(*profile)
		}
	}

	if file != "" {
		contents, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("could not read API key file: %w", err)
		}
		direct = strings.TrimSpace(string(contents))
	} else if prompt {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", errors.New("cannot prompt for an API key without a terminal")
		}
		if _, err := fmt.Fprint(command.ErrOrStderr(), "SCIM API key: "); err != nil {
			return "", err
		}
		contents, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(command.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("read API key: %w", err)
		}
		direct = strings.TrimSpace(string(contents))
	}
	if direct == "" {
		return "", errors.New("select a profile with a saved key, set SCIM_API_KEY, pass --api-key-file, or use --prompt-api-key")
	}
	return direct, nil
}

func (cfg *config) setting(command *cobra.Command, flag, environment, fallback, value, profile string) string {
	if command.Root().PersistentFlags().Changed(flag) {
		return value
	}
	if configured, ok := os.LookupEnv(environment); ok {
		return configured
	}
	if cfg.profile != "" {
		return profile
	}
	return fallback
}

func (cfg *config) endpointStore() (*endpointconfig.Store, error) {
	if strings.TrimSpace(cfg.configDir) != "" {
		return endpointconfig.NewStore(cfg.configDir), nil
	}
	return endpointconfig.DefaultStore()
}

func (cfg *config) selectedProfile() (*endpointconfig.Endpoint, error) {
	if strings.TrimSpace(cfg.profile) == "" {
		return nil, nil
	}
	store, err := cfg.endpointStore()
	if err != nil {
		return nil, err
	}
	profile, err := store.Find(cfg.profile)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (cfg *config) useEndpointSelector(command *cobra.Command) bool {
	flags := command.Root().PersistentFlags()
	if strings.TrimSpace(cfg.profile) != "" || flags.Changed("endpoint") || flags.Changed("api-key") || flags.Changed("api-key-file") || (flags.Changed("prompt-api-key") && cfg.promptAPIKey) {
		return false
	}
	for _, name := range []string{"SCIM_ENDPOINT", "SCIM_API_KEY", "SCIM_API_KEY_FILE"} {
		if _, ok := os.LookupEnv(name); ok {
			return false
		}
	}
	return true
}

func profileValue(profile *endpointconfig.Endpoint, value func(endpointconfig.Endpoint) string) string {
	if profile == nil {
		return ""
	}
	return value(*profile)
}

func parseDuration(value string) (time.Duration, error) {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return 0, errors.New("duration must be positive")
		}
		return time.Duration(seconds * float64(time.Second)), nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, errors.New("invalid duration")
	}
	return duration, nil
}
