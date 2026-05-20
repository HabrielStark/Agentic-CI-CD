package cache

import "testing"

func TestDetectHints(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantTool string
	}{
		{"npm", "uses: actions/setup-node@v4\nrun: npm ci", "npm"},
		{"pnpm", "run: pnpm install --frozen-lockfile", "pnpm"},
		{"yarn", "run: yarn install --frozen-lockfile", "yarn"},
		{"pip", "run: pip install -r requirements.txt", "pip"},
		{"go", "uses: actions/setup-go@v5\nrun: go test ./...", "go"},
		{"maven", "run: mvn -B verify", "maven"},
		{"gradle", "run: ./gradlew test", "gradle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hints := DetectHints(c.body)
			if len(hints) == 0 {
				t.Fatalf("no hints for %s", c.name)
			}
			found := false
			for _, h := range hints {
				if h.Tool == c.wantTool {
					found = true
					if h.RestoreCommand == "" {
						t.Fatalf("empty restore command for %s", h.Tool)
					}
					break
				}
			}
			if !found {
				t.Fatalf("expected tool %q in %v", c.wantTool, hints)
			}
		})
	}
}

func TestDetectHints_Combined(t *testing.T) {
	hints := DetectHints("npm ci\ngo mod download\npip install -r requirements.txt")
	tools := map[string]bool{}
	for _, h := range hints {
		tools[h.Tool] = true
	}
	for _, want := range []string{"npm", "go", "pip"} {
		if !tools[want] {
			t.Fatalf("missing tool %q", want)
		}
	}
}

func TestDetectHints_None(t *testing.T) {
	hints := DetectHints("nothing here")
	if len(hints) != 0 {
		t.Fatalf("unexpected: %v", hints)
	}
}
