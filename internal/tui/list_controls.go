package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"pool-skimmer/internal/scim"
)

type sortSpec struct {
	key        string
	descending bool
}

type sortOption struct {
	key   string
	label string
}

var (
	userSortOptions = []sortOption{
		{key: "name", label: "Name"},
		{key: "username", label: "Username"},
		{key: "status", label: "Status"},
	}
	groupSortOptions = []sortOption{
		{key: "name", label: "Group"},
		{key: "members", label: "Members"},
		{key: "id", label: "ID"},
	}
)

func defaultSort(resourceType string) sortSpec {
	return sortSpec{key: "name"}
}

func sortOptions(resourceType string) []sortOption {
	if resourceType == "Users" {
		return userSortOptions
	}
	return groupSortOptions
}

func (m Model) currentFilter() string {
	if m.activeType == "Users" {
		return m.userFilter
	}
	return m.groupFilter
}

func (m *Model) setCurrentFilter(value string) {
	if m.activeType == "Users" {
		m.userFilter = value
		return
	}
	m.groupFilter = value
}

func (m Model) currentSort() sortSpec {
	if m.activeType == "Users" {
		return m.userSort
	}
	return m.groupSort
}

func (m *Model) setCurrentSort(spec sortSpec) {
	if m.activeType == "Users" {
		m.userSort = spec
		return
	}
	m.groupSort = spec
}

func (m *Model) resetListControls() {
	m.userFilter = ""
	m.groupFilter = ""
	m.userSort = defaultSort("Users")
	m.groupSort = defaultSort("Groups")
	m.sortIndex = 0
	m.sortDescending = false
}

func (m Model) sortColumnTitle(title, key string) string {
	spec := m.currentSort()
	if spec.key != key {
		return title
	}
	if spec.descending {
		return title + " ↓"
	}
	return title + " ↑"
}

type filterValueKind uint8

const (
	filterString filterValueKind = iota
	filterBool
	filterStatus
	filterInteger
)

type filterField struct {
	key  string
	kind filterValueKind
}

type filterClause struct {
	field    filterField
	operator string
	value    any
}

type localFilter struct {
	text    []string
	clauses []filterClause
}

func parseLocalFilter(resourceType, input string) (localFilter, error) {
	terms, err := splitFilterTerms(input)
	if err != nil {
		return localFilter{}, err
	}
	if len(terms) == 0 {
		return localFilter{}, nil
	}

	parsed := localFilter{}
	structured := false
	for _, term := range terms {
		clause, found, err := parseFilterClause(resourceType, term)
		if err != nil {
			return localFilter{}, err
		}
		if found {
			structured = true
			parsed.clauses = append(parsed.clauses, clause)
			continue
		}
		parsed.text = append(parsed.text, strings.ToLower(term))
	}
	if !structured {
		parsed.text = []string{strings.ToLower(strings.Join(terms, " "))}
	}
	return parsed, nil
}

func splitFilterTerms(input string) ([]string, error) {
	var terms []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			terms = append(terms, current.String())
			current.Reset()
		}
	}

	for _, character := range strings.TrimSpace(input) {
		if escaped {
			current.WriteRune(character)
			escaped = false
			continue
		}
		if quote != 0 {
			switch character {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			default:
				current.WriteRune(character)
			}
			continue
		}
		switch {
		case character == '\'' || character == '"':
			quote = character
		case unicode.IsSpace(character):
			flush()
		default:
			current.WriteRune(character)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("filter contains an unterminated quoted value")
	}
	if escaped {
		return nil, fmt.Errorf("filter ends with an incomplete escape")
	}
	flush()
	return terms, nil
}

func parseFilterClause(resourceType, term string) (filterClause, bool, error) {
	for _, operator := range []string{"!=", ">=", "<=", "~", "=", ">", "<"} {
		index := strings.Index(term, operator)
		if index < 1 {
			continue
		}
		fieldName := strings.TrimSpace(term[:index])
		value := strings.TrimSpace(term[index+len(operator):])
		if value == "" {
			return filterClause{}, true, fmt.Errorf("filter field %q needs a value", fieldName)
		}
		field, err := resolveFilterField(resourceType, fieldName)
		if err != nil {
			return filterClause{}, true, err
		}
		parsedValue, err := parseFilterValue(field, operator, value)
		if err != nil {
			return filterClause{}, true, err
		}
		return filterClause{field: field, operator: operator, value: parsedValue}, true, nil
	}
	return filterClause{}, false, nil
}

