package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pool-skimmer/internal/exportfile"
	"pool-skimmer/internal/scim"
)

type projectionFlags struct {
	attributes         []string
	excludedAttributes []string
}

func newResourceCommand(cfg *config, resourceType string) *cobra.Command {
	name := strings.ToLower(resourceType)
	command := &cobra.Command{
		Use:     name,
		Aliases: []string{strings.TrimSuffix(name, "s")},
		Short:   "Manage SCIM " + name,
	}
	command.AddCommand(newListCommand(cfg, resourceType))
	command.AddCommand(newExportCommand(cfg, resourceType))
	command.AddCommand(newGetCommand(cfg, resourceType))
	command.AddCommand(newCreateCommand(cfg, resourceType))
	command.AddCommand(newPatchCommand(cfg, resourceType))
	command.AddCommand(newReplaceCommand(cfg, resourceType))
	command.AddCommand(newDeleteCommand(cfg, resourceType))
	if resourceType == "Users" {
		command.AddCommand(newActiveCommand(cfg, true))
		command.AddCommand(newActiveCommand(cfg, false))
	} else {
		command.AddCommand(newRenameCommand(cfg))
		command.AddCommand(newMembersCommand(cfg))
	}
	return command
}

func newExportCommand(cfg *config, resourceType string) *cobra.Command {
	var filter string
	var startIndex, count int
	var force bool
	projection := projectionFlags{}
	command := &cobra.Command{
		Use:   "export FILE",
		Short: "Export all " + strings.ToLower(resourceType) + " as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if startIndex < 1 || count < 1 {
				return errors.New("--start-index and --count must be positive")
			}
			if err := exportfile.ValidatePath(args[0], force); err != nil {
				return err
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			resources, err := client.ListAll(command.Context(), resourceType, scim.ListOptions{
				Filter: filter, StartIndex: startIndex, Count: count,
				Attributes: projection.attributes, ExcludedAttributes: projection.excludedAttributes,
			})
			if err != nil {
				return err
			}
			if err := exportfile.WriteJSON(args[0], resources, force); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Exported %d %s to %q.\n", len(resources), strings.ToLower(resourceType), args[0])
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&filter, "filter", "", "SCIM filter expression")
	flags.IntVar(&startIndex, "start-index", 1, "1-based starting index")
	flags.IntVar(&count, "count", 100, "page size")
	flags.BoolVar(&force, "force", false, "replace an existing export file")
	addProjectionFlags(command, &projection)
	return command
}

func newListCommand(cfg *config, resourceType string) *cobra.Command {
	var filter string
	var startIndex, count int
	var everyPage bool
	var output string
	projection := projectionFlags{}
	command := &cobra.Command{
		Use:   "list",
		Short: "List " + strings.ToLower(resourceType),
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if startIndex < 1 || count < 1 {
				return errors.New("--start-index and --count must be positive")
			}
			if err := validateOutput(output); err != nil {
				return err
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			options := scim.ListOptions{
				Filter: filter, StartIndex: startIndex, Count: count,
				Attributes: projection.attributes, ExcludedAttributes: projection.excludedAttributes,
			}

			var document map[string]any
			var resources []scim.Resource
			if everyPage {
				resources, err = client.ListAll(command.Context(), resourceType, options)
				document = map[string]any{
					"schemas": []string{scim.ListResponseSchema}, "totalResults": len(resources),
					"startIndex": startIndex, "itemsPerPage": len(resources), "Resources": resources,
				}
			} else {
				document, err = client.List(command.Context(), resourceType, options)
				if err == nil {
					resources, err = scim.Resources(document)
				}
			}
			if err != nil {
				return err
			}

			switch output {
			case "json":
				return printJSON(command.OutOrStdout(), document)
			case "jsonl":
				return printJSONLines(command.OutOrStdout(), resources)
			default:
				if err := printTable(command.OutOrStdout(), resourceType, resources); err != nil {
					return err
				}
				if !everyPage && totalResults(document) > len(resources) {
					_, err = fmt.Fprintf(command.ErrOrStderr(), "\nShowing %d of %d; pass --all to retrieve every page.\n", len(resources), totalResults(document))
				}
				return err
			}
		},
	}
	flags := command.Flags()
	flags.StringVar(&filter, "filter", "", "SCIM filter expression")
	flags.IntVar(&startIndex, "start-index", 1, "1-based starting index")
	flags.IntVar(&count, "count", 100, "page size")
	flags.BoolVar(&everyPage, "all", false, "retrieve all remaining pages")
	flags.StringVarP(&output, "output", "o", "table", "output format: table, json, or jsonl")
	addProjectionFlags(command, &projection)
	return command
}

