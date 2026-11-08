package usage

import (
	"strings"
	"unicode"
)

// providerRules map a model id or display name to the company that serves it. Short keywords
// ("gpt", "o3", "phi") are matched whole against the name's hyphen-separated tokens so they cannot
// fire inside unrelated words; longer ones match anywhere. Order matters: whichever rule comes
// first names the provider.
var providerRules = []struct {
	keyword  string
	provider string
}{
	{"claude", "Anthropic"},
	{"sonnet", "Anthropic"},
	{"opus", "Anthropic"},
	{"haiku", "Anthropic"},
	{"gpt", "OpenAI"},
	{"codex", "OpenAI"},
	{"davinci", "OpenAI"},
	{"o1", "OpenAI"},
	{"o3", "OpenAI"},
	{"o4", "OpenAI"},
	{"gemini", "Google"},
	{"gemma", "Google"},
	{"deepseek", "DeepSeek"},
	{"qwen", "Alibaba"},
	{"qwq", "Alibaba"},
	{"nemotron", "NVIDIA"},
	{"llama", "Meta"},
	{"mistral", "Mistral"},
	{"mixtral", "Mistral"},
	{"codestral", "Mistral"},
	{"devstral", "Mistral"},
	{"magistral", "Mistral"},
	{"grok", "xAI"},
	{"kimi", "Moonshot"},
	{"moonshot", "Moonshot"},
	{"glm", "Z.ai"},
	{"chatglm", "Z.ai"},
	{"mimo", "Xiaomi"},
	{"minimax", "MiniMax"},
	{"abab", "MiniMax"},
	{"nova", "Amazon"},
	{"titan", "Amazon"},
	{"phi", "Microsoft"},
	{"command", "Cohere"},
	{"aya", "Cohere"},
	{"sonar", "Perplexity"},
	{"jamba", "AI21"},
	{"granite", "IBM"},
	{"dbrx", "Databricks"},
	{"falcon", "TII"},
	{"solar", "Upstage"},
	{"olmo", "Allen Institute"},
	{"arctic", "Snowflake"},
	{"lfm", "Liquid AI"},
	{"liquid", "Liquid AI"},
	{"yi", "01.AI"},
	{"internlm", "Shanghai AI Lab"},
	{"baichuan", "Baichuan"},
	{"step-", "StepFun"},
	{"doubao", "ByteDance"},
	{"bytedance", "ByteDance"},
	{"hunyuan", "Tencent"},
	{"ernie", "Baidu"},
	{"reka", "Reka"},
	{"hermes", "Nous Research"},
	{"cursor", "Cursor"},
	{"composer", "Cursor"},
	{"thinkingmachines", "Thinking Machines"},
	{"inclusionai", "InclusionAI"},
}

// ProviderOther is the grouping for model names no rule recognizes.
const ProviderOther = "Other"

// ProviderHint turns a provider id a source recorded per message, such as OpenCode's providerID,
// into the vendor to show. Only ids naming a vendor the keyword table knows are accepted: a gateway
// id such as "openrouter" or "opencode" says who proxied the request rather than who made the
// model, and a host such as "groq" serves other vendors' models, so both return "" and the model
// name keeps deciding.
func ProviderHint(id string) string {
	return vendorNames[vendorSlug(id)]
}

// providerFor prefers a vendor a source measured, then the keyword table and the pricing catalog,
// and only then the vendor a source claims for models nothing else names.
func providerFor(measured, owned, model string, vendor func(string) string) string {
	if measured != "" {
		return measured
	}
	if name := providerOf(model, vendor); name != ProviderOther {
		return name
	}
	if owned != "" {
		return owned
	}
	return ProviderOther
}

// providerOf prefers a keyword rule and falls back to the pricing catalog's vendor prefix for
// models the table has not learned yet, so a priced route still groups under its vendor.
func providerOf(model string, vendor func(string) string) string {
	name := ProviderOf(model)
	if name != ProviderOther || vendor == nil {
		return name
	}
	prefix := vendor(model)
	if prefix == "" {
		return ProviderOther
	}
	if fromPrefix := providerFromPrefix(prefix); fromPrefix != "" {
		return fromPrefix
	}
	return ProviderOther
}

// providerFromPrefix turns a catalog vendor slug into a display name. A slug spelling a known
// vendor keeps that spelling ("openai", "x-ai"); one carrying a known family uses its vendor
// ("meta-llama" is Meta); anything else is title-cased ("nousresearch" becomes Nousresearch).
func providerFromPrefix(prefix string) string {
	if name, ok := vendorNames[vendorSlug(prefix)]; ok {
		return name
	}
	if name := ProviderOf(prefix); name != ProviderOther {
		return name
	}
	words := strings.FieldsFunc(prefix, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		words[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// vendorNames indexes the display spelling of every vendor the keyword table knows, so a catalog
// slug like "x-ai" keeps the xAI casing rather than reading as two words.
var vendorNames = func() map[string]string {
	names := map[string]string{}
	for _, rule := range providerRules {
		names[vendorSlug(rule.provider)] = rule.provider
	}
	return names
}()

// vendorSlug folds separators away so "x-ai" and "xAI" compare equal.
func vendorSlug(v string) string { return strings.ReplaceAll(normalizeModel(v), "-", "") }

// ProviderOf names the company behind a model row. Claude Code and Codex can be pointed at other
// providers' models, so the provider is read from the model part of the row, never from its source
// prefix: "Claude · deepseek-v4.1-flash" is DeepSeek usage.
func ProviderOf(model string) string {
	if i := strings.Index(model, " · "); i >= 0 {
		model = model[i+len(" · "):]
	}
	norm := normalizeModel(model)
	if norm == "" {
		return ProviderOther
	}
	tokens := strings.Split(norm, "-")
	for _, rule := range providerRules {
		if len(rule.keyword) <= 3 {
			for _, token := range tokens {
				if token == rule.keyword {
					return rule.provider
				}
			}
			continue
		}
		if strings.Contains(norm, rule.keyword) {
			return rule.provider
		}
	}
	return ProviderOther
}

// normalizeModel lowercases a model name and collapses separators to single hyphens.
func normalizeModel(model string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(model) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}
