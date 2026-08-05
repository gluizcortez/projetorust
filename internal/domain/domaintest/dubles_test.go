package domaintest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
)

// TestDiarioRegistraNaOrdem garante que os dublês servem ao propósito para o
// qual existem: asserir a SEQUÊNCIA de chamadas do pipeline na fase F8.
func TestDiarioRegistraNaOrdem(t *testing.T) {
	d := &domaintest.Diario{}
	ctx := context.Background()

	repoImp := &domaintest.RepositorioImportacaoFalso{Diario: d, IDGerado: 42}
	repoPerfil := &domaintest.RepositorioPerfilFalso{Diario: d, Chaves: []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA"},
	}}
	indice := &domaintest.IndiceFalso{Diario: d, Acertos: map[string][]uint64{"ALFA": {3}}}
	indexador := &domaintest.IndexadorFalso{Diario: d, Indice: indice}
	extrator := &domaintest.ExtratorTextoFalso{Diario: d, Paginas: []string{"p1", "p2", "p3"}}
	repoRec := &domaintest.RepositorioRecorteFalso{Diario: d}

	id, err := repoImp.Registrar(ctx, domain.Importacao{IDCaderno: 9, HashSHA256: "h"})
	if err != nil || id != 42 {
		t.Fatalf("Registrar = %d, %v", id, err)
	}
	_ = repoImp.AtualizarStatus(ctx, id, domain.StatusIndexando)
	paginas, _ := extrator.ExtrairPaginas(ctx, []byte("pdf"))
	idx, _ := indexador.Construir(ctx, paginas)
	chaves, _ := repoPerfil.ChavesPesquisa(ctx, id)
	recortes, _ := idx.Frase(ctx, chaves[0].Expressao)
	_, _ = repoRec.Salvar(ctx, id, chaves[0], recortes)
	_ = idx.Fechar()

	esperado := []string{
		"Importacao.Registrar(caderno=9, hash=h)",
		"Importacao.AtualizarStatus(42, indexando)",
		"Extrator.ExtrairPaginas(bytes=3)",
		"Indexador.Construir(paginas=3)",
		"Perfil.ChavesPesquisa(42)",
		`Indice.Frase("ALFA")`,
		`Recorte.Salvar(imp=42, perfil=7, expressao="ALFA", n=1)`,
		"Indice.Fechar()",
	}
	obtido := d.Entradas()
	if len(obtido) != len(esperado) {
		t.Fatalf("registrei %d chamadas, esperava %d:\n%s", len(obtido), len(esperado), strings.Join(obtido, "\n"))
	}
	for i := range esperado {
		if obtido[i] != esperado[i] {
			t.Errorf("chamada %d:\n  obtida:   %s\n  esperada: %s", i, obtido[i], esperado[i])
		}
	}
	if !indice.Fechado {
		t.Error("o índice deveria ficar marcado como fechado")
	}
}

func TestErroInjetadoNaEnesimaChamada(t *testing.T) {
	d := &domaintest.Diario{}
	falha := errors.New("banco fora")
	repo := &domaintest.RepositorioRecorteFalso{Diario: d, ErroNaChamada: 3, Erro: falha}

	ctx := context.Background()
	chave := domain.ChavePesquisa{IDPerfil: 1, Expressao: "X"}
	recortes := []domain.Recorte{domain.NovoRecorte(1, "t")}

	for i := 1; i <= 4; i++ {
		_, err := repo.Salvar(ctx, 10, chave, recortes)
		if i == 3 {
			if !errors.Is(err, falha) {
				t.Errorf("chamada %d deveria falhar", i)
			}
			continue
		}
		if err != nil {
			t.Errorf("chamada %d não deveria falhar: %v", i, err)
		}
	}
	// A chamada que falhou não registra gravação.
	if n := len(repo.GravacoesObservadas()); n != 3 {
		t.Errorf("gravações observadas = %d, esperado 3", n)
	}
}

func TestDiarioEhSeguroParaUsoConcorrente(t *testing.T) {
	d := &domaintest.Diario{}
	repo := &domaintest.RepositorioImportacaoFalso{Diario: d}
	ctx := context.Background()

	pronto := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			_ = repo.AtualizarStatus(ctx, 1, domain.StatusIndexando)
			pronto <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-pronto
	}
	if len(d.Entradas()) != 20 {
		t.Errorf("registrei %d chamadas, esperava 20", len(d.Entradas()))
	}
}