func newGetCommand(cfg *config, resourceType string) *cobra.Command {
	projection := projectionFlags{}
	command := &cobra.Command{
		Use:   "get ID",
		Short: "Get one resource by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			resource, err := client.Get(command.Context(), resourceType, args[0], projection.attributes, projection.excludedAttributes)
			if err != nil {
				return err
			}
			return printJSON(command.OutOrStdout(), resource)
		},
	}
	addProjectionFlags(command, &projection)
	return command
}

func newCreateCommand(cfg *config, resourceType string) *cobra.Command {
	var data string
	var dryRun bool
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a resource with SCIM POST",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			document, err := objectData(command, data, "create")
			if err != nil {
				return err
			}
			if dryRun {
				return printPlan(command, "POST", resourceType, "", document)
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			response, err := client.Create(command.Context(), resourceType, document)
			if err != nil {
				return err
			}
			return printMutation(command, response)
		},
	}
	command.Flags().StringVar(&data, "data", "", "resource JSON: inline, @FILE, or - for stdin")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without contacting the endpoint")
	_ = command.MarkFlagRequired("data")
	return command
}

func newPatchCommand(cfg *config, resourceType string) *cobra.Command {
	var adds, replacements, removals []string
	var data, ifMatch string
	var dryRun bool
	command := &cobra.Command{
		Use:   "patch ID",
		Short: "Modify selected attributes with SCIM PATCH",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			document, err := patchData(command, data, adds, replacements, removals)
			if err != nil {
				return err
			}
			if dryRun {
				return printPlan(command, "PATCH", resourceType+"/"+args[0], ifMatch, document)
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			response, err := client.Patch(command.Context(), resourceType, args[0], document, ifMatch)
			if err != nil {
				return err
			}
			return printMutation(command, response)
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&adds, "add", nil, "add PATH=VALUE (repeatable; VALUE may be JSON)")
	flags.StringArrayVar(&replacements, "replace", nil, "replace PATH=VALUE (repeatable; VALUE may be JSON)")
	flags.StringArrayVar(&removals, "remove", nil, "remove PATH (repeatable)")
	flags.StringVar(&data, "data", "", "PatchOp object or Operations array: inline, @FILE, or -")
	flags.StringVar(&ifMatch, "if-match", "", "only update the matching ETag")
	flags.BoolVar(&dryRun, "dry-run", false, "print the request without contacting the endpoint")
	return command
}

func newReplaceCommand(cfg *config, resourceType string) *cobra.Command {
	var data, ifMatch string
	var dryRun bool
	command := &cobra.Command{
		Use:   "replace ID",
		Short: "Replace a complete resource with SCIM PUT",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			document, err := objectData(command, data, "replace")
			if err != nil {
				return err
			}
			if dryRun {
				return printPlan(command, "PUT", resourceType+"/"+args[0], ifMatch, document)
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			response, err := client.Replace(command.Context(), resourceType, args[0], document, ifMatch)
			if err != nil {
				return err
			}
			return printMutation(command, response)
		},
	}
	flags := command.Flags()
	flags.StringVar(&data, "data", "", "resource JSON: inline, @FILE, or - for stdin")
	flags.StringVar(&ifMatch, "if-match", "", "only update the matching ETag")
	flags.BoolVar(&dryRun, "dry-run", false, "print the request without contacting the endpoint")
	_ = command.MarkFlagRequired("data")
	return command
}

func newDeleteCommand(cfg *config, resourceType string) *cobra.Command {
	var ifMatch string
	var yes, dryRun bool
	command := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if dryRun {
				return printPlan(command, "DELETE", resourceType+"/"+args[0], ifMatch, nil)
			}
			if !yes {
				if err := confirmDelete(command, resourceType, args[0]); err != nil {
					return err
				}
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			if err := client.Delete(command.Context(), resourceType, args[0], ifMatch); err != nil {
				return err
			}
			return printJSON(command.OutOrStdout(), map[string]any{"status": "deleted", "resource": resourceType, "id": args[0]})
		},
	}
	flags := command.Flags()
	flags.StringVar(&ifMatch, "if-match", "", "only delete the matching ETag")
	flags.BoolVarP(&yes, "yes", "y", false, "skip the interactive confirmation")
	flags.BoolVar(&dryRun, "dry-run", false, "print the request without contacting the endpoint")
	return command
}

