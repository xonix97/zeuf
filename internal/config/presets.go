package config

// Preset is a one-shot template for connecting a model backend.
type Preset struct {
	ID      string
	Title   string
	BaseURL string
	KeyEnv  string // suggested env var; "" means no key needed
}

// Presets lists the backends the /connect wizard can attach.
var Presets = []Preset{
	{ID: "openrouter", Title: "OpenRouter — many models, one key", BaseURL: "https://openrouter.ai/api/v1", KeyEnv: "OPENROUTER_API_KEY"},
	{ID: "openai", Title: "OpenAI", BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY"},
	{ID: "anthropic", Title: "Anthropic (OpenAI-compatible)", BaseURL: "https://api.anthropic.com/v1", KeyEnv: "ANTHROPIC_API_KEY"},
	{ID: "ollama", Title: "Ollama — local, no key", BaseURL: "http://localhost:11434/v1", KeyEnv: ""},
	{ID: "lmstudio", Title: "LM Studio — local, no key", BaseURL: "http://localhost:1234/v1", KeyEnv: ""},
	{ID: "custom", Title: "Custom OpenAI-compatible endpoint", BaseURL: "", KeyEnv: ""},
}
