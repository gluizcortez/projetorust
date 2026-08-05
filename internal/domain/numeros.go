package domain

import (
	"fmt"
	"math"
)

// Conversões estreitantes verificadas.
//
// Existem porque o legado usa try_from, que FALHA em vez de truncar
// (reference/main.rs:603 e 712), enquanto Go converte em silêncio:
// int32(2147483648) devolve -2147483648 sem aviso do compilador nem do tempo
// de execução.
//
// Toda conversão estreitante do serviço passa por aqui. Ver docs/INVARIANTES.md,
// INV-P15 e INV-P18.

// ParaInt32 converte um int64 para int32, recusando o que não couber.
//
// Reproduz i32::try_from (reference/main.rs:712). O chamador decide o que
// fazer com o erro; em MarcarTermino, ele impede que data_fim e
// total_recortes sejam gravados (INV-P18).
func ParaInt32(v int64) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %d não cabe em int32", ErrEstouroNumerico, v)
	}
	return int32(v), nil
}

// ParaInt64 converte um uint64 para int64, recusando o que não couber.
//
// Reproduz i64::try_from (reference/main.rs:603), usado no número da página.
func ParaInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%w: %d não cabe em int64", ErrEstouroNumerico, v)
	}
	return int64(v), nil
}

// TamanhoParaInt32 converte o tamanho de uma coleção para int32.
//
// Atalho para ParaInt32(int64(n)) nos pontos em que o valor vem de len().
func TamanhoParaInt32(n int) (int32, error) {
	return ParaInt32(int64(n))
}
