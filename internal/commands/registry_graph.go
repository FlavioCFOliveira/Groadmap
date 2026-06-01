// Package commands — graph family registry entry.
package commands

func buildGraphCommand() Command {
	queryFlag := Flag{
		Long:          "--query",
		Short:         "-q",
		Type:          "string",
		Required:      false,
		Description:   "Cypher query string. When absent, the query is read from standard input.",
		StdinFallback: true,
	}

	return Command{
		Name:          "graph",
		Summary:       "Manage the roadmap knowledge graph (create/query/update/delete/search nodes and edges using Cypher).",
		Description:   "Provides five subcommands that accept Cypher and validate the query's operation class before executing it. The graph is stored under ~/.roadmaps/<name>/graph/ and is created on first use. Each subcommand writes results as JSON to stdout.",
		HelpPrinter:   printGraphHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "create",
				Summary:     "Add nodes or edges (CREATE / MERGE).",
				Description: "Executes a Cypher query whose writing clauses are CREATE and/or MERGE. SET, REMOVE, DELETE, and DETACH DELETE are rejected by the guard rail.",
				Usage:       "rmp graph create -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphCreateHelp,
				Handler:     runGraphCreate,
				ReadsStdin:  true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"ok": true} when no RETURN clause; {"columns": [...], "rows": [[...],...]} when RETURN is present.`,
					Example: `{"ok":true}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Writes to ~/.roadmaps/<name>/graph/wal; creates graph/ on first use (mode 0700).",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Create a node",
						Cmd:    `rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'})"`,
						Stdout: `{"ok":true}`,
						Exit:   0,
					},
					{
						Title: "Create with RETURN",
						Cmd:   `rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'}) RETURN n"`,
						Exit:  0,
					},
					{
						Title:  "Guard-rail: writing clause mismatch",
						Cmd:    `rmp graph create -r myproject --query "MATCH (n) RETURN n"`,
						Stderr: "Error: validation error: graph create accepts only CREATE/MERGE queries",
						Exit:   6,
					},
					{
						Title:  "Roadmap not found",
						Cmd:    `rmp graph create -r missing --query "CREATE (n:Spec)"`,
						Stderr: `Error: resource not found: roadmap "missing" not found`,
						Exit:   4,
					},
				},
			},
			{
				Name:        "query",
				Summary:     "Read nodes or edges (MATCH ... RETURN, read-only).",
				Description: "Executes a read-only Cypher query. Any query containing a writing clause (CREATE, MERGE, SET, REMOVE, DELETE, DETACH DELETE) is rejected by the guard rail.",
				Usage:       "rmp graph query -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphQueryHelp,
				Handler:     runGraphQuery,
				ReadsStdin:  true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"columns": [...], "rows": [[...],...]}`,
					Example: `{"columns":["n.key"],"rows":[["auth"]]}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Read-only (graph/ opened for recovery only).",
					Network:    "None.",
				},
				Idempotent: true,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Query all Spec nodes",
						Cmd:    `rmp graph query -r myproject --query "MATCH (n:Spec) RETURN n.key"`,
						Stdout: `{"columns":["n.key"],"rows":[["auth"]]}`,
						Exit:   0,
					},
					{
						Title: "Query via stdin",
						Cmd:   `echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject`,
						Exit:  0,
					},
					{
						Title:  "No query supplied",
						Cmd:    `rmp graph query -r myproject`,
						Stderr: "Error: required parameter missing: no query supplied",
						Exit:   2,
					},
					{
						Title:  "Guard-rail: writing clause rejected",
						Cmd:    `rmp graph query -r myproject --query "CREATE (n:Spec)"`,
						Stderr: "Error: validation error: graph query accepts only read-only queries",
						Exit:   6,
					},
				},
			},
			{
				Name:        "update",
				Summary:     "Mutate existing nodes or edges (SET / REMOVE).",
				Description: "Executes a Cypher query whose writing clauses are SET and/or REMOVE. CREATE, MERGE, DELETE, and DETACH DELETE are rejected by the guard rail.",
				Usage:       "rmp graph update -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphUpdateHelp,
				Handler:     runGraphUpdate,
				ReadsStdin:  true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"ok": true} when no RETURN clause; {"columns": [...], "rows": [[...],...]} when RETURN is present.`,
					Example: `{"ok":true}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Writes to ~/.roadmaps/<name>/graph/wal.",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Set a property",
						Cmd:    `rmp graph update -r myproject --query "MATCH (n:Spec {key:'auth'}) SET n.status='done'"`,
						Stdout: `{"ok":true}`,
						Exit:   0,
					},
					{
						Title:  "Guard-rail: CREATE not accepted",
						Cmd:    `rmp graph update -r myproject --query "CREATE (n:Spec)"`,
						Stderr: "Error: validation error: graph update accepts only SET/REMOVE queries",
						Exit:   6,
					},
				},
			},
			{
				Name:        "delete",
				Summary:     "Remove nodes or edges (DELETE / DETACH DELETE).",
				Description: "Executes a Cypher query whose writing clauses are DELETE and/or DETACH DELETE. CREATE, MERGE, SET, and REMOVE are rejected by the guard rail.",
				Usage:       "rmp graph delete -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphDeleteHelp,
				Handler:     runGraphDelete,
				ReadsStdin:  true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"ok": true} when no RETURN clause; {"columns": [...], "rows": [[...],...]} when RETURN is present.`,
					Example: `{"ok":true}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Writes to ~/.roadmaps/<name>/graph/wal.",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Detach delete a node",
						Cmd:    `rmp graph delete -r myproject --query "MATCH (n:Spec {key:'auth'}) DETACH DELETE n"`,
						Stdout: `{"ok":true}`,
						Exit:   0,
					},
					{
						Title:  "Guard-rail: read-only query rejected",
						Cmd:    `rmp graph delete -r myproject --query "MATCH (n:Spec) RETURN n"`,
						Stderr: "Error: validation error: graph delete accepts only DELETE/DETACH DELETE queries",
						Exit:   6,
					},
				},
			},
			{
				Name:        "search",
				Summary:     "Traverse the graph (variable-length paths, read-only).",
				Description: "Executes a read-only traversal query, including variable-length path patterns such as -[*1..3]-. Any writing clause is rejected by the guard rail.",
				Usage:       "rmp graph search -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphSearchHelp,
				Handler:     runGraphSearch,
				ReadsStdin:  true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"columns": [...], "rows": [[...],...]}`,
					Example: `{"columns":["p"],"rows":[[{"nodes":[...],"relationships":[...]}]]}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Read-only (graph/ opened for recovery only).",
					Network:    "None.",
				},
				Idempotent: true,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title: "Variable-length traversal",
						Cmd:   `rmp graph search -r myproject --query "MATCH p=(a)-[*1..3]-(b) RETURN p"`,
						Exit:  0,
					},
					{
						Title:  "Guard-rail: writing clause rejected",
						Cmd:    `rmp graph search -r myproject --query "MERGE (n:Spec)"`,
						Stderr: "Error: validation error: graph search accepts only read-only queries",
						Exit:   6,
					},
				},
			},
		},
	}
}
