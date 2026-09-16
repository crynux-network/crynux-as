package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

type rawToolCallParser func(string) []parsedLlmToolCallMatch

var rawToolCallParsers = []rawToolCallParser{
	parseKimiK3ToolCalls,
	parseKimiToolCalls,
	parseDSMLToolCalls,
	parseDeepSeekTokenToolCalls,
	parseHYV4ToolCalls,
	parseGLMToolCalls,
	parseTaggedJSONToolCalls,
	parseLlamaJSONToolCalls,
}

var (
	kimiCallRegex       = regexp.MustCompile(`(?s)<\|tool_call_begin\|>(.*?)<\|tool_call_argument_begin\|>(.*?)<\|tool_call_end\|>`)
	dsmlCallRegex       = regexp.MustCompile(`(?s)<｜DSML｜invoke name="([^"]+)">\s*(.*?)</｜DSML｜invoke>`)
	dsmlParamRegex      = regexp.MustCompile(`(?s)<｜DSML｜parameter name="([^"]+)" string="(true|false)">(.*?)</｜DSML｜parameter>`)
	deepSeekCallRegex   = regexp.MustCompile(`(?s)<｜tool▁call▁begin｜>(.*?)<｜tool▁sep｜>(.*?)<｜tool▁call▁end｜>`)
	hyCallsRegex        = regexp.MustCompile(`(?s)<tool_calls(?::[^>]+)?>.*?</tool_calls(?::[^>]+)?>`)
	hyCallRegex         = regexp.MustCompile(`(?s)<tool_call(?::[^>]+)?>(.*?)</tool_call(?::[^>]+)?>`)
	hyArgStartRegex     = regexp.MustCompile(`<arg_key(?::[^>]+)?>`)
	hyArgRegex          = regexp.MustCompile(`(?s)<arg_key(?::[^>]+)?>(.*?)</arg_key(?::[^>]+)?><arg_value(?::[^>]+)?>(.*?)</arg_value(?::[^>]+)?>`)
	glmCallRegex        = regexp.MustCompile(`(?s)<tool_call>\s*([^<\s{]+)(.*?)</tool_call>`)
	glmArgRegex         = regexp.MustCompile(`(?s)<arg_key>(.*?)</arg_key>\s*<arg_value>(.*?)</arg_value>`)
	k3CallsSectionRegex = regexp.MustCompile(`(?s)<\|open\|>\s*tools\s*<\|sep\|>.*?<\|close\|>\s*tools\s*<\|sep\|>`)
	k3CallRegex         = regexp.MustCompile(`(?s)<\|open\|>\s*call\s+(.*?)<\|sep\|>(.*?)<\|close\|>\s*call\s*<\|sep\|>`)
	k3ArgumentRegex     = regexp.MustCompile(`(?s)<\|open\|>\s*argument\s+(.*?)<\|sep\|>(.*?)<\|close\|>\s*argument\s*<\|sep\|>`)
	k3AttributeRegex    = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

func NormalizeAssistantContentForTask(content string, args *models.GPTTaskArgs) (string, []parsedLlmToolCall) {
	if args == nil {
		return NormalizeAssistantContent(content)
	}
	if choice, ok := args.ToolChoice.(string); ok && choice == "none" {
		return content, nil
	}
	if len(args.Tools) == 0 {
		return content, nil
	}
	if args.ResponseFormat != nil && args.ToolChoice == "auto" && !hasStrictTool(args.Tools) {
		return content, nil
	}

	var matches []parsedLlmToolCallMatch
	for _, parser := range rawToolCallParsers {
		matches = parser(content)
		if len(matches) > 0 {
			break
		}
	}
	matches = filterDeclaredToolCalls(matches, args.Tools)
	if len(matches) == 0 {
		if name := namedToolChoiceName(args.ToolChoice); name != "" {
			if arguments, ok := decodeJSONObject(strings.TrimSpace(content)); ok {
				return "", []parsedLlmToolCall{{Name: name, Arguments: arguments}}
			}
		}
		return content, nil
	}
	clean, calls := removeToolCallMatches(content, matches)
	if strings.Contains(content, "<|open|>tools<|sep|>") {
		clean = unwrapKimiK3Response(clean)
	}
	return clean, calls
}

func hasStrictTool(tools []map[string]interface{}) bool {
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]interface{})
		if strict, _ := function["strict"].(bool); strict {
			return true
		}
	}
	return false
}

func removeToolCallMatches(content string, matches []parsedLlmToolCallMatch) (string, []parsedLlmToolCall) {
	var clean strings.Builder
	calls := make([]parsedLlmToolCall, 0, len(matches))
	start := 0
	for _, match := range matches {
		if match.Start < start || match.End > len(content) {
			continue
		}
		clean.WriteString(content[start:match.Start])
		calls = append(calls, match.ToolCall)
		start = match.End
	}
	clean.WriteString(content[start:])
	return clean.String(), calls
}

