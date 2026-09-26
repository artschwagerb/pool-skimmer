package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"pool-skimmer/internal/scim"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func newDoctorCommand(cfg *config) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check endpoint authentication and core SCIM resources",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if output != "table" && output != "json" {
				return fmt.Errorf("unsupported output %q; use table or json", output)
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			checks := []doctorCheck{{Name: "configuration", Status: "ok", Detail: client.Endpoint()}}
			failed := false
			if _, err := client.Discovery(command.Context(), "ServiceProviderConfig"); err != nil {
				checks = append(checks, doctorCheck{Name: "ServiceProviderConfig", Status: "failed", Detail: err.Error()})
				failed = true
			} else {
				checks = append(checks, doctorCheck{Name: "ServiceProviderConfig", Status: "ok"})
			}
			for _, resourceType := range []string{"Users", "Groups"} {
				_, err := client.List(command.Context(), resourceType, scim.ListOptions{Count: 1})
				if err != nil {
					checks = append(checks, doctorCheck{Name: resourceType, Status: "failed", Detail: err.Error()})
					failed = true
				} else {
					checks = append(checks, doctorCheck{Name: resourceType, Status: "ok"})
				}
			}

			if output == "json" {
				err = printJSON(command.OutOrStdout(), map[string]any{"endpoint": client.Endpoint(), "checks": checks})
			} else {
				table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(table, "CHECK\tSTATUS\tDETAIL")
				fmt.Fprintln(table, "-----\t------\t------")
				for _, check := range checks {
					fmt.Fprintf(table, "%s\t%s\t%s\n", safeCell(check.Name), safeCell(check.Status), safeCell(check.Detail))
				}
				err = table.Flush()
			}
			if err != nil {
				return err
			}
			if failed {
				return fmt.Errorf("one or more SCIM checks failed")
			}
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "table", "output format: table or json")
	return command
}
