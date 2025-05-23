package flags

import (
	"strings"

	"github.com/aquasecurity/tracee/pkg/errfmt"
)

func eventsHelp() string {
	return `The events flag (--events) selects which events to trace and applies event-specific filters.

Event Selection:
1. Individual Events:
   --events execve,open                              | trace specific events
   --events 'open*'                                  | trace events with prefix 'open'

2. Event Sets:
   --events fs                                       | trace all filesystem events
   --events fs --events -open,-openat                | trace fs events except open(at)

Event Filtering:
1. Event Scope (event_name.scope.field):
   Filters based on event metadata
   Examples:
   --events openat.scope.processName=ls              | match events from ls
   --events security_file_open.scope.container       | match events from containers

2. Event Data Fields (event_name.data.arg_name):
   Operators: '=', '!='
   String matching: prefix with '*' or suffix with '*'
   Examples:
   --events close.data.fd=5                          | match specific file descriptor
   --events openat.data.pathname='/tmp*'             | match paths starting with /tmp
   --events openat.data.pathname='*shadow'           | match paths ending with shadow
   --events openat.data.pathname!=/tmp/1,/bin/ls     | exclude specific paths

3. Return Values (event_name.retval):
   Uses numerical comparison operators: '=', '!=', '<', '>'
   Example:
   --events open.retval=0                            | match successful opens

Note: Use quotes to escape special characters: '*', '>', '<'
For full field reference, see trace.Event struct in types/trace package.
`
}

// PolicyEventMap maps policy id to its pre-parsed event flag fields
type PolicyEventMap map[int]policyEvents

// policyEvents holds pre-parsed event flag fields of one policy
type policyEvents struct {
	policyName string
	eventFlags []eventFlag
}

// eventFlag holds pre-parsed event flag fields
type eventFlag struct {
	full              string
	eventFilter       string
	eventName         string
	eventOptionType   string
	eventOptionName   string
	operator          string
	values            string
	operatorAndValues string
	filter            string
}

func PrepareEventMapFromFlags(eventsArr []string) (PolicyEventMap, error) {
	// parse and store events flags
	var evtFlags []eventFlag
	for _, evtFlag := range eventsArr {
		parsed, err := parseEventFlag(evtFlag)
		if err != nil {
			return nil, err
		}

		evtFlags = append(evtFlags, parsed...)
	}

	eventMap := make(PolicyEventMap)
	eventMap[0] = policyEvents{eventFlags: evtFlags}

	return eventMap, nil
}

// parseEventFlag parses an event flag and returns a slice of eventFlag struct
// with pre-parsed fields, or an error if the flag is invalid.
func parseEventFlag(flag string) ([]eventFlag, error) {
	if flag == "" {
		return []eventFlag{}, errfmt.WrapError(InvalidFlagEmpty())
	}

	// get first idx of any expression operator (=, !=, <, >, <=, >=)
	operatorIdx := strings.IndexAny(flag, "=!<>")

	//
	// without expression operator
	//

	if operatorIdx == -1 { // no operator, as a set flag
		if strings.Contains(flag, ".") { // "openat.scope.container" edge case
			evtParts, err := getEventFilterParts(flag, flag)
			if err != nil {
				return []eventFlag{}, errfmt.WrapError(err)
			}

			return []eventFlag{
				{
					full:            flag,
					eventFilter:     flag,
					eventName:       evtParts.name,
					eventOptionType: evtParts.optType,
					eventOptionName: evtParts.optName,
				},
			}, nil
		}

		// parse multiple event names separated by comma
		evtNames := strings.Split(flag, ",")
		evtFlags := make([]eventFlag, 0, len(evtNames))
		for _, evtName := range evtNames {
			if evtName == "" {
				return []eventFlag{}, errfmt.WrapError(InvalidFilterFlagFormat(flag))
			}
			if hasLeadingOrTrailingWhitespace(evtName) {
				return []eventFlag{}, errfmt.WrapError(InvalidFilterFlagFormat(flag))
			}

			// unset flag
			if evtName[0] == '-' {
				name := evtName[1:]
				if hasLeadingOrTrailingWhitespace(name) {
					return []eventFlag{}, errfmt.WrapError(InvalidFilterFlagFormat(flag))
				}

				evtFlags = append(evtFlags, eventFlag{
					full:      evtName,
					eventName: name,
					operator:  "-",
				})

				continue
			}

			// set flag
			evtFlags = append(evtFlags, eventFlag{
				full:      evtName,
				eventName: evtName,
			})
		}

		return evtFlags, nil
	}

	//
	// with expression operator
	//

	// validate event filter
	evtFilter := flag[:operatorIdx]
	evtParts, err := getEventFilterParts(evtFilter, flag)
	if err != nil {
		return []eventFlag{}, errfmt.WrapError(err)
	}
	filter := flag[len(evtParts.name)+1:]

	// validate operator and values
	opAndValParts, err := getOperatorAndValuesParts(flag, operatorIdx)
	if err != nil {
		return []eventFlag{}, errfmt.WrapError(err)
	}

	return []eventFlag{
		{
			full:              flag,                            // "openat.data.pathname=/etc/*"
			eventFilter:       evtFilter,                       // "openat.data.pathname"
			eventName:         evtParts.name,                   // "openat"
			eventOptionType:   evtParts.optType,                // "data"
			eventOptionName:   evtParts.optName,                // "pathname"
			operator:          opAndValParts.operator,          // "="
			values:            opAndValParts.values,            // "/etc/*"
			operatorAndValues: opAndValParts.operatorAndValues, // "=/etc/*"
			filter:            filter,                          // "data.pathname=/etc/*"
		},
	}, nil
}

type filterOptParts struct {
	name    string
	optType string
	optName string
}

// getEventFilterParts splits an event filter into its parts, validating them.
// Valid formats: "xxxx.xxxx", "xxxx.xxxx.xxxx".
func getEventFilterParts(filter, flag string) (filterOptParts, error) {
	filterParts := strings.Split(filter, ".")

	if filterParts[0] == "" || len(filterParts) < 2 || filterParts[1] == "" || len(filterParts) > 3 {
		return filterOptParts{}, InvalidFilterFlagFormat(flag)
	}
	name := filterParts[0]
	optType := filterParts[1]
	optName := ""
	if len(filterParts) == 3 {
		optName = filterParts[2]
		if optName == "" {
			return filterOptParts{}, InvalidFilterFlagFormat(flag)
		}
	}

	if hasLeadingOrTrailingWhitespace(name) ||
		hasLeadingOrTrailingWhitespace(optType) ||
		hasLeadingOrTrailingWhitespace(optName) {
		return filterOptParts{}, InvalidFilterFlagFormat(flag)
	}

	return filterOptParts{
		name:    name,
		optType: optType,
		optName: optName,
	}, nil
}
