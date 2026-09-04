package config

// Preset is a one-shot template for connecting a model backend.
type Preset struct {
	ID      string
	Title   string
	BaseURL string
	KeyEnv  string // suggested env var; "" means no key needed
	Type    string // "openai" (default) or "anthropic"
}

// Presets lists the backends the /connect wizard can attach.
var Presets = []Preset{
	{ID: "openrouter", Title: "OpenRouter — many models, one key (has free models)", BaseURL: "https://openrouter.ai/api/v1", KeyEnv: "OPENROUTER_API_KEY"},
	{ID: "anthropic", Title: "Anthropic — Claude 3.7 Sonnet, 3.5 Sonnet & Haiku (Native /v1/messages)", BaseURL: "https://api.anthropic.com/v1", KeyEnv: "ANTHROPIC_API_KEY", Type: "anthropic"},
	{ID: "deepseek", Title: "DeepSeek — direct API (V3 chat & R1 reasoning models)", BaseURL: "https://api.deepseek.com", KeyEnv: "DEEPSEEK_API_KEY"},
	{ID: "groq", Title: "Groq — ultra-fast Llama 3 & DeepSeek inference", BaseURL: "https://api.groq.com/openai/v1", KeyEnv: "GROQ_API_KEY"},
	{ID: "openai", Title: "OpenAI — bring your own key (o1, o3-mini, GPT-4o)", BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY"},
	{ID: "mistral", Title: "Mistral & Codestral — state-of-the-art coding models", BaseURL: "https://api.mistral.ai/v1", KeyEnv: "MISTRAL_API_KEY"},
	{ID: "gemini", Title: "Google Gemini — free-tier key from AI Studio", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", KeyEnv: "GEMINI_API_KEY"},
	{ID: "together", Title: "Together AI — open source models, fast Llama & Qwen", BaseURL: "https://api.together.xyz/v1", KeyEnv: "TOGETHER_API_KEY"},
	{ID: "fireworks", Title: "Fireworks AI — high speed function calling & reasoning", BaseURL: "https://api.fireworks.ai/inference/v1", KeyEnv: "FIREWORKS_API_KEY"},
	{ID: "xai", Title: "xAI — Grok models", BaseURL: "https://api.x.ai/v1", KeyEnv: "XAI_API_KEY"},
	{ID: "ollama", Title: "Ollama — local models, free, no key", BaseURL: "http://localhost:11434/v1", KeyEnv: ""},
	{ID: "lmstudio", Title: "LM Studio — local, no key", BaseURL: "http://localhost:1234/v1", KeyEnv: ""},
	{ID: "vllm", Title: "vLLM — high-throughput self-hosted local server", BaseURL: "http://localhost:8000/v1", KeyEnv: ""},
	{ID: "custom", Title: "Custom OpenAI-compatible endpoint", BaseURL: "", KeyEnv: ""},
}
