// Package cache implements FR-028: dependency-cache hint extraction so a
// capsule can prime its replay container without leaking credentials.
//
// The package never copies sensitive files; it only records lockfile names
// and the recommended cache-restore commands. Replay scripts decide whether
// to honour the hints based on the user's Options.
package cache

import (
	"sort"
	"strings"
)

// Hint is a structured cache hint embedded into a capsule.
type Hint struct {
	// Tool is one of "npm", "pnpm", "yarn", "pip", "uv", "go", "maven", "gradle".
	Tool string `json:"tool"`
	// Lockfiles lists the canonical lockfile paths for the tool.
	Lockfiles []string `json:"lockfiles"`
	// RestoreCommand is the recommended command to prime the cache.
	RestoreCommand string `json:"restoreCommand"`
	// Notes adds human context.
	Notes string `json:"notes,omitempty"`
}

// DetectHints analyses a workflow YAML body and returns the cache hints
// applicable to it. The detection is content-based (not full YAML parsing)
// to remain robust to formatting; keywords are matched case-insensitively.
func DetectHints(workflowYAML string) []Hint {
	body := strings.ToLower(workflowYAML)
	seen := map[string]Hint{}

	cases := []struct {
		match []string
		hint  Hint
	}{
		{
			match: []string{"actions/setup-node", "npm ci", "npm install"},
			hint: Hint{
				Tool: "npm", Lockfiles: []string{"package-lock.json", "npm-shrinkwrap.json"},
				RestoreCommand: "npm ci || npm install",
				Notes:          "Mount or COPY package-lock.json before `npm ci` to maximise cache hits.",
			},
		},
		{
			match: []string{"pnpm install", "pnpm/action-setup", "pnpm install --frozen-lockfile"},
			hint: Hint{
				Tool: "pnpm", Lockfiles: []string{"pnpm-lock.yaml"},
				RestoreCommand: "pnpm install --frozen-lockfile || pnpm install",
			},
		},
		{
			match: []string{"yarn install", "yarn install --frozen-lockfile"},
			hint: Hint{
				Tool: "yarn", Lockfiles: []string{"yarn.lock"},
				RestoreCommand: "yarn install --frozen-lockfile || yarn install",
			},
		},
		{
			match: []string{"pip install -r ", "pip install -e ", "actions/setup-python", "uv pip"},
			hint: Hint{
				Tool: "pip", Lockfiles: []string{"requirements.txt", "Pipfile.lock", "poetry.lock"},
				RestoreCommand: "pip install -r requirements.txt",
			},
		},
		{
			match: []string{"go test", "go build", "actions/setup-go", "go mod"},
			hint: Hint{
				Tool: "go", Lockfiles: []string{"go.mod", "go.sum"},
				RestoreCommand: "go mod download",
			},
		},
		{
			match: []string{"mvn ", "actions/setup-java", "maven-resolver"},
			hint: Hint{
				Tool: "maven", Lockfiles: []string{"pom.xml"},
				RestoreCommand: "mvn -B -DskipTests dependency:resolve",
			},
		},
		{
			match: []string{"./gradlew", "gradle "},
			hint: Hint{
				Tool: "gradle", Lockfiles: []string{"gradle.lockfile", "build.gradle", "build.gradle.kts"},
				RestoreCommand: "./gradlew dependencies",
			},
		},
	}

	for _, c := range cases {
		for _, m := range c.match {
			if strings.Contains(body, m) {
				if _, ok := seen[c.hint.Tool]; !ok {
					seen[c.hint.Tool] = c.hint
				}
				break
			}
		}
	}
	out := make([]Hint, 0, len(seen))
	for _, h := range seen {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}
