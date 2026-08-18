package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
)

func TestClockTickUpdatesOnlyRendererTimeAndReschedules(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	revision := model.service.Revision()
	at := time.Date(2026, time.August, 18, 9, 41, 0, 0, time.Local)

	updated, command := model.Update(clockTickMsg{at: at})
	model = updated.(Model)

	assert.Equal(t, at, model.clockAt)
	assert.Equal(t, revision, model.service.Revision())
	assert.NotNil(t, command)
}
