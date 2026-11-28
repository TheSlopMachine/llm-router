package main

import "testing"

func TestLayoutViolationsOf(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		content string
		want    int
	}{
		{"stack gap", "page.svelte", `<VStack gap={3}>`, 1},
		{"stack padding step", "page.svelte", `<VStack pad={3} radius="md">`, 1},
		{"spacing role", "page.svelte", `<VStack spacing="tight" padding>`, 0},
		{"text truncate", "page.svelte", `<Text size="sm" truncate>{name}</Text>`, 1},
		{"text truncate expression", "page.svelte", `<Text truncate={lazy}>{name}</Text>`, 1},
		{"text break", "page.svelte", `<Text mono break>{url}</Text>`, 1},
		{"text line limit", "page.svelte", `<Text lineLimit={1}>{name}</Text>`, 0},
		{"break statement", "page.svelte", "      break\n", 0},
		{"ladder token in style", "page.svelte", `  gap: var(--fui-space-3);`, 1},
		{"ladder token in script", "lib/sizes.ts", `const height = 'calc(var(--fui-space-3) * 2)'`, 1},
		{"role token", "page.svelte", `  gap: var(--fui-spacing-tight);`, 0},
		{"pixel padding", "app.css", `  padding: 12px 16px;`, 1},
		{"pixel width", "app.css", `  width: 56px;`, 0},
		{"rule skips other files", "data.json", `"gap={3}"`, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := layoutViolationsOf(testCase.path, testCase.content)
			if len(got) != testCase.want {
				t.Fatalf("got %d violations %v, want %d", len(got), got, testCase.want)
			}
		})
	}
}
