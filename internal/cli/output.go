package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode"

	"pool-skimmer/internal/scim"
)

func printJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printJSONLines(writer io.Writer, resources []scim.Resource) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, resource := range resources {
		if err := encoder.Encode(resource); err != nil {
			return err
		}
	}
	return nil
}

func printTable(writer io.Writer, resourceType string, resources []scim.Resource) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if resourceType == "Users" {
		fmt.Fprintln(table, "ID\tUSERNAME\tDISPLAY NAME\tACTIVE")
		fmt.Fprintln(table, "--\t--------\t------------\t------")
		for _, resource := range resources {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\n",
				field(resource, "id"), field(resource, "userName"), userDisplayName(resource), field(resource, "active"))
		}
	} else {
		fmt.Fprintln(table, "ID\tDISPLAY NAME\tMEMBERS")
		fmt.Fprintln(table, "--\t------------\t-------")
		for _, resource := range resources {
			fmt.Fprintf(table, "%s\t%s\t%d\n",
				field(resource, "id"), field(resource, "displayName"), arrayLength(resource["members"]))
		}
	}
	return table.Flush()
}

func printMembersTable(writer io.Writer, members []scim.Resource) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "VALUE\tDISPLAY\tTYPE")
	fmt.Fprintln(table, "-----\t-------\t----")
	for _, member := range members {
		fmt.Fprintf(table, "%s\t%s\t%s\n", field(member, "value"), field(member, "display"), field(member, "type"))
	}
	return table.Flush()
}

func field(resource scim.Resource, key string) string {
	value, ok := resource[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return safeCell(typed)
	case bool:
		return fmt.Sprint(typed)
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprint(typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return safeCell(string(data))
	}
}

func userDisplayName(resource scim.Resource) string {
	if display := field(resource, "displayName"); display != "" {
		return display
	}
	name, ok := resource["name"].(map[string]any)
	if !ok {
		return ""
	}
	parts := []string{stringField(name, "givenName"), stringField(name, "familyName")}
	return safeCell(strings.TrimSpace(strings.Join(parts, " ")))
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func arrayLength(value any) int {
	items, ok := value.([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func safeCell(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\t' || character == '\n' || character == '\r' || unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}
