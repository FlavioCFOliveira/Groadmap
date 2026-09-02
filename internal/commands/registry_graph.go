// Package commands — graph family registry entry.
package commands

func buildGraphCommand() Command {
	// socketFlag names the socket an invocation resolves. It is declared once and
	// shared by every graph subcommand that publishes it, so the three cannot come
	// to describe it differently — the flag's whole point is that a caller and a
	// server which name the same socket agree on what that means.
	socketFlag := Flag{
		Long:        "--socket",
		Type:        "string",
		Required:    false,
		Default:     "~/.roadmaps/<name>/graph.sock",
		Description: "Path of the graph server Unix domain socket this invocation resolves. Defaults to the path derived from the selected roadmap.",
	}

	queryFlag := Flag{
		Long:          "--query",
		Short:         "-q",
		Type:          "string",
		Required:      false,
		Description:   "Cypher statement. When absent, the statement is read from standard input.",
		StdinFallback: true,
	}

	return Command{
		Name:          "graph",
		Summary:       "Operate the roadmap knowledge graph by running Cypher statements against it.",
		Description:   "Provides two subcommands. execute runs any Cypher statement the engine accepts against the roadmap graph and returns what it produces; serve makes that graph available over a Unix domain socket until it is stopped, so a caller pays one store open instead of one per invocation. rmp does not examine a statement and never refuses one for what it does, so a single subcommand covers reads, writes, deletions, schema DDL and schema introspection alike. The names create, query, update, delete and search are not subcommands of rmp graph and do not resolve. The graph is stored under ~/.roadmaps/<name>/graph/ and is created on first use by execute; serve creates none. Results are written as JSON to stdout.",
		HelpPrinter:   printGraphHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "execute",
				Summary:     "Run one Cypher statement against the roadmap knowledge graph.",
				Description: "Executes exactly one Cypher statement and prints what it returns. Every statement the engine accepts is accepted here: a read such as MATCH ... RETURN, including variable-length traversals; a write such as CREATE, MERGE, SET or REMOVE; a deletion such as DELETE or DETACH DELETE; schema DDL, namely CREATE INDEX [name] [IF NOT EXISTS] FOR (n:Label) ON (n.property) [OPTIONS {indexType:'hash'|'btree'}], DROP INDEX <name> [IF EXISTS], CREATE CONSTRAINT [name] [IF NOT EXISTS] FOR (n:Label) REQUIRE n.property IS UNIQUE (or IS NOT NULL) and DROP CONSTRAINT <name> [IF EXISTS]; and schema introspection, namely SHOW INDEXES and SHOW CONSTRAINTS with their singular aliases SHOW INDEX and SHOW CONSTRAINT, each optionally followed by a YIELD / WHERE / RETURN projection. rmp does not inspect the statement and refuses none on the ground of its operation class, so what a statement reads, writes or deletes is decided by its Cypher alone and by nothing rmp checks. A statement that changes the graph runs inside a single transaction and is durable before the process exits. Exactly one statement per invocation. Schema introspection returns the columns/rows listing rather than {\"ok\": true} even though it carries no RETURN clause. There is no ALTER INDEX; changing an index is a DROP INDEX followed by a CREATE INDEX, as two separate invocations, and the index is absent between them. An index or a constraint covers a single node property, removal is by name, and SHOW INDEXES is the authoritative report of the name an unnamed object was given. A schema statement the engine refuses — a duplicate create, a drop of an object that does not exist, an unsupported definition, or a constraint the data already in the graph does not satisfy — exits 1.",
				Usage:       "rmp graph execute -r <roadmap> [--query <cypher>]",
				HelpPrinter: printGraphExecuteHelp,
				Handler:     runGraphExecute,
				// `graph` publishes its own refusal for an excess positional
				// argument — the canonical line with a hint naming the two
				// sources a Cypher query may come from (SPEC/COMMANDS.md
				// § Positional Arguments; SPEC/GRAPH.md § Cypher Input Source
				// and Precedence). The shared enforcement point defers so that
				// wording survives.
				PublishesOwnArityRefusal: true,
				ReadsStdin:               true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"columns": [...], "rows": [[...],...]} when the statement produces result columns; {"ok": true} when it produces none.`,
					Example: `{"columns":["n.key"],"rows":[["auth"]]}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Writes to ~/.roadmaps/<name>/graph/wal when the statement writes; creates graph/ on first use (mode 0700).",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Read the Spec nodes",
						Cmd:    `rmp graph execute -r myproject --query "MATCH (n:Spec) RETURN n.key"`,
						Stdout: `{"columns":["n.key"],"rows":[["auth"]]}`,
						Exit:   0,
					},
					{
						Title:  "Create a node",
						Cmd:    `rmp graph execute -r myproject --query "CREATE (n:Spec {key:'auth'})"`,
						Stdout: `{"ok":true}`,
						Exit:   0,
					},
					{
						Title:  "Create an index",
						Cmd:    `rmp graph execute -r myproject --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"`,
						Stdout: `{"ok":true}`,
						Exit:   0,
					},
					{
						Title: "Read the statement from standard input",
						Cmd:   `echo "MATCH (n) RETURN count(n)" | rmp graph execute -r myproject`,
						Exit:  0,
					},
					{
						Title:  "No statement supplied",
						Cmd:    `rmp graph execute -r myproject`,
						Stderr: "Error: required parameter missing: no query supplied",
						Exit:   2,
					},
					{
						Title:  "Roadmap not found",
						Cmd:    `rmp graph execute -r missing --query "CREATE (n:Spec)"`,
						Stderr: `Error: resource not found: roadmap "missing" not found`,
						Exit:   4,
					},
				},
			},
			{
				Name:        "serve",
				Summary:     "Serve the roadmap knowledge graph over a Unix domain socket until stopped.",
				Description: "Opens the roadmap knowledge graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher statements over a Unix domain socket. The protocol is Bolt version 5, served by the graph engine's own server; rmp defines no protocol of its own. serve is long-lived: unlike every other command except rmp web it does not complete and exit, but runs until it receives SIGINT (Ctrl+C) or SIGTERM, whereupon it drains the work in flight, shuts the server down, checkpoints, releases the lock, removes its socket, and exits 0. It runs no statement of its own, creates no graph directory that does not already exist, and never reads or writes a roadmap project.db. One server runs per roadmap: a second rmp graph serve against the same roadmap cannot take the store lock, fails with exit code 1, and leaves the first server's socket untouched; it does not queue. Access control is the filesystem and there is no other. The socket is created with mode 0600, set explicitly rather than left to the process umask, inside a roadmap home directory that is 0700, and the server authenticates nobody, so any caller able to open the socket can read, write, delete and change the schema of that roadmap's graph. A non-default --socket path is followed by the CLI through the same flag and by nothing else: the web interface has no way to receive one, resolves the derived path, finds nothing there, and fails against this server's lock for as long as it runs.",
				Usage:       "rmp graph serve -r <roadmap> [--socket <path>]",
				HelpPrinter: printGraphServeHelp,
				Handler:     runGraphServe,
				Flags: []Flag{
					sharedRoadmapFlag(),
					socketFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"socket": "<absolute path>"} written once at startup, naming the socket the server bound. Per-statement results go to the client that asked for them, not to this command's stdout.`,
					Example: `{"socket":"/home/user/.roadmaps/myproject/graph.sock"}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "Binds ~/.roadmaps/<name>/graph.sock (mode 0600) and removes it on shutdown; holds the exclusive lock on ~/.roadmaps/<name>/graph/ for the process lifetime; writes to that store's wal and snapshot/ on behalf of the statements clients send.",
					Network:    "None. The transport is a Unix domain socket; no network port is bound, on loopback or anywhere else.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4},
				Examples: []Example{
					{
						Title:  "Serve a roadmap's graph on the default socket",
						Cmd:    "rmp graph serve -r myproject",
						Stdout: `{"socket":"/home/user/.roadmaps/myproject/graph.sock"}`,
						Exit:   0,
					},
					{
						Title: "Serve on a socket of your own choosing",
						Cmd:   "rmp graph serve -r myproject --socket /run/user/1000/myproject-graph.sock",
						Exit:  0,
					},
					{
						Title:  "Roadmap not found",
						Cmd:    "rmp graph serve -r missing",
						Stderr: `Error: resource not found: roadmap "missing" not found`,
						Exit:   4,
					},
				},
			},
		},
	}
}
