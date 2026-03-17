# Groadmap

Local Roadmap Manager CLI for agentic workflows. Groadmap é uma ferramenta CLI em Go para gestão de roadmaps técnicos, utilizando SQLite como backend.

## Funcionalidades

- **Gestão de Roadmaps**: Criar, listar, selecionar e remover roadmaps
- **Gestão de Tarefas**: Criar, editar, listar tarefas com status, prioridade e severidade
- **Gestão de Sprints**: Organizar tarefas em sprints com ciclo de vida completo
- **Audit Trail**: Registo automático de todas as operações
- **Backup/Restore**: Criar e restaurar backups dos roadmaps
- **Export/Import**: Exportar e importar roadmaps em formato JSON
- **Métricas**: Monitorização de operações e performance

## Comandos Disponíveis

| Comando | Descrição | Documentação |
|---------|-----------|--------------|
| `roadmap` | Gestão de roadmaps (criar, listar, selecionar) | [DOCS/commands/roadmap.md](DOCS/commands/roadmap.md) |
| `task` | Gestão de tarefas | [DOCS/commands/task.md](DOCS/commands/task.md) |
| `sprint` | Gestão de sprints | [DOCS/commands/sprint.md](DOCS/commands/sprint.md) |
| `audit` | Registo de auditoria | [DOCS/commands/audit.md](DOCS/commands/audit.md) |
| `user` | Gestão de utilizadores | [DOCS/commands/user.md](DOCS/commands/user.md) |
| `metrics` | Métricas e monitorização | [DOCS/commands/metrics.md](DOCS/commands/metrics.md) |

## Instalação

### Compilar a partir do código fonte

```bash
# Clonar o repositório
git clone https://github.com/FlavioCFOliveira/Groadmap.git
cd Groadmap

# Compilar
go build -o ./bin/ ./cmd/rmp

# Adicionar ao PATH (opcional)
cp ./bin/rmp /usr/local/bin/
```

## Uso Rápido

```bash
# Criar um novo roadmap
rmp roadmap create myproject

# Selecionar roadmap por omissão
rmp roadmap use myproject

# Criar uma tarefa
rmp task create -d "Implementar feature X" -a "Desenvolver código" -e "Feature funcional"

# Listar tarefas
rmp task list

# Criar um sprint
rmp sprint create -d "Sprint 1 - Setup"

# Adicionar tarefas ao sprint
rmp sprint add-tasks 1 1,2,3

# Iniciar sprint
rmp sprint start 1
```

## Estrutura do Projeto

```
.
├── cmd/rmp/main.go          # Ponto de entrada CLI
├── internal/
│   ├── commands/            # Subcomandos (roadmap, task, sprint, audit)
│   ├── db/                  # SQLite, schema, queries parametrizadas
│   ├── models/              # Structs e enums
│   └── utils/               # JSON, datas ISO 8601, paths
├── bin/                     # Output de build
├── SPEC/                    # Especificações técnicas
└── DOCS/                    # Documentação de comandos
```

## Convenções

- **Output sucesso**: JSON para stdout
- **Output erro**: Plain text para stderr
- **Datas**: ISO 8601 UTC
- **Roadmaps**: Armazenados em `~/.roadmaps/` com permissões `0700`

## Documentação Técnica

Consulte a pasta `SPEC/` para documentação técnica detalhada:
- `SPEC/ARCHITECTURE.md` - Design do sistema
- `SPEC/COMMANDS.md` - Hierarquia CLI e aliases
- `SPEC/DATABASE.md` - Schema SQLite
- `SPEC/DATA_FORMATS.md` - Schema JSON outputs
- `SPEC/MODELS.md` - Definição de modelos
- `SPEC/STATE_MACHINE.md` - Máquinas de estado

## Licença

MIT License - ver [LICENSE](LICENSE) para detalhes.