func filterDeclaredToolCalls(matches []parsedLlmToolCallMatch, tools []map[string]interface{}) []parsedLlmToolCallMatch {
	allowed := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	filtered := matches[:0]
	for _, match := range matches {
		if _, ok := allowed[match.ToolCall.Name]; ok {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func namedToolChoiceName(choice any) string {
	value, ok := choice.(map[string]interface{})
	if !ok {
		return ""
	}
	function, _ := value["function"].(map[string]interface{})
	name, _ := function["name"].(string)
	return name
}

func parseTaggedJSONToolCalls(content string) []parsedLlmToolCallMatch {
	return parseToolCalls(content)
}

func parseKimiToolCalls(content string) []parsedLlmToolCallMatch {
	sectionStart := strings.Index(content, "<|tool_calls_section_begin|>")
	if sectionStart < 0 {
		return nil
	}
	sectionEnd := strings.Index(content[sectionStart:], "<|tool_calls_section_end|>")
	if sectionEnd < 0 {
		return nil
	}
	sectionEnd += sectionStart + len("<|tool_calls_section_end|>")
	calls := make([]parsedLlmToolCall, 0)
	for _, match := range kimiCallRegex.FindAllStringSubmatch(content[sectionStart:sectionEnd], -1) {
		header := strings.TrimSpace(match[1])
		name := strings.TrimPrefix(strings.SplitN(header, ":", 2)[0], "functions.")
		if arguments, ok := decodeJSONObject(strings.TrimSpace(match[2])); ok && name != "" {
			calls = append(calls, parsedLlmToolCall{Name: name, Arguments: arguments})
		}
	}
	return sectionMatch(calls, sectionStart, sectionEnd)
}

func parseDSMLToolCalls(content string) []parsedLlmToolCallMatch {
	start, end := findWrapper(content,
		[]string{"<｜DSML｜function_calls>", "<｜DSML｜tool_calls>"},
		[]string{"</｜DSML｜function_calls>", "</｜DSML｜tool_calls>"},
	)
	if start < 0 {
		return nil
	}
	calls := make([]parsedLlmToolCall, 0)
	for _, match := range dsmlCallRegex.FindAllStringSubmatch(content[start:end], -1) {
		args := map[string]interface{}{}
		for _, parameter := range dsmlParamRegex.FindAllStringSubmatch(match[2], -1) {
			value := interface{}(parameter[3])
			if parameter[2] == "false" {
				var decoded interface{}
				if json.Unmarshal([]byte(parameter[3]), &decoded) == nil {
					value = decoded
				}
			}
			args[parameter[1]] = value
		}
		calls = append(calls, parsedLlmToolCall{Name: match[1], Arguments: marshalArguments(args)})
	}
	return sectionMatch(calls, start, end)
}

func parseDeepSeekTokenToolCalls(content string) []parsedLlmToolCallMatch {
	start, end := findWrapper(content,
		[]string{"<｜tool▁calls▁begin｜>"},
		[]string{"<｜tool▁calls▁end｜>"},
	)
	if start < 0 {
		return nil
	}
	calls := make([]parsedLlmToolCall, 0)
	for _, match := range deepSeekCallRegex.FindAllStringSubmatch(content[start:end], -1) {
		name := strings.TrimSpace(match[1])
		arguments := strings.TrimSpace(match[2])
		if name == "function" {
			lineEnd := strings.IndexByte(arguments, '\n')
			if lineEnd < 0 {
				continue
			}
			name = strings.TrimSpace(arguments[:lineEnd])
			arguments = strings.TrimSpace(arguments[lineEnd+1:])
		}
		arguments = strings.TrimPrefix(arguments, "```json")
		arguments = strings.TrimSuffix(strings.TrimSpace(arguments), "```")
		if decoded, ok := decodeJSONObject(strings.TrimSpace(arguments)); ok && name != "" {
			calls = append(calls, parsedLlmToolCall{Name: name, Arguments: decoded})
		}
	}
	return sectionMatch(calls, start, end)
}

func parseHYV4ToolCalls(content string) []parsedLlmToolCallMatch {
	loc := hyCallsRegex.FindStringIndex(content)
	if loc == nil {
		return nil
	}
	section := content[loc[0]:loc[1]]
	calls := make([]parsedLlmToolCall, 0)
	for _, match := range hyCallRegex.FindAllStringSubmatch(section, -1) {
		body := match[1]
		name := strings.TrimSpace(body)
		if keyStart := hyArgStartRegex.FindStringIndex(body); keyStart != nil {
			name = strings.TrimSpace(body[:keyStart[0]])
		}
		args := map[string]interface{}{}
		for _, argument := range hyArgRegex.FindAllStringSubmatch(body, -1) {
			args[strings.TrimSpace(argument[1])] = parseQwenParameterValue(argument[2])
		}
		if name != "" {
			calls = append(calls, parsedLlmToolCall{Name: name, Arguments: marshalArguments(args)})
		}
	}
	return sectionMatch(calls, loc[0], loc[1])
}

func parseGLMToolCalls(content string) []parsedLlmToolCallMatch {
	matches := glmCallRegex.FindAllStringSubmatchIndex(content, -1)
	calls := make([]parsedLlmToolCallMatch, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(content[match[2]:match[3]])
		body := content[match[4]:match[5]]
		args := map[string]interface{}{}
		for _, argument := range glmArgRegex.FindAllStringSubmatch(body, -1) {
			args[strings.TrimSpace(argument[1])] = argument[2]
		}
		if name != "" {
			calls = append(calls, parsedLlmToolCallMatch{
				ToolCall: parsedLlmToolCall{Name: name, Arguments: marshalArguments(args)},
				Start:    match[0],
				End:      match[1],
			})
		}
	}
	return calls
}

func parseKimiK3ToolCalls(content string) []parsedLlmToolCallMatch {
	loc := k3CallsSectionRegex.FindStringIndex(content)
	if loc == nil {
		return nil
	}
	calls := make([]parsedLlmToolCall, 0)
	for _, match := range k3CallRegex.FindAllStringSubmatch(content[loc[0]:loc[1]], -1) {
		attributes := parseK3Attributes(match[1])
		name := attributes["tool"]
		args := map[string]interface{}{}
		for _, argument := range k3ArgumentRegex.FindAllStringSubmatch(match[2], -1) {
			argAttributes := parseK3Attributes(argument[1])
			value := interface{}(argument[2])
			if argAttributes["type"] != "string" {
				var decoded interface{}
				if json.Unmarshal([]byte(argument[2]), &decoded) == nil {
					value = decoded
				}
			}
			args[argAttributes["key"]] = value
		}
		if name != "" {
			calls = append(calls, parsedLlmToolCall{Name: name, Arguments: marshalArguments(args)})
		}
	}
	return sectionMatch(calls, loc[0], loc[1])
}

func parseK3Attributes(raw string) map[string]string {
	attributes := map[string]string{}
	for _, match := range k3AttributeRegex.FindAllStringSubmatch(raw, -1) {
		attributes[match[1]] = html.UnescapeString(match[2])
	}
	return attributes
}

func unwrapKimiK3Response(content string) string {
	const (
		responseOpen  = "<|open|>response<|sep|>"
		responseClose = "<|close|>response<|sep|>"
		messageClose  = "<|close|>message<|sep|>"
	)
	if start := strings.Index(content, responseOpen); start >= 0 {
		content = content[:start] + content[start+len(responseOpen):]
	}
	content = strings.ReplaceAll(content, responseClose, "")
	content = strings.ReplaceAll(content, messageClose, "")
	return content
}

func parseLlamaJSONToolCalls(content string) []parsedLlmToolCallMatch {
	objects := findJSONObjects(content)
	calls := make([]parsedLlmToolCallMatch, 0)
	for _, object := range objects {
		var value struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal([]byte(content[object[0]:object[1]]), &value) != nil || value.Name == "" || len(value.Parameters) == 0 {
			continue
		}
		if arguments, ok := decodeJSONObject(string(value.Parameters)); ok {
			calls = append(calls, parsedLlmToolCallMatch{
				ToolCall: parsedLlmToolCall{Name: value.Name, Arguments: arguments},
				Start:    object[0],
				End:      object[1],
			})
		}
	}
	return calls
}

func findJSONObjects(content string) [][2]int {
	var objects [][2]int
	depth, start := 0, -1
	inString, escaped := false, false
	for index, char := range content {
		if inString {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
		} else if char == '{' {
			if depth == 0 {
				start = index
			}
			depth++
		} else if char == '}' && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, [2]int{start, index + 1})
				start = -1
			}
		}
	}
	return objects
}

func findWrapper(content string, starts, ends []string) (int, int) {
	for i, startTag := range starts {
		start := strings.Index(content, startTag)
		if start < 0 {
			continue
		}
		end := strings.Index(content[start+len(startTag):], ends[i])
		if end >= 0 {
			return start, start + len(startTag) + end + len(ends[i])
		}
	}
	return -1, -1
}

func sectionMatch(calls []parsedLlmToolCall, start, end int) []parsedLlmToolCallMatch {
	if len(calls) == 0 {
		return nil
	}
	matches := make([]parsedLlmToolCallMatch, len(calls))
	for i, call := range calls {
		matches[i] = parsedLlmToolCallMatch{ToolCall: call, Start: start, End: end}
		if i > 0 {
			matches[i].Start = end
		}
	}
	return matches
}

func decodeJSONObject(raw string) (string, bool) {
	var value map[string]interface{}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return "", false
	}
	return marshalArguments(value), true
}

func marshalArguments(value map[string]interface{}) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
