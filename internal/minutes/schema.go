package minutes

// Registers in the order the specialist passes run: interdependencies last,
// because it links to the actions and risks found before it.
var Registers = []string{"actions", "decisions", "risks", "changes", "interdependencies"}

// Schema is the structured-output JSON schema for all of Records. Strict
// mode needs every object closed (additionalProperties: false) and every
// field required; "unknown" is an empty string or empty list.
func Schema() map[string]any { return closed(registerSchemas()) }

// SchemaFor is the schema of a single-register pass.
func SchemaFor(register string) map[string]any {
	return closed(map[string]any{register: registerSchemas()[register]})
}

func closed(props map[string]any) map[string]any {
	req := make([]string, 0, len(props))
	for k := range props {
		req = append(req, k)
	}
	return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
}

func registerSchemas() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	enum := func(desc string, vals []string) map[string]any {
		return map[string]any{"type": "string", "enum": append([]string{""}, vals...), "description": desc}
	}
	strs := func(desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
	}
	obj := closed
	list := func(item map[string]any) map[string]any { return map[string]any{"type": "array", "items": item} }

	evidence := list(obj(map[string]any{
		"at":      str("HH:MM:SS of the transcript line the quote comes from"),
		"speaker": str("speaker name exactly as in that transcript line"),
		"quote":   str("verbatim words copied from that line, at most 200 characters"),
	}))
	evidence["description"] = "1-3 transcript quotes proving this record; never paraphrase"

	return map[string]any{
		"actions": list(obj(map[string]any{
			"key":         str("unique id: a1, a2, …"),
			"title":       str("short title, Romanian"),
			"description": str("what must be done, Romanian"),
			"deadline":    str("YYYY-MM-DD when a date can be resolved from the meeting date, else empty"),
			"responsible": str("person responsible, as named in the meeting, else empty"),
			"status":      enum("status", ActionStatuses),
			"notes":       str("extra context, e.g. an unresolved deadline phrase; Romanian; may be empty"),
			"evidence":    evidence,
		})),
		"decisions": list(obj(map[string]any{
			"key":         str("unique id: d1, d2, …"),
			"statement":   str("the decision, Romanian"),
			"rationale":   str("why, if stated; Romanian; may be empty"),
			"made_by":     str("who decided, if stated"),
			"agreed_with": str("who consented, if stated"),
			"status":      enum("status", DecisionStatuses),
			"outcomes":    str("expected outcome, if stated; Romanian; may be empty"),
			"evidence":    evidence,
		})),
		"risks": list(obj(map[string]any{
			"key":               str("unique id: r1, r2, …"),
			"title_description": str("risk title and brief description, Romanian"),
			"cause":             str("cause, if stated"),
			"severity":          enum("severity, only if it can be judged from what was said", Levels),
			"probability":       enum("probability, only if it can be judged from what was said", Levels),
			"decision":          str("decision taken about the risk, if any"),
			"response_actions":  str("mitigation actions, if any"),
			"owner":             str("owner for implementation, if stated"),
			"status_update":     str("current status as stated, may be empty"),
			"evidence":          evidence,
		})),
		"interdependencies": list(obj(map[string]any{
			"key":                  str("unique id: i1, i2, …"),
			"dependent_on_project": str("the other project/team this depends on"),
			"dependency":           str("what the dependency is, Romanian"),
			"status":               enum("status", InterdepStatuses),
			"action_keys":          strs("keys of related actions (a…)"),
			"risk_keys":            strs("keys of related risks (r…)"),
			"notes":                str("may be empty"),
			"evidence":             evidence,
		})),
		"changes": list(obj(map[string]any{
			"key":                str("unique id: c1, c2, …"),
			"title":              str("short title, Romanian"),
			"summary":            str("summary of the requested change, Romanian"),
			"requested_by":       str("who requested it"),
			"requestor_priority": enum("priority the requester gave", Levels),
			"change_type":        enum("size of the change", ChangeTypes),
			"cost_impact":        enum("cost impact", Levels),
			"schedule_impact":    enum("schedule impact", Levels),
			"quality_impact":     enum("quality impact", Levels),
			"overall_impact":     enum("overall impact", Levels),
			"priority":           enum("priority", Levels),
			"status":             enum("status", ChangeStatuses),
			"notes":              str("may be empty"),
			"evidence":           evidence,
		})),
	}
}
