package domain

// Recorte é uma ocorrência de expressão de perfil em uma página de documento.
// É o produto do serviço.
type Recorte struct {
	// Pagina é o número da página, começando em 1
	// (reference/main.rs:526: doc.add_u64(page_field, (i+1) as u64)).
	Pagina uint64

	// Texto é o texto integral da página.
	//
	// DADO MORTO NO LEGADO: o campo `text` de Recorte é escrito e nunca lido —
	// salvar_recorte grava `highlight` (reference/main.rs:619). O compilador
	// Rust confirma com "field `text` is never read". Mantido por fidelidade;
	// em Go compartilha o mesmo backing array de Destaque, então custa um
	// cabeçalho de string, não uma cópia.
	// Ver docs/MAPA-DE-CHAMADAS.md §4.1.
	Texto string

	// Destaque é o que vai para recorte.tb_recorte_texto.recorte.
	//
	// Apesar do nome, NÃO é um trecho ao redor da ocorrência: é o texto
	// integral da página, normalizado e sem diacríticos — idêntico a Texto por
	// construção. Recortar uma janela de contexto é evolução da fase F11,
	// atrás de chave. Ver docs/ESPECIFICACAO.md §5.4.
	Destaque string
}

// NovoRecorte monta um recorte a partir do texto de uma página.
//
// Texto e Destaque recebem o mesmo valor, como em reference/main.rs:400-406.
func NovoRecorte(pagina uint64, textoDaPagina string) Recorte {
	return Recorte{
		Pagina:   pagina,
		Texto:    textoDaPagina,
		Destaque: textoDaPagina,
	}
}
