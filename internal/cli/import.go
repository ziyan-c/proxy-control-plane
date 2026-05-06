package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ziyan-c/proxy-control-plane/internal/config"
	"github.com/ziyan-c/proxy-control-plane/internal/store"
	"github.com/ziyan-c/proxy-control-plane/internal/subscription"
	"github.com/ziyan-c/proxy-control-plane/internal/xrayconfig"
)

type importXrayConfigOptions struct {
	file                string
	nodeName            string
	inboundTag          string
	customerEmail       string
	customerDisplayName string
	emailTagPrefix      string
	dryRun              bool
}

type importSubscriptionFileOptions struct {
	file                string
	publicPath          string
	aliasName           string
	sourcePath          string
	customerEmail       string
	customerDisplayName string
	emailTagPrefix      string
	dryRun              bool
}

func newImportCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import existing runtime state into PostgreSQL",
	}
	cmd.AddCommand(
		newImportXrayConfigCommand(rootOpts),
		newImportSubscriptionFileCommand(rootOpts),
	)
	return cmd
}

func newImportXrayConfigCommand(rootOpts *Options) *cobra.Command {
	serviceOpts := &serviceOptions{}
	importOpts := &importXrayConfigOptions{
		inboundTag:          "proxy-control-plane-vless-in",
		customerEmail:       "legacy-public@proxy-control-plane.local",
		customerDisplayName: "Legacy public accounts",
		emailTagPrefix:      "legacy-public",
	}

	cmd := &cobra.Command{
		Use:   "xray-config",
		Short: "Import static VLESS clients from an Xray config.json file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportXrayConfig(cmd, rootOpts, serviceOpts, importOpts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&importOpts.file, "file", "", "local Xray config.json file to import")
	cmd.Flags().StringVar(&importOpts.nodeName, "node", "", "existing proxy node name to bind imported clients to")
	cmd.Flags().StringVar(&importOpts.inboundTag, "inbound-tag", importOpts.inboundTag, "VLESS inbound tag to import")
	cmd.Flags().StringVar(&importOpts.customerEmail, "customer-email", importOpts.customerEmail, "legacy customer email to create or reuse")
	cmd.Flags().StringVar(&importOpts.customerDisplayName, "customer-name", importOpts.customerDisplayName, "legacy customer display name")
	cmd.Flags().StringVar(&importOpts.emailTagPrefix, "email-tag-prefix", importOpts.emailTagPrefix, "email_tag prefix for imported proxy accounts")
	cmd.Flags().BoolVar(&importOpts.dryRun, "dry-run", false, "parse the file and print counts without writing PostgreSQL")
	return cmd
}

func newImportSubscriptionFileCommand(rootOpts *Options) *cobra.Command {
	serviceOpts := &serviceOptions{}
	importOpts := &importSubscriptionFileOptions{
		customerEmail:       "legacy-public@proxy-control-plane.local",
		customerDisplayName: "Legacy public accounts",
		emailTagPrefix:      "legacy-public",
	}

	cmd := &cobra.Command{
		Use:   "subscription-file",
		Short: "Import a legacy public VLESS subscription file and register its compatibility path",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportSubscriptionFile(cmd, rootOpts, serviceOpts, importOpts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&importOpts.file, "file", "", "local legacy public subscription file to import")
	cmd.Flags().StringVar(&importOpts.publicPath, "public-path", "", "original public URL path on Caddy, for example /public/vless.txt")
	cmd.Flags().StringVar(&importOpts.aliasName, "alias-name", "", "display name for the compatibility subscription path")
	cmd.Flags().StringVar(&importOpts.sourcePath, "source-path", "", "source path label to store; defaults to --file")
	cmd.Flags().StringVar(&importOpts.customerEmail, "customer-email", importOpts.customerEmail, "legacy customer email to create or reuse")
	cmd.Flags().StringVar(&importOpts.customerDisplayName, "customer-name", importOpts.customerDisplayName, "legacy customer display name")
	cmd.Flags().StringVar(&importOpts.emailTagPrefix, "email-tag-prefix", importOpts.emailTagPrefix, "email_tag prefix for imported proxy accounts")
	cmd.Flags().BoolVar(&importOpts.dryRun, "dry-run", false, "parse the file and print counts without writing PostgreSQL")
	return cmd
}

func runImportXrayConfig(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, importOpts *importXrayConfigOptions) error {
	importOpts.file = strings.TrimSpace(importOpts.file)
	importOpts.nodeName = strings.TrimSpace(importOpts.nodeName)
	importOpts.customerEmail = strings.TrimSpace(importOpts.customerEmail)
	importOpts.customerDisplayName = strings.TrimSpace(importOpts.customerDisplayName)
	importOpts.emailTagPrefix = strings.TrimSpace(importOpts.emailTagPrefix)
	if importOpts.file == "" {
		return fmt.Errorf("--file is required")
	}
	if importOpts.nodeName == "" {
		return fmt.Errorf("--node is required")
	}
	if importOpts.customerEmail == "" {
		return fmt.Errorf("--customer-email is required")
	}
	if importOpts.emailTagPrefix == "" {
		importOpts.emailTagPrefix = "legacy-public"
	}

	data, err := os.ReadFile(importOpts.file)
	if err != nil {
		return fmt.Errorf("read Xray config %s: %w", importOpts.file, err)
	}
	clients, err := xrayconfig.VLESSClients(data, importOpts.inboundTag)
	if err != nil {
		return err
	}

	accounts := make([]store.LegacyProxyAccountInput, 0, len(clients))
	for _, client := range clients {
		accounts = append(accounts, store.LegacyProxyAccountInput{
			UUID:     client.ID,
			EmailTag: importOpts.emailTagPrefix + "-" + shortImportValue(client.ID),
			Flow:     client.Flow,
		})
	}

	fmt.Fprintf(os.Stderr, "parsed %d VLESS client(s) from %s for node %s\n", len(accounts), importOpts.file, importOpts.nodeName)
	if importOpts.dryRun {
		fmt.Fprintln(os.Stderr, "dry run: no database changes")
		return nil
	}

	ctx := cmd.Context()
	st, err := openStoreForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()

	result, err := st.ImportLegacyProxyAccounts(ctx, store.LegacyProxyAccountImport{
		CustomerEmail:       importOpts.customerEmail,
		CustomerDisplayName: importOpts.customerDisplayName,
		NodeName:            importOpts.nodeName,
		Accounts:            accounts,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"import complete: customer_created=%d account_created=%d account_existing=%d node_bindings_created=%d skipped=%d\n",
		result.CustomerCreated,
		result.AccountCreated,
		result.AccountExisting,
		result.NodeBindings,
		result.Skipped,
	)
	return nil
}

func runImportSubscriptionFile(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, importOpts *importSubscriptionFileOptions) error {
	importOpts.file = strings.TrimSpace(importOpts.file)
	importOpts.publicPath = strings.TrimSpace(importOpts.publicPath)
	importOpts.aliasName = strings.TrimSpace(importOpts.aliasName)
	importOpts.sourcePath = strings.TrimSpace(importOpts.sourcePath)
	importOpts.customerEmail = strings.TrimSpace(importOpts.customerEmail)
	importOpts.customerDisplayName = strings.TrimSpace(importOpts.customerDisplayName)
	importOpts.emailTagPrefix = strings.TrimSpace(importOpts.emailTagPrefix)
	if importOpts.file == "" {
		return fmt.Errorf("--file is required")
	}
	if importOpts.publicPath == "" {
		return fmt.Errorf("--public-path is required")
	}
	if importOpts.customerEmail == "" {
		return fmt.Errorf("--customer-email is required")
	}
	if importOpts.emailTagPrefix == "" {
		importOpts.emailTagPrefix = "legacy-public"
	}
	if importOpts.sourcePath == "" {
		importOpts.sourcePath = importOpts.file
	}

	data, err := os.ReadFile(importOpts.file)
	if err != nil {
		return fmt.Errorf("read subscription file %s: %w", importOpts.file, err)
	}
	parsed, err := subscription.ParseLinks(data)
	if err != nil {
		return err
	}
	links := parsed.VLESSLinks
	if len(links) == 0 {
		return fmt.Errorf("subscription file %s does not contain VLESS links", importOpts.file)
	}

	inputLinks := make([]store.LegacySubscriptionLinkInput, 0, len(links))
	for _, link := range links {
		inputLinks = append(inputLinks, store.LegacySubscriptionLinkInput{
			UUID:             link.UUID,
			EmailTag:         link.EmailTag,
			Flow:             link.Flow,
			Host:             link.Host,
			Port:             link.Port,
			Transport:        link.Transport,
			Security:         link.Security,
			Path:             link.Path,
			HostHeader:       link.HostHeader,
			RealityPublicKey: link.RealityPublicKey,
			RealityShortID:   link.RealityShortID,
		})
	}

	sourceHash := sha256.Sum256(data)
	sourceSHA := hex.EncodeToString(sourceHash[:])
	fmt.Fprintf(os.Stderr, "parsed %d VLESS link(s) from %s for legacy path %s\n", len(inputLinks), importOpts.file, importOpts.publicPath)
	if parsed.UnsupportedURIs > 0 {
		fmt.Fprintf(os.Stderr, "warning: skipped %d unsupported non-VLESS subscription URI(s)\n", parsed.UnsupportedURIs)
	}
	if importOpts.dryRun {
		fmt.Fprintf(os.Stderr, "source_sha256=%s\n", sourceSHA)
		fmt.Fprintln(os.Stderr, "dry run: no database changes")
		return nil
	}

	ctx := cmd.Context()
	st, err := openStoreForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()

	result, err := st.ImportLegacySubscriptionFile(ctx, store.LegacySubscriptionFileImport{
		CustomerEmail:       importOpts.customerEmail,
		CustomerDisplayName: importOpts.customerDisplayName,
		PublicPath:          importOpts.publicPath,
		AliasName:           importOpts.aliasName,
		SourcePath:          importOpts.sourcePath,
		SourceSHA256:        sourceSHA,
		EmailTagPrefix:      importOpts.emailTagPrefix,
		Links:               inputLinks,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"import complete: customer_created=%d alias_created=%d alias_updated=%d account_created=%d account_existing=%d node_bindings_created=%d unmatched_links=%d skipped=%d\n",
		result.CustomerCreated,
		result.AliasCreated,
		result.AliasUpdated,
		result.AccountCreated,
		result.AccountExisting,
		result.NodeBindings,
		result.UnmatchedLinks,
		result.Skipped,
	)
	if result.UnmatchedLinks > 0 {
		fmt.Fprintln(os.Stderr, "warning: some VLESS links did not match existing proxy_nodes; register nodes first, then rerun this import")
	}
	return nil
}

func openStoreForCLI(ctx context.Context, cmd *cobra.Command, rootOpts *Options, opts *serviceOptions) (*store.Store, error) {
	if !opts.noLocalConfig || opts.envFile != "" {
		if !opts.noLocalConfig {
			if err := initLocal(rootOpts.ConfigDir, effectiveExampleDir(cmd, rootOpts)); err != nil {
				return nil, err
			}
		}

		envFile := opts.envFile
		if envFile == "" {
			envFile = appEnvFile(rootOpts.ConfigDir)
		}
		if err := loadEnvFile(envFile, true); err != nil {
			return nil, err
		}
	}

	cfg := config.Load()
	if err := applyServiceOptions(&cfg, opts); err != nil {
		return nil, err
	}
	return store.Open(ctx, cfg.DatabaseURL, cfg.AutoCreateDatabase)
}

func shortImportValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[len(value)-8:]
}