func newActiveCommand(cfg *config, active bool) *cobra.Command {
	action := "activate"
	if !active {
		action = "deactivate"
	}
	var ifMatch string
	var dryRun, noVerify bool
	command := &cobra.Command{
		Use:   action + " ID",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			document := patchDocument([]any{map[string]any{"op": "replace", "path": "active", "value": active}})
			return executeConveniencePatch(command, cfg, "Users", args[0], document, ifMatch, dryRun, noVerify)
		},
	}
	addConvenienceFlags(command, &ifMatch, &dryRun, &noVerify)
	return command
}

func newRenameCommand(cfg *config) *cobra.Command {
	var ifMatch string
	var dryRun, noVerify bool
	command := &cobra.Command{
		Use:   "rename ID NEW_NAME",
		Short: "Change a group's SCIM displayName",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(args[1]) == "" {
				return errors.New("new group name must not be empty")
			}
			document := patchDocument([]any{map[string]any{"op": "replace", "path": "displayName", "value": args[1]}})
			return executeConveniencePatch(command, cfg, "Groups", args[0], document, ifMatch, dryRun, noVerify)
		},
	}
	addConvenienceFlags(command, &ifMatch, &dryRun, &noVerify)
	return command
}

func newMembersCommand(cfg *config) *cobra.Command {
	command := &cobra.Command{Use: "members", Short: "Manage group membership"}
	command.AddCommand(newMembersListCommand(cfg))
	command.AddCommand(newMemberMutationCommand(cfg, true))
	command.AddCommand(newMemberMutationCommand(cfg, false))
	return command
}

func newMembersListCommand(cfg *config) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "list GROUP_ID",
		Short: "List group member references",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := validateOutput(output); err != nil {
				return err
			}
			client, _, err := cfg.client(command)
			if err != nil {
				return err
			}
			group, err := client.Get(command.Context(), "Groups", args[0], []string{"id", "displayName", "members"}, nil)
			if err != nil {
				return err
			}
			members, err := resourceArray(group["members"])
			if err != nil {
				return err
			}
			switch output {
			case "json":
				return printJSON(command.OutOrStdout(), members)
			case "jsonl":
				return printJSONLines(command.OutOrStdout(), members)
			default:
				return printMembersTable(command.OutOrStdout(), members)
			}
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "table", "output format: table, json, or jsonl")
	return command
}

