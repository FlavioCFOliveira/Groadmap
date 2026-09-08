// Package commands — graph family registry entry.
package commands

func buildGraphCommand() Command {
	// socketFlag names the socket an invocation uses. It is declared once and
	// shared by both graph subcommands, so the two cannot come to describe it
	// differently — the flag's whole point is that a caller and a server which
	// name the same socket agree on what that means.
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
		Description:   "Provides two subcommands. serve opens the roadmap graph and makes it available over a Unix domain socket until it is stopped; client sends one Cypher statement to a running server and prints what comes back. The graph is reached through a running server and through nothing else: serve is the only process that opens the store, client is the only subcommand that runs a statement, and with nothing listening client fails rather than opening anything. Using the graph therefore begins by starting a server, and starting one against a roadmap that has never had a graph is what creates it. rmp does not examine a statement and never refuses one for what it does, so the one statement-running subcommand covers reads, writes, deletions, schema DDL and schema introspection alike. The graph is stored under ~/.roadmaps/<name>/graph/. Results are written as JSON to stdout.",
		HelpPrinter:   printGraphHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "serve",
				Summary:     "Serve the roadmap knowledge graph over a Unix domain socket until stopped.",
				Description: "Opens the roadmap knowledge graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher statements over a Unix domain socket. The protocol is Bolt version 5, served by the graph engine's own server; rmp defines no protocol of its own. serve is long-lived: unlike every other command except rmp web it does not complete and exit, but runs until it receives SIGINT (Ctrl+C) or SIGTERM, whereupon it drains the work in flight, shuts the server down, checkpoints, releases the lock, removes its socket, and exits 0. Starting a server is how a roadmap graph comes into being and nothing else creates one: against a roadmap that has never had a graph, serve creates ~/.roadmaps/<name>/graph/ with mode 0700 and serves it empty, and a serve that is refused for any other reason creates nothing and leaves no directory behind. It runs no statement of its own and never reads or writes a roadmap project.db. One server runs per roadmap: a second rmp graph serve against the same roadmap cannot take the store lock, fails with exit code 1, and leaves the first server's socket untouched; it does not queue. Access control is the filesystem and there is no other. The socket is created with mode 0600, set explicitly rather than left to the process umask, inside a roadmap home directory that is 0700, and the server authenticates nobody, so any caller able to open the socket can read, write, delete and change the schema of that roadmap's graph. A non-default --socket path is followed by the CLI through the same flag and by nothing else: the web interface has no way to receive one, resolves the derived path, finds nothing there, and fails against this server's lock for as long as it runs.",
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
					Filesystem: "Creates ~/.roadmaps/<name>/graph/ (mode 0700) when the roadmap has none, after every refusal that could still stop the server has been passed; binds ~/.roadmaps/<name>/graph.sock (mode 0600) and removes it on shutdown; holds the exclusive lock on ~/.roadmaps/<name>/graph/ for the process lifetime; writes to that store's wal and snapshot/ on behalf of the statements clients send.",
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
			{
				Name:        "client",
				Summary:     "Send one Cypher statement to a running graph server and print its result.",
				Description: "Sends exactly one Cypher statement to a graph server over its Unix domain socket and prints what comes back. This is the only way to run a statement against a roadmap graph. It reads and writes alike: the server does not examine the statement at all, so a statement that creates, changes, deletes or alters the schema is executed and committed. The statement comes from --query or, when that flag is absent, from standard input. It requires a server: it resolves ~/.roadmaps/<name>/graph.sock, or the --socket path when one is given, and with nothing listening there it fails with exit code 1 rather than opening the store, which no subcommand does. Start one with rmp graph serve -r <roadmap>. A retriable serialisation conflict is not an error and is not printed: two clients writing to the same nodes at once is ordinary inside a server and the losing statement committed nothing, so it is re-sent under the retry policy and a failure is reported only once that policy or the statement time budget is exhausted. The statement runs under a 5-second time budget enforced by the server, and this command keeps a later deadline of its own purely as a backstop against a server that answers nothing.",
				Usage:       "rmp graph client -r <roadmap> [--query <cypher>] [--socket <path>]",
				HelpPrinter: printGraphClientHelp,
				Handler:     runGraphClient,
				// `client` reads a Cypher statement from two sources, --query and
				// standard input, so it publishes its own refusal for an excess
				// positional argument: the canonical line with the hint naming
				// those two sources. It is now the only subcommand in the CLI
				// that publishes that hint (SPEC/COMMANDS.md § Client Error
				// Cases, the stray-positional row; § Positional Arguments).
				PublishesOwnArityRefusal: true,
				ReadsStdin:               true,
				Flags: []Flag{
					sharedRoadmapFlag(),
					queryFlag,
					socketFlag,
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"columns": [...], "rows": [[...],...], "plan": {...} with EXPLAIN, "profile": {...} with PROFILE (never both), "counters": {...} when the statement changed the graph} when the statement produces result columns or carries either prefix; {"ok": true, "counters": {...} when the statement changed the graph} when it produces none.`,
					Example: `{"columns":["n.key"],"rows":[["auth"]]}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (SQLite project.db not touched).",
					Filesystem: "None in this process. The statement runs in the server's store, which writes to that store's wal and snapshot/ on its behalf; this command opens no store, takes no lock, and creates no graph directory.",
					Network:    "None. The statement crosses a Unix domain socket, not a network.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 1, 2, 3, 4, 6},
				Examples: []Example{
					{
						Title:  "Read the Spec nodes through a running server",
						Cmd:    `rmp graph client -r myproject --query "MATCH (n:Spec) RETURN n.key"`,
						Stdout: `{"columns":["n.key"],"rows":[["auth"]]}`,
						Exit:   0,
					},
					{
						Title:  "Write through a running server",
						Cmd:    `rmp graph client -r myproject --query "CREATE (n:Spec {key:'auth'})"`,
						Stdout: `{"ok":true,"counters":{"nodesCreated":1,"propertiesWritten":1,"labelsAdded":1}}`,
						Exit:   0,
					},
					{
						Title: "Read the statement from standard input",
						Cmd:   `echo "MATCH (n) RETURN count(n)" | rmp graph client -r myproject`,
						Exit:  0,
					},
					{
						Title: "Reach a server started on a socket of its own",
						Cmd:   `rmp graph client -r myproject --socket /run/user/1000/myproject-graph.sock --query "SHOW INDEXES"`,
						Exit:  0,
					},
					{
						Title:  "No statement supplied",
						Cmd:    `rmp graph client -r myproject`,
						Stderr: "Error: required parameter missing: no query supplied",
						Exit:   2,
					},
					{
						Title:  "Roadmap not found",
						Cmd:    `rmp graph client -r missing --query "MATCH (n) RETURN n"`,
						Stderr: `Error: resource not found: roadmap "missing" not found`,
						Exit:   4,
					},
				},
			},
		},
	}
}
