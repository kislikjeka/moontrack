package zerion_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
)

func TestErrUnsupportedChain(t *testing.T) {
	assert.NotNil(t, zerion.ErrUnsupportedChain)
	assert.Contains(t, zerion.ErrUnsupportedChain.Error(), "unsupported chain")
}
