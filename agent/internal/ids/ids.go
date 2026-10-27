package ids

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

func New(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}
