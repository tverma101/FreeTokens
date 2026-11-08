package usage

import "testing"

func TestProviderOfReadsTheModelNotTheSource(t *testing.T) {
	cases := map[string]string{
		// Names measured on a real machine, including Claude Code pointed at other providers.
		"Agy CLI · Gemini 3.8 Flash (High)":          "Google",
		"Antigravity · Claude Opus 4.6 (Thinking)":   "Anthropic",
		"Claude · Qwen3.8-2B-Uncensored-GGUF:Q4_K_M": "Alibaba",
		"Claude · deepseek-v4.1-flash-1017":          "DeepSeek",
		"Claude · nemotron-3.5-lightning:free":       "NVIDIA",
		"Codex · Codex Auto Review":                  "OpenAI",
		"Codex · GPT 6 Luna":                         "OpenAI",
		"DeepSeek V4.1 Flash 10/17":                  "DeepSeek",
		"MiMo v2.6 Flash Fast":                       "Xiaomi",
		"Nemotron 3.5 Lightning":                     "NVIDIA",
		"claude-sonnet-4-5-20250929":                 "Anthropic",
		"gpt-5-codex":                                "OpenAI",
		"llama-3.3-70b":                              "Meta",
		"Claude · bridge-model-1":                    ProviderOther,
		"Claude · mock-model":                        ProviderOther,
		"Claude · m":                                 ProviderOther,
		"":                                           ProviderOther,
	}
	for model, want := range cases {
		if got := ProviderOf(model); got != want {
			t.Errorf("ProviderOf(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestProviderHintAcceptsVendorsAndRejectsGateways(t *testing.T) {
	cases := map[string]string{
		// Provider ids a tool records per message, which name the model's vendor.
		"anthropic":    "Anthropic",
		"openai":       "OpenAI",
		"google":       "Google",
		"deepseek":     "DeepSeek",
		"xai":          "xAI",
		"mistral":      "Mistral",
		"moonshot":     "Moonshot",
		"alibaba":      "Alibaba",
		"cursor":       "Cursor",
		"nousresearch": "Nous Research",
		// Gateways and hosts say who proxied or ran the request, so they leave the guess to the
		// model name instead of relabelling it.
		"openrouter":     "",
		"opencode":       "",
		"vercel":         "",
		"amazon-bedrock": "",
		"google-vertex":  "",
		"azure":          "",
		"groq":           "",
		"ollama":         "",
		"":               "",
	}
	for id, want := range cases {
		if got := ProviderHint(id); got != want {
			t.Errorf("ProviderHint(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestAccumulatorPrefersAMeasuredProvider(t *testing.T) {
	var a Accumulator
	// The model name says Anthropic, but the source measured Mistral as the vendor.
	a.AddServed("2026-11-02", 30, nil, "OpenCode · Claude Sonnet 4.5", "", "Mistral")
	a.Add("2026-11-02", 10, nil, "OpenCode · Claude Sonnet 4.5", "")
	period := Summarize("Today", a.Build(), func(string) bool { return true })
	if len(period.Models) != 1 || period.Models[0].Provider != "Mistral" {
		t.Fatalf("models = %+v, want one Mistral row", period.Models)
	}
}

func TestAccumulatorOwnsOnlyUnattributedModels(t *testing.T) {
	var a Accumulator
	a.AddOwned("2026-11-02", 10, nil, "OpenCode · Big Pickle", "", "OpenCode")
	a.AddOwned("2026-11-02", 20, nil, "OpenCode · GPT 6 Luna", "", "OpenCode")
	period := Summarize("Today", a.Build(), func(string) bool { return true })

	byName := map[string]string{}
	for _, row := range period.Models {
		byName[row.Name] = row.Provider
	}
	// Nothing names Big Pickle, so the source's own vendor stands in.
	if got := byName["OpenCode · Big Pickle"]; got != "OpenCode" {
		t.Errorf("owned model provider = %q, want OpenCode", got)
	}
	// A keyword match is not overridden by the source claiming the model.
	if got := byName["OpenCode · GPT 6 Luna"]; got != "OpenAI" {
		t.Errorf("keyword model provider = %q, want OpenAI", got)
	}
}

func TestOwnedVendorYieldsToTheCatalog(t *testing.T) {
	a := Accumulator{Vendor: func(model string) string {
		if model == "orion-4-405b" {
			return "nousresearch"
		}
		return ""
	}}
	a.AddOwned("2026-11-02", 10, nil, "orion-4-405b", "", "OpenCode")
	period := Summarize("Today", a.Build(), func(string) bool { return true })
	if len(period.Models) != 1 || period.Models[0].Provider != "Nous Research" {
		t.Fatalf("models = %+v, want Nous Research", period.Models)
	}
}

func TestProviderFromPrefixNamesTheCatalogVendor(t *testing.T) {
	cases := map[string]string{
		"openai":           "OpenAI",
		"x-ai":             "xAI",
		"meta-llama":       "Meta",
		"mistralai":        "Mistral",
		"01-ai":            "01.AI",
		"nousresearch":     "Nous Research",
		"bytedance-seed":   "ByteDance",
		"liquid":           "Liquid AI",
		"thinkingmachines": "Thinking Machines",
		"inclusionai":      "InclusionAI",
		"some-lab":         "Some Lab",
	}
	for prefix, want := range cases {
		if got := providerFromPrefix(prefix); got != want {
			t.Errorf("providerFromPrefix(%q) = %q, want %q", prefix, got, want)
		}
	}
}

func TestProviderOfPrefersKeywordsOverTheCatalog(t *testing.T) {
	vendor := func(string) string { return "nousresearch" }
	if got := providerOf("deepseek-chat", vendor); got != "DeepSeek" {
		t.Errorf("keyword match = %q, want DeepSeek", got)
	}
	if got := providerOf("orion-4-405b", vendor); got != "Nous Research" {
		t.Errorf("catalog fallback = %q, want Nous Research", got)
	}
	if got := providerOf("orion-4-405b", nil); got != ProviderOther {
		t.Errorf("nil catalog = %q, want %q", got, ProviderOther)
	}
}

func TestModelRowsCarryProviderAndSource(t *testing.T) {
	var a Accumulator
	a.Add("2026-11-02", 10, nil, "Claude · deepseek-v4.1-flash", "")
	a.Add("2026-11-02", 20, nil, "DeepSeek V4.1 Flash", "")
	days := a.Build()
	period := Summarize("Today", days, func(string) bool { return true })

	byName := map[string]ModelRow{}
	for _, row := range period.Models {
		byName[row.Name] = row
	}
	session := byName["Claude · deepseek-v4.1-flash"]
	if session.Provider != "DeepSeek" || session.Source != "Claude" {
		t.Errorf("Claude session row = %+v, want provider DeepSeek and source Claude", session)
	}
	hosted := byName["DeepSeek V4.1 Flash"]
	if hosted.Provider != "DeepSeek" || hosted.Source != SourceFreebuff {
		t.Errorf("hosted row = %+v, want provider DeepSeek and source Freebuff", hosted)
	}
}

func TestAccumulatorGroupsUnmatchedModelsUnderTheCatalogVendor(t *testing.T) {
	a := Accumulator{Vendor: func(model string) string {
		if model == "orion-4-405b" {
			return "nousresearch"
		}
		return ""
	}}
	a.Add("2026-11-02", 10, nil, "orion-4-405b", "")
	a.Add("2026-11-02", 20, nil, "deepseek-chat", "")
	a.Add("2026-11-02", 30, nil, "mystery-model", "")
	period := Summarize("Today", a.Build(), func(string) bool { return true })

	byName := map[string]ModelRow{}
	for _, row := range period.Models {
		byName[row.Name] = row
	}
	if got := byName["orion-4-405b"].Provider; got != "Nous Research" {
		t.Errorf("unmatched model provider = %q, want Nous Research", got)
	}
	if got := byName["deepseek-chat"].Provider; got != "DeepSeek" {
		t.Errorf("keyword model provider = %q, want DeepSeek", got)
	}
	if got := byName["mystery-model"].Provider; got != ProviderOther {
		t.Errorf("unpriced model provider = %q, want %q", got, ProviderOther)
	}
}

// One model id per vendor the keyword table recognizes, so a new rule is never added without a
// name that exercises it.
func TestProviderOfNamesEveryVendor(t *testing.T) {
	cases := map[string]string{
		"claude-haiku-4-5-20251001": "Anthropic",
		"o3-pro":                    "OpenAI",
		"gemma-3-27b-it":            "Google",
		"deepseek-coder-v2":         "DeepSeek",
		"qwen3-coder-480b":          "Alibaba",
		"nemotron-4-340b-instruct":  "NVIDIA",
		"llama-4-scout-17b":         "Meta",
		"devstral-small":            "Mistral",
		"grok-4-fast":               "xAI",
		"kimi-k2-thinking":          "Moonshot",
		"chatglm3-6b":               "Z.ai",
		"MiMo v2.6 Flash Fast":      "Xiaomi",
		"minimax-m2":                "MiniMax",
		"titan-text-premier":        "Amazon",
		"phi-4-mini":                "Microsoft",
		"command-r-plus":            "Cohere",
		"sonar-pro":                 "Perplexity",
		"jamba-1.5-large":           "AI21",
		"granite-3.3-8b-instruct":   "IBM",
		"dbrx-instruct":             "Databricks",
		"falcon-180b-chat":          "TII",
		"solar-pro-2":               "Upstage",
		"olmo-2-13b":                "Allen Institute",
		"snowflake-arctic-instruct": "Snowflake",
		"lfm-2.5-1.2b":              "Liquid AI",
		"yi-34b-chat":               "01.AI",
		"internlm3-8b":              "Shanghai AI Lab",
		"baichuan2-13b":             "Baichuan",
		"step-3":                    "StepFun",
		"doubao-1.5-pro":            "ByteDance",
		"hunyuan-turbo":             "Tencent",
		"ernie-4.5":                 "Baidu",
		"reka-core":                 "Reka",
		"hermes-4-405b":             "Nous Research",
		"cursor-small":              "Cursor",
		"composer-1":                "Cursor",
		"bytedance-seed":            "ByteDance",
		"liquid-lfm-2":              "Liquid AI",
		"thinkingmachines-inkling":  "Thinking Machines",
		"inclusionai-ling-1t":       "InclusionAI",
	}
	for model, want := range cases {
		if got := ProviderOf(model); got != want {
			t.Errorf("ProviderOf(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestProviderOfIgnoresShortKeywordsInsideWords(t *testing.T) {
	// "o1" and "phi" only match whole tokens, so these stay Other.
	for _, model := range []string{"to1-ultra", "gphi-2"} {
		if got := ProviderOf(model); got != ProviderOther {
			t.Errorf("ProviderOf(%q) = %q, want %q", model, got, ProviderOther)
		}
	}
	if got := ProviderOf("grok-3"); got != "xAI" {
		t.Errorf("ProviderOf(grok-3) = %q, want xAI", got)
	}
}
