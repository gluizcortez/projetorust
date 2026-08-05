package postgres

import (
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// dataEmUTC converte a data pura do domínio para time.Time à meia-noite UTC.
//
// UTC é obrigatório e não é detalhe: com o fuso local do processo, uma data
// como 2024-03-15 pode chegar ao driver como 2024-03-14T21:00-03:00 e ser
// gravada com o dia anterior. Ver docs/INVARIANTES.md, INV-P16.
func dataEmUTC(d domain.Data) time.Time {
	return time.Date(d.Ano(), time.Month(d.Mes()), d.Dia(), 0, 0, 0, 0, time.UTC)
}