func newMemberMutationCommand(cfg *config, add bool) *cobra.Command {
	action := "add"
	if !add {
		action = "remove"
	}
	var ifMatch string
	var dryRun, noVerify bool
	command := &cobra.Command{
		Use:   action + " GROUP_ID USER_ID",
		Short: strings.ToUpper(action[:1]) + action[1:] + " a user reference",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			var operation map[string]any
			if add {
				operation = map[string]any{"op": "add", "path": "members", "value": []any{map[string]any{"value": args[1]}}}
			} else {
				path := fmt.Sprintf(`members[value eq "%s"]`, escapeFilterString(args[1]))
				operation = map[string]any{"op": "remove", "path": path}
			}
			document := patchDocument([]any{operation})
			return executeConveniencePatch(command, cfg, "Groups", args[0], document, ifMatch, dryRun, noVerify)
		},
	}
	addConvenienceFlags(command, &ifMatch, &dryRun, &noVerify)
	return command
}

func executeConveniencePatch(
	command *cobra.Command,
	cfg *config,
	resourceType, id string,
	document map[string]any,
	ifMatch string,
	dryRun, noVerify bool,
) error {
	if dryRun {
		return printPlan(command, "PATCH", resourceType+"/"+id, ifMatch, document)
	}
	client, _, err := cfg.client(command)
	if err != nil {
		return err
	}
	response, err := client.Patch(command.Context(), resourceType, id, document, ifMatch)
	if err != nil {
		return err
	}
	if noVerify {
		return printMutation(command, response)
	}
	verified, err := client.Get(command.Context(), resourceType, id, nil, nil)
	if err != nil {
		return fmt.Errorf("update succeeded but read-back verification failed: %w", err)
	}
	return printJSON(command.OutOrStdout(), verified)
}

func addConvenienceFlags(command *cobra.Command, ifMatch *string, dryRun, noVerify *bool) {
	command.Flags().StringVar(ifMatch, "if-match", "", "only update the matching ETag")
	command.Flags().BoolVar(dryRun, "dry-run", false, "print the request without contacting the endpoint")
	command.Flags().BoolVar(noVerify, "no-verify", false, "skip the read-back verification")
}

func addProjectionFlags(command *cobra.Command, flags *projectionFlags) {
	command.Flags().StringSliceVar(&flags.attributes, "attributes", nil, "comma-separated attributes to return")
	command.Flags().StringSliceVar(&flags.excludedAttributes, "excluded-attributes", nil, "comma-separated attributes to omit")
}

func printMutation(command *cobra.Command, resource scim.Resource) error {
	if resource == nil {
		return printJSON(command.OutOrStdout(), map[string]string{"status": "success"})
	}
	return printJSON(command.OutOrStdout(), resource)
}

func printPlan(command *cobra.Command, method, path, ifMatch string, body any) error {
	plan := map[string]any{"dryRun": true, "method": method, "path": path}
	if ifMatch != "" {
		plan["ifMatch"] = ifMatch
	}
	if body != nil {
		plan["body"] = body
	}
	return printJSON(command.OutOrStdout(), plan)
}

func confirmDelete(command *cobra.Command, resourceType, id string) error {
	input, ok := command.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return errors.New("refusing non-interactive delete without --yes")
	}
	if _, err := fmt.Fprintf(command.ErrOrStderr(), "Delete %s %q? [y/N] ", strings.ToLower(strings.TrimSuffix(resourceType, "s")), id); err != nil {
		return err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return errors.New("delete cancelled")
	}
	return nil
}

func resourceArray(value any) ([]scim.Resource, error) {
	if value == nil {
		return []scim.Resource{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("members was not an array")
	}
	resources := make([]scim.Resource, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("members contained a non-object value")
		}
		resources = append(resources, scim.Resource(object))
	}
	return resources, nil
}

func totalResults(document map[string]any) int {
	switch value := document["totalResults"].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case interface{ Int64() (int64, error) }:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

func validateOutput(output string) error {
	if output != "table" && output != "json" && output != "jsonl" {
		return fmt.Errorf("unsupported output %q; use table, json, or jsonl", output)
	}
	return nil
}

func escapeFilterString(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`)
}
