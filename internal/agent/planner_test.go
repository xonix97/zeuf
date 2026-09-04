package agent

import (
	"context"
	"testing"
)

func TestPlannerFastPath(t *testing.T) {
	p := NewPlanner(nil)
	ev := &Evidence{TestCommand: "go test ./..."}

	g, err := p.Plan(context.Background(), PlanInput{
		Task:     "what does router.go do?",
		Evidence: ev,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Tasks) < 2 {
		t.Fatalf("expected at least 2 tasks in fast path, got %d", len(g.Tasks))
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("fast path graph failed validation: %v", err)
	}
}

func TestPlannerParseJSON(t *testing.T) {
	jsonSample := `
	{
		"goal": "Fix authentication timeout",
		"tasks": [
			{
				"id": "T1",
				"title": "Inspect auth flow",
				"description": "Examine timeout constant and retry logic",
				"dependencies": [],
				"assigned_agent": "explorer",
				"affected_paths": ["internal/auth/client.go"]
			},
			{
				"id": "T2",
				"title": "Implement timeout fix",
				"description": "Increase timeout from 5s to 30s",
				"dependencies": ["T1"],
				"assigned_agent": "implementer",
				"affected_paths": ["internal/auth/client.go"],
				"verification": "go test ./internal/auth"
			}
		]
	}`

	g, err := parsePlanJSON(jsonSample, "Fix authentication timeout")
	if err != nil {
		t.Fatalf("failed to parse valid plan json: %v", err)
	}
	if len(g.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(g.Tasks))
	}
	if g.Goal != "Fix authentication timeout" {
		t.Errorf("goal = %q", g.Goal)
	}

	// Markdown wrapped json
	mdSample := "Here is the plan:\n```json\n" + jsonSample + "\n```\nLet's run it!"
	g2, err := parsePlanJSON(mdSample, "Fix authentication timeout")
	if err != nil {
		t.Fatalf("failed to parse markdown wrapped json: %v", err)
	}
	if len(g2.Tasks) != 2 {
		t.Fatalf("expected 2 tasks from markdown, got %d", len(g2.Tasks))
	}
}

func TestPlannerMalformedJSON(t *testing.T) {
	malformed := `{"goal": "invalid", "tasks": [broken json}`
	_, err := parsePlanJSON(malformed, "task")
	if err == nil {
		t.Fatal("expected error on broken JSON")
	}

	cycleJSON := `
	{
		"goal": "cycle",
		"tasks": [
			{"id": "A", "title": "A", "dependencies": ["B"]},
			{"id": "B", "title": "B", "dependencies": ["A"]}
		]
	}`
	_, err = parsePlanJSON(cycleJSON, "cycle")
	if err == nil {
		t.Fatal("expected cycle detection error on plan JSON")
	}
}
