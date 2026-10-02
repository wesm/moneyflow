package tui

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestMerchantCommitTakesPriorityOverOwnRefresh(t *testing.T) {
	t.Parallel()
	for _, cancelCommit := range []bool{false, true} {
		name := "commit"
		if cancelCommit {
			name = "escape keeps edit pending"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newProviderModel(t, 1)
			fixture.source.writer = tuiProviderWriter{identity: fixture.source.identity}
			started := make(chan struct{})
			fixture.source.setFetch(func(ctx context.Context, _ provider.ProgressFunc) (domain.ImportSnapshot, error) {
				close(started)
				<-ctx.Done()
				return domain.ImportSnapshot{}, ctx.Err()
			})
			model := fixture.model
			updated, _ := model.Update(keyRune('r'))
			model = updated.(Model)
			require.True(t, model.provider.refreshing)
			result := make(chan tea.Msg, 1)
			done := make(chan struct{})
			cancel := model.provider.cancel
			t.Cleanup(func() {
				cancel()
				<-done
			})
			go func(command tea.Cmd) {
				defer close(done)
				result <- command()
			}(model.providerRefreshCommand(true, ""))
			<-started

			model = press(t, model, keyRune('m'))
			model = typeText(t, model, "Renamed Merchant")
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.Equal(t, 1, model.service.Pending().ActiveOperations)
			model = press(t, model, keyRune('w'))
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
			require.Contains(t, rendered, "Stopping refresh to commit")
			model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
			if cancelCommit {
				model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
			}
			var message tea.Msg
			select {
			case message = <-result:
			case <-time.After(time.Second):
				t.Fatal("Commit did not cancel the background refresh")
			}
			updated, command := model.Update(message)
			model = updated.(Model)
			if cancelCommit {
				assert.Equal(t, overlayNone, model.overlay)
				status, err := model.service.ProviderWriteStatus(t.Context())
				require.NoError(t, err)
				assert.Empty(t, status.Phase)
				assert.Equal(t, 1, model.service.Pending().ActiveOperations)
				return
			}
			require.Equal(t, overlayProviderWrite, model.overlay)
			require.NotNil(t, command)
			writeResult := command()
			synctest.Test(t, func(t *testing.T) {
				previousGeneration := model.provider.timerGeneration
				updated, schedule := model.Update(writeResult)
				model = updated.(Model)
				require.NotNil(t, schedule, "write completion must restore background scheduling")
				tick := schedule()
				updated, obsolete := model.Update(providerProgressTickMsg{timerGeneration: previousGeneration})
				model = updated.(Model)
				assert.Nil(t, obsolete)
				_, poll := model.Update(tick)
				require.NotNil(t, poll, "the new schedule must poll provider status")
			})
			assert.Equal(t, overlayNone, model.overlay)
			assert.Zero(t, model.service.Pending().ActiveOperations)
			assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "Renamed Merchant")
		})
	}
}

func TestCommitAfterRefreshDoesNotAcceptChangedReview(t *testing.T) {
	t.Parallel()
	fixture := newProviderModel(t, 1)
	fixture.source.writer = tuiProviderWriter{identity: fixture.source.identity}
	model := press(t, fixture.model, keyRune('m'))
	model = typeText(t, model, "Renamed Merchant")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('r'))
	model = press(t, model, keyRune('w'))
	reviewed := model.review.reviewedRevision
	fixture.source.setSnapshot(tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 2))
	// The refresh can finish its atomic fold before cancellation reaches it,
	// with the result still waiting in the TUI event queue.
	message := model.providerRefreshCommand(true, "")()
	require.Greater(t, model.service.Revision(), reviewed)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	require.Greater(t, model.review.reviewedRevision, reviewed)
	updated, _ = model.Update(message)
	model = updated.(Model)
	assert.Equal(t, overlayReview, model.overlay)
	assert.Contains(t, model.review.err, "review it again")
	assert.Equal(t, 1, model.service.Pending().ActiveOperations)
	status, err := model.service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	assert.Empty(t, status.Phase)
	assert.Equal(t, 2, model.review.projection.Pending.AffectedTransactions)
}