func resolveFilterField(resourceType, name string) (filterField, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if resourceType == "Users" {
		switch normalized {
		case "name", "displayname":
			return filterField{key: "name", kind: filterString}, nil
		case "username", "user_name":
			return filterField{key: "username", kind: filterString}, nil
		case "enabled", "active":
			return filterField{key: "active", kind: filterBool}, nil
		case "status":
			return filterField{key: "active", kind: filterStatus}, nil
		case "id":
			return filterField{key: "id", kind: filterString}, nil
		case "externalid", "external_id":
			return filterField{key: "externalid", kind: filterString}, nil
		case "givenname", "given_name":
			return filterField{key: "givenname", kind: filterString}, nil
		case "familyname", "family_name":
			return filterField{key: "familyname", kind: filterString}, nil
		}
		return filterField{}, fmt.Errorf("unknown user filter field %q; use name, username, status, enabled, active, id, externalId, givenName, or familyName", name)
	}

	switch normalized {
	case "name", "group", "displayname":
		return filterField{key: "name", kind: filterString}, nil
	case "members", "membercount", "member_count":
		return filterField{key: "members", kind: filterInteger}, nil
	case "id":
		return filterField{key: "id", kind: filterString}, nil
	case "externalid", "external_id":
		return filterField{key: "externalid", kind: filterString}, nil
	default:
		return filterField{}, fmt.Errorf("unknown group filter field %q; use name, members, id, or externalId", name)
	}
}

func parseFilterValue(field filterField, operator, value string) (any, error) {
	switch field.kind {
	case filterString:
		if operator != "=" && operator != "!=" && operator != "~" {
			return nil, fmt.Errorf("filter operator %q is not valid for text fields", operator)
		}
		return value, nil
	case filterBool:
		if operator != "=" && operator != "!=" {
			return nil, fmt.Errorf("filter operator %q is not valid for enabled or active", operator)
		}
		parsed, err := strconv.ParseBool(strings.ToLower(value))
		if err != nil {
			return nil, fmt.Errorf("enabled and active filters require true or false")
		}
		return parsed, nil
	case filterStatus:
		if operator != "=" && operator != "!=" {
			return nil, fmt.Errorf("filter operator %q is not valid for status", operator)
		}
		switch strings.ToLower(value) {
		case "enabled", "true":
			return true, nil
		case "disabled", "false":
			return false, nil
		default:
			return nil, fmt.Errorf("status filters require enabled or disabled")
		}
	case filterInteger:
		if operator == "~" {
			return nil, fmt.Errorf("filter operator %q is not valid for numeric fields", operator)
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("members filters require a whole number")
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unsupported filter field")
	}
}

func (filter localFilter) matches(resourceType string, resource scim.Resource) bool {
	searchable := searchText(resource)
	for _, query := range filter.text {
		if !strings.Contains(searchable, query) {
			return false
		}
	}
	for _, clause := range filter.clauses {
		if !clause.matches(resourceType, resource) {
			return false
		}
	}
	return true
}

func (clause filterClause) matches(resourceType string, resource scim.Resource) bool {
	actual := filterFieldValue(resourceType, resource, clause.field.key)
	switch expected := clause.value.(type) {
	case string:
		value, _ := actual.(string)
		switch clause.operator {
		case "=":
			return strings.EqualFold(value, expected)
		case "!=":
			return !strings.EqualFold(value, expected)
		case "~":
			return strings.Contains(strings.ToLower(value), strings.ToLower(expected))
		}
	case bool:
		value, _ := actual.(bool)
		if clause.operator == "!=" {
			return value != expected
		}
		return value == expected
	case int:
		value, _ := actual.(int)
		switch clause.operator {
		case "=":
			return value == expected
		case "!=":
			return value != expected
		case ">":
			return value > expected
		case ">=":
			return value >= expected
		case "<":
			return value < expected
		case "<=":
			return value <= expected
		}
	}
	return false
}

func filterFieldValue(resourceType string, resource scim.Resource, key string) any {
	switch key {
	case "name":
		return resourceLabel(resourceType, resource)
	case "username":
		return rawString(resource["userName"])
	case "active":
		value, _ := resource["active"].(bool)
		return value
	case "members":
		return arrayLength(resource["members"])
	case "id":
		return rawString(resource["id"])
	case "externalid":
		return rawString(resource["externalId"])
	case "givenname":
		return nestedString(resource, "name", "givenName")
	case "familyname":
		return nestedString(resource, "name", "familyName")
	default:
		return nil
	}
}

func sortVisibleResources(resources []scim.Resource, resourceType string, spec sortSpec) {
	if spec.key == "" {
		spec = defaultSort(resourceType)
	}
	sort.SliceStable(resources, func(i, j int) bool {
		comparison := compareSortValues(resourceType, spec.key, resources[i], resources[j])
		if comparison != 0 {
			if spec.descending {
				return comparison > 0
			}
			return comparison < 0
		}
		return strings.ToLower(rawString(resources[i]["id"])) < strings.ToLower(rawString(resources[j]["id"]))
	})
}

func compareSortValues(resourceType, key string, left, right scim.Resource) int {
	if key == "members" {
		return compareInts(arrayLength(left["members"]), arrayLength(right["members"]))
	}
	if key == "status" {
		leftActive, _ := left["active"].(bool)
		rightActive, _ := right["active"].(bool)
		return compareInts(boolInt(leftActive), boolInt(rightActive))
	}
	var leftValue, rightValue string
	switch key {
	case "username":
		leftValue = rawString(left["userName"])
		rightValue = rawString(right["userName"])
	case "id":
		leftValue = rawString(left["id"])
		rightValue = rawString(right["id"])
	default:
		leftValue = resourceLabel(resourceType, left)
		rightValue = resourceLabel(resourceType, right)
	}
	return strings.Compare(strings.ToLower(leftValue), strings.ToLower(rightValue))
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
