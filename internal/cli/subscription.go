package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"github.com/ziyan-c/proxy-control-plane/internal/security"
)

type subscriptionTokenEnsureOptions struct {
	customerID    string
	customerEmail string
	name          string
	outputFile    string
}

type subscriptionTokenEncryptOptions struct {
	tokenID   string
	tokenFile string
}

func newSubscriptionCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscription",
		Short: "Manage subscription tokens",
	}
	cmd.AddCommand(newSubscriptionTokenCommand(rootOpts))
	return cmd
}

func newSubscriptionTokenCommand(rootOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage subscription tokens",
	}
	cmd.AddCommand(
		newSubscriptionTokenEnsureCommand(rootOpts),
		newSubscriptionTokenEncryptCommand(rootOpts),
	)
	return cmd
}

func newSubscriptionTokenEnsureCommand(rootOpts *Options) *cobra.Command {
	serviceOpts := &serviceOptions{}
	opts := &subscriptionTokenEnsureOptions{
		name: "default",
	}

	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Create one subscription token for a customer if none exists",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubscriptionTokenEnsure(cmd, rootOpts, serviceOpts, opts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&opts.customerID, "customer-id", "", "customer id to create a subscription token for")
	cmd.Flags().StringVar(&opts.customerEmail, "customer-email", "", "customer email to create a subscription token for")
	cmd.Flags().StringVar(&opts.name, "name", opts.name, "subscription token display name")
	cmd.Flags().StringVar(&opts.outputFile, "output-file", "", "write the one-time token output to this file with 0600 permissions")
	return cmd
}

func newSubscriptionTokenEncryptCommand(rootOpts *Options) *cobra.Command {
	serviceOpts := &serviceOptions{}
	opts := &subscriptionTokenEncryptOptions{}

	cmd := &cobra.Command{
		Use:   "encrypt",
		Short: "Backfill encrypted_token for an existing subscription token from a saved plaintext file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubscriptionTokenEncrypt(cmd, rootOpts, serviceOpts, opts)
		},
	}
	addServiceFlags(cmd, serviceOpts)
	cmd.Flags().StringVar(&opts.tokenID, "token-id", "", "subscription token id; defaults to token_id from --token-file")
	cmd.Flags().StringVar(&opts.tokenFile, "token-file", "", "file containing subscription_path=/sub/<token>")
	return cmd
}

func runSubscriptionTokenEnsure(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, opts *subscriptionTokenEnsureOptions) error {
	opts.customerID = strings.TrimSpace(opts.customerID)
	opts.customerEmail = strings.TrimSpace(opts.customerEmail)
	opts.name = strings.TrimSpace(opts.name)
	opts.outputFile = strings.TrimSpace(opts.outputFile)
	if opts.customerID == "" && opts.customerEmail == "" {
		return fmt.Errorf("--customer-id or --customer-email is required")
	}
	if opts.customerID != "" && opts.customerEmail != "" {
		return fmt.Errorf("use only one of --customer-id or --customer-email")
	}
	if opts.name == "" {
		opts.name = "default"
	}

	ctx := cmd.Context()
	st, cfg, err := openStoreAndConfigForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()

	customerID := opts.customerID
	if opts.customerEmail != "" {
		customer, err := st.GetCustomerByEmail(ctx, opts.customerEmail)
		if err != nil {
			return err
		}
		customerID = customer.ID
	}

	existing, err := st.ListSubscriptionTokens(ctx, customerID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		fmt.Fprintf(os.Stderr, "subscription token already exists for customer %s; existing token plaintext cannot be recovered\n", customerID)
		return nil
	}

	rawToken, err := security.NewRandomToken()
	if err != nil {
		return err
	}
	encryptedToken, err := security.EncryptStringWithBase64Key(cfg.DatabaseEncryptionKey, rawToken)
	if err != nil {
		return err
	}
	token, err := st.CreateSubscriptionToken(ctx, domain.SubscriptionToken{
		CustomerID:     customerID,
		Name:           opts.name,
		TokenHash:      security.TokenDigest(rawToken),
		EncryptedToken: encryptedToken,
		Enabled:        true,
	})
	if err != nil {
		return err
	}
	if opts.outputFile != "" {
		if err := writeSubscriptionTokenOutput(opts.outputFile, customerID, token.ID, rawToken); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "subscription token created: id=%s customer_id=%s output_file=%s\n", token.ID, customerID, opts.outputFile)
		return nil
	}
	fmt.Fprintf(os.Stdout, "subscription token created: id=%s customer_id=%s\n/sub/%s\n", token.ID, customerID, rawToken)
	return nil
}

func runSubscriptionTokenEncrypt(cmd *cobra.Command, rootOpts *Options, serviceOpts *serviceOptions, opts *subscriptionTokenEncryptOptions) error {
	opts.tokenID = strings.TrimSpace(opts.tokenID)
	opts.tokenFile = strings.TrimSpace(opts.tokenFile)
	if opts.tokenFile == "" {
		return fmt.Errorf("--token-file is required")
	}

	data, err := os.ReadFile(opts.tokenFile)
	if err != nil {
		return err
	}
	fileTokenID, rawToken, err := parseSavedSubscriptionToken(data)
	if err != nil {
		return err
	}
	if opts.tokenID == "" {
		opts.tokenID = fileTokenID
	}
	if opts.tokenID == "" {
		return fmt.Errorf("--token-id is required when token_id is not present in --token-file")
	}

	ctx := cmd.Context()
	st, cfg, err := openStoreAndConfigForCLI(ctx, cmd, rootOpts, serviceOpts)
	if err != nil {
		return err
	}
	defer st.Close()
	if strings.TrimSpace(cfg.DatabaseEncryptionKey) == "" {
		return fmt.Errorf("PCP_DATABASE_ENCRYPTION_KEY is required")
	}

	token, err := st.GetSubscriptionToken(ctx, opts.tokenID)
	if err != nil {
		return err
	}
	if token.TokenHash != security.TokenDigest(rawToken) {
		return fmt.Errorf("saved token plaintext does not match token id %s", opts.tokenID)
	}
	encryptedToken, err := security.EncryptStringWithBase64Key(cfg.DatabaseEncryptionKey, rawToken)
	if err != nil {
		return err
	}
	token.EncryptedToken = encryptedToken
	if _, err := st.UpdateSubscriptionToken(ctx, token); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "subscription token encrypted: id=%s\n", opts.tokenID)
	return nil
}

func writeSubscriptionTokenOutput(path string, customerID string, tokenID string, rawToken string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "customer_id=%s\ntoken_id=%s\nsubscription_path=/sub/%s\n", customerID, tokenID, rawToken)
	return err
}

func parseSavedSubscriptionToken(data []byte) (string, string, error) {
	var tokenID string
	var rawToken string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "token_id="):
			tokenID = strings.TrimSpace(strings.TrimPrefix(line, "token_id="))
		case strings.HasPrefix(line, "subscription_path="):
			rawToken = subscriptionTokenFromPath(strings.TrimSpace(strings.TrimPrefix(line, "subscription_path=")))
		case rawToken == "":
			rawToken = subscriptionTokenFromPath(line)
		}
	}
	if rawToken == "" {
		return "", "", fmt.Errorf("token file does not contain subscription_path=/sub/<token>")
	}
	return tokenID, rawToken, nil
}

func subscriptionTokenFromPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	if i := strings.Index(value, "?"); i >= 0 {
		value = value[:i]
	}
	if i := strings.Index(value, "/sub/"); i >= 0 {
		return strings.TrimSpace(value[i+len("/sub/"):])
	}
	if strings.Contains(value, "/") {
		return ""
	}
	return value
}
