# Serviço de recorte de diários oficiais — migração Rust → Go

Reescrita em Go do serviço de ingestão e recorte de diários oficiais, hoje em
Rust. **A regra dura do projeto é paridade comportamental total:** nenhuma
funcionalidade pode ser alterada ou removida, e toda evolução técnica entra
atrás de chave de configuração desligada por padrão.

## Onde está o quê

| Caminho | Conteúdo |
|---|---|
| `docs/roadmap-migracao-rust-go.html` | Documento de arquitetura: análise do legado, armadilhas de paridade, arquitetura alvo e as 14 fases com seus prompts |
| `docs/ESPECIFICACAO.md` | Especificação normativa do comportamento observável |
| `docs/INVARIANTES.md` | As 20 invariantes de paridade, com casos positivos e negativos |
| `docs/DECISOES-ABERTAS.md` | Perguntas pendentes, com padrão provisório e responsável |
| `docs/MAPA-DE-CHAMADAS.md` | Grafo de chamadas do legado e achados estruturais |
| `docs/CONTEXT.md` | Memória do projeto entre fases — decisões, desvios, pendências |
| `reference/main.rs` | Implementação Rust de referência (normativa) |
| `tools/` | Ferramental de apoio: captura de corpus e geração de corpus sintético |
| `test/testdata/corpus/` | Documentos de entrada |
| `test/testdata/expected/` | Saídas capturadas do legado — oráculo de paridade |

## Estado

| Fase | Descrição | Estado |
|---|---|---|
| F0 | Especificação executável e corpus dourado | **concluída** com ressalvas (D-11, D-15) |
| F1 | Fundação do repositório e cadeia de ferramentas | **concluída** — empacotamento não verificado (sem Docker no ambiente) |
| F2 | Configuração tipada e observabilidade | **concluída** |
| F3 | Núcleo de domínio e portas | **concluída** |
| F4 | Persistência | **concluída** |
| F5 | Extração de texto do PDF | pendente |
| F6 | Normalização e tokenização | pendente |
| F7 | Índice e busca | pendente |
| F8 | Pipeline e máquina de estados | pendente |
| F9 | Camada HTTP | pendente |
| F10 | Ciclo de vida e drenagem | pendente |
| F11 | Evoluções técnicas atrás de chaves | pendente |
| F12 | Paridade e execução em sombra | pendente |
| F13 | Corte e descomissionamento | pendente |

## Antes de continuar

Duas pendências bloqueantes devem ser solicitadas o quanto antes, porque o
prazo de resposta corre em paralelo às fases seguintes:

- **D-15** — `Cargo.toml` e `Cargo.lock` do serviço em produção.
- **D-11** — corpus de PDFs reais anonimizados e *dump* das tabelas de perfil.

Dois achados são **defeitos de produto existentes**, não questões da migração, e
foram escalados: **D-17** (perfis com expressões acentuadas estão inertes) e
**D-18** (PDFs truncados são registrados como processados com sucesso).
