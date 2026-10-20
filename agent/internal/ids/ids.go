// Package ids gera ULIDs: ordenáveis por tempo e criados localmente, para que
// a sincronização futura não dependa do servidor para atribuir identidade.
package ids

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

func New(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}
