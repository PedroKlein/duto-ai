package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/publisher"
	githubtool "github.com/PedroKlein/duto-ai/internal/tool/github"
)

var errPublishUnavailable = errors.New("publisher is unavailable")

type publishInput struct {
	ConfigPath          string
	ControlEvidencePath string
	BundlePath          string
	ExpectedBundleSHA   string
	PermissionProfile   string
	ReceiptPath         string
	Format              outputFormat
}

type publishBundle func(context.Context, publishInput) (*publisher.Receipt, error)

func newPublishCommand(run publishBundle) *cobra.Command {
	var (
		input       publishInput
		formatValue string
	)

	command := &cobra.Command{
		Use:          commandPublish,
		Args:         noArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			format, err := parseFormat(formatValue)
			if err != nil {
				return usageError(err)
			}

			input.Format = format

			if err := validatePublishFlags(input); err != nil {
				return usageError(err)
			}

			if run == nil {
				return internalError(errPublishUnavailable)
			}

			receipt, publishErr := run(command.Context(), input)
			if receipt != nil {
				if err := publisher.WriteReceipt(input.ReceiptPath, receipt); err != nil {
					return internalError(err)
				}

				output, err := formatPublisherReceipt(receipt, input.Format)
				if err != nil {
					return internalError(err)
				}

				if err := writePayload(command.OutOrStdout(), output); err != nil {
					return err
				}
			}

			if publishErr != nil {
				switch {
				case errors.Is(publishErr, publisher.ErrRejected):
					return admissionError(publishErr)
				case errors.Is(publishErr, publisher.ErrConflict):
					return executionError(publishErr)
				default:
					return internalError(publishErr)
				}
			}

			return nil
		},
	}

	command.Flags().StringVar(&input.ConfigPath, "config", "", "trusted runtime configuration")
	command.Flags().StringVar(&input.ControlEvidencePath, "control-evidence", "", "current trusted control-evidence JSON file")
	command.Flags().StringVar(&input.BundlePath, "bundle", "", "verified M3 bundle directory")
	command.Flags().StringVar(&input.ExpectedBundleSHA, "expected-bundle-sha256", "", "expected manifest SHA-256")
	command.Flags().StringVar(&input.PermissionProfile, "permission-profile", "", "publisher permission profile: reply or branch-pr")
	command.Flags().StringVar(&input.ReceiptPath, "receipt", "", "publisher receipt file")
	command.Flags().StringVar(&formatValue, "format", string(formatText), "output format: text or json")
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })

	return command
}

func validatePublishFlags(input publishInput) error {
	if input.ConfigPath == "" || input.ControlEvidencePath == "" || input.BundlePath == "" || input.ExpectedBundleSHA == "" || input.PermissionProfile == "" || input.ReceiptPath == "" {
		return errPublishFlagsRequired
	}

	return nil
}

func runPublisher(ctx context.Context, input publishInput) (*publisher.Receipt, error) {
	cfg, err := config.LoadConfig(input.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading publisher config: %w", err)
	}

	verified, err := publisher.Verify(publisher.VerifyInput{
		Config: cfg, ControlEvidencePath: input.ControlEvidencePath, BundlePath: input.BundlePath,
		ExpectedBundleSHA256: input.ExpectedBundleSHA, PermissionProfile: input.PermissionProfile, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("verifying publisher bundle: %w", err)
	}

	remote, err := publisherRemote(verified)
	if err != nil {
		return nil, err
	}

	receipt, err := verified.Publish(ctx, remote)
	if err != nil {
		return receipt, fmt.Errorf("publishing verified bundle: %w", err)
	}

	return receipt, nil
}

func publisherRemote(verified *publisher.Verified) (publisher.Remote, error) {
	if statePath := os.Getenv("DUTO_TEST_PUBLISH_STATE"); statePath != "" {
		remote, err := publisher.NewFileRemote(statePath)
		if err != nil {
			return nil, fmt.Errorf("constructing file remote: %w", err)
		}

		return remote, nil
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, publisher.ErrRejected
	}

	baseURL := os.Getenv("GITHUB_API_URL")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	owner, repository := verified.Repository()

	remote, err := githubtool.NewPublisher(githubtool.PublisherPolicy{
		BaseURL: baseURL, Token: token, Owner: owner, Repository: repository,
	})
	if err != nil {
		return nil, fmt.Errorf("constructing publisher adapter: %w", err)
	}

	return remote, nil
}

func formatPublisherReceipt(receipt *publisher.Receipt, format outputFormat) ([]byte, error) {
	if format == formatJSON {
		encoded, err := receipt.JSON()
		if err != nil {
			return nil, fmt.Errorf("encoding publisher receipt: %w", err)
		}

		return encoded, nil
	}

	encoded, err := receipt.Text()
	if err != nil {
		return nil, fmt.Errorf("encoding publisher receipt: %w", err)
	}

	return encoded, nil
}
